//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const extendedFilesystemScript = `import fs,{Dirent} from "node:fs";
import {open,readdir,readFile} from "node:fs/promises";
const path="新-file.bin";
const missing=await open("missing-file.bin").then(()=>null,error=>error);
if(missing?.code!=="ENOENT"||typeof missing.errno!=="number"||missing.errno<=0)throw Error("filesystem error code and errno");
const file=await open(path,"wx+",0o600);
try{
 const metadata=await file.stat();
 if(!(metadata.mtime instanceof Date)||metadata.mtime.getTime()!==Math.trunc(metadata.mtimeMs))throw Error("Date and mtimeMs metadata");
 if((metadata.mode&0o170000)!==0o100000)throw Error("POSIX file type mode");
 await file.write("abcdef");
 await file.truncate(3);
 await file.sync();
 if((await file.stat()).size!==3)throw Error("descriptor metadata");
 await file.write("z");
 const buffer=new Uint8Array(7);
 const result=await file.read(buffer,{position:0});
 if(result.bytesRead!==7||buffer.join()!=="97,98,99,0,0,0,122")throw Error("truncate changed cursor or lost zero fill");
}finally{await file.close()}
const append=await open(path,"a+");try{await append.write("!",0);await append.sync()}finally{await append.close()}
if((await readFile(path)).length!==8)throw Error("append honored explicit position");
const exists=await open(path,"wx").then(()=>null,error=>error);
if(exists?.code!=="EEXIST"||typeof exists.errno!=="number"||exists.errno<=0)throw Error("exclusive create error code and errno");
for(const entries of [fs.readdirSync(".",{withFileTypes:true}),await readdir(".",{withFileTypes:true})]){
 const entry=entries.find(item=>item.name===path);
 if(!(entry instanceof Dirent)||!entry.isFile()||entry.isDirectory()||entry.parentPath!==".")throw Error("directory entry contract");
}
if(!(await readdir(".")).includes(path))throw Error("default names changed");
console.log("extended filesystem ready");`

func TestFileHandleBasicsAcrossExecutionModes(t *testing.T) {
	for _, mode := range []string{"run", "eval", "worker"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var got cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "main.js"), extendedFilesystemScript)
				got = runCLI(t, work, nil, "run", "main.js")
			case "eval":
				got = runCLI(t, work, nil, "eval", extendedFilesystemScript)
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.js"), extendedFilesystemScript)
				got = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";const w=new Worker("./worker.js");await new Promise((resolve,reject)=>{w.on("error",reject);w.on("exit",code=>code?reject(Error("worker exit "+code)):resolve())});`)
			}
			requireExit(t, got, 0)
			if got.stdout != "extended filesystem ready\n" || got.stderr != "" {
				t.Fatalf("filesystem result: %+v", got)
			}
		})
	}
}

func TestFileHandleBasicsThroughAgentRunJS(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 0 || !strings.Contains(result.Stdout, "extended filesystem ready") {
					t.Errorf("run_js filesystem result: %+v", result)
				}
				return agentModelReply{Content: "filesystem fixture complete"}
			}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("filesystem", "// Exercise filesystem handles and directory entries.\n"+extendedFilesystemScript)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	got := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"exercise filesystem handles");if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`)
	requireExit(t, got, 0)
	if !strings.Contains(got.stdout, "filesystem fixture complete") {
		t.Fatalf("agent result: %+v", got)
	}
}
