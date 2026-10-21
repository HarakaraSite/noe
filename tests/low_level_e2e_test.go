//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// No protocol helper is installed in Go: the same public modules implement
// the server, concurrent clients, FIN/response exchange and indexed update.
func lowLevelRoundTrip(network string) string {
	return `const {open}=await import("noe:fs-handle");
const {listen,connect}=await import("noe:sockets");
const {readFileSync}=await import("node:fs");
const network=` + strconv.Quote(network) + `;
const options=network==="tcp"?{network,host:"127.0.0.1",port:0}:{network,path:"./s.sock"};
const file=await open("index.bin",{mode:"r+"});
const listener=await listen(options);
async function allBytes(readable){return new Uint8Array(await new Response(readable).arrayBuffer())}
try {
 const before=readFileSync("index.bin");
 const {data,bytesRead,eof}=await file.readAt(4,4);
 if(bytesRead!==4||eof||data[0]!==4)throw Error("index read");
 const serving=(async()=>{
  const jobs=[];
  for(let n=0;n<3;n++){
   const c=await listener.accept();
   jobs.push((async()=>{try{
    const request=await allBytes(c.readable);
    for(let i=0;i<request.length;i++)request[i]^=255;
    const writer=c.writable.getWriter();await writer.write(request);await writer.close();
   }finally{await c.close()}})());
  }
  /* Accepted connections remain usable after the listener closes. */
  await listener.close();await Promise.all(jobs);
 })();
 const requests=await Promise.all([0,1,2].map(async n=>{
  const c=await connect(network==="tcp"?{network,host:"127.0.0.1",port:listener.address.port}:options);
  try{
   const payload=new Uint8Array(131072+n);for(let i=0;i<payload.length;i++)payload[i]=(i+n+data[0])%256;
   const response=allBytes(c.readable), writer=c.writable.getWriter();
   await writer.write(payload);await writer.close();
   const reply=await response;
   if(reply.length!==payload.length||reply.some((b,i)=>b!==(payload[i]^255)))throw Error("binary/FIN response");
   return reply[0];
  }finally{await c.close()}
 }));
 await serving;
 if(await file.writeAt(4,new Uint8Array([...requests,99]))!==4)throw Error("index write");
 const after=readFileSync("index.bin");
 if(after.length!==before.length||after.some((b,i)=>(i<4||i>=8)&&b!==before[i]))throw Error("unrelated bytes changed");
 console.log("low-level roundtrip passed");
}finally{await listener.close();await file.close()}`
}

func lowLevelFixture(t *testing.T) string {
	t.Helper()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "index.bin"), []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, 0600); err != nil {
		t.Fatal(err)
	}
	return work
}

func TestLowLevelPublicModulesAcrossExecutionModes(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		if network == "unix" && runtime.GOOS == "windows" {
			continue // AF_UNIX availability depends on the Windows host version.
		}
		for _, mode := range []string{"run", "eval", "worker", "events", "observe"} {
			t.Run(network+"/"+mode, func(t *testing.T) {
				work, source := lowLevelFixture(t), lowLevelRoundTrip(network)
				var got cliResult
				switch mode {
				case "run":
					writeFixture(t, filepath.Join(work, "main.js"), source)
					got = runCLI(t, work, nil, "run", "main.js")
				case "eval":
					got = runCLI(t, work, nil, "eval", source)
				case "events":
					got = runCLI(t, work, nil, "eval", "--events=-", source)
					if !strings.Contains(processIOEventText(t, got.stdout, "stdout"), "low-level roundtrip passed") {
						t.Fatalf("missing event output: %+v", got)
					}
				case "observe":
					path := filepath.Join(work, "events.jsonl")
					got = runCLI(t, work, nil, "eval", "--observe", "--events="+path, source)
					data, err := os.ReadFile(path)
					if err != nil || !strings.Contains(processIOEventText(t, string(data), "stdout"), "low-level roundtrip passed") {
						t.Fatalf("missing observe output: %v %+v", err, got)
					}
				case "worker":
					writeFixture(t, filepath.Join(work, "worker.js"), source)
					got = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";
const w=new Worker("./worker.js");await new Promise((resolve,reject)=>{w.on("error",reject);w.on("exit",code=>code?reject(Error("worker exit "+code)):resolve())});`)
				}
				requireExit(t, got, 0)
				if got.stderr != "" || (mode != "observe" && mode != "events" && !strings.Contains(got.stdout, "low-level roundtrip passed")) {
					t.Fatalf("roundtrip: %+v", got)
				}
				if network == "unix" {
					if _, err := os.Lstat(filepath.Join(work, "s.sock")); !os.IsNotExist(err) {
						t.Fatalf("owned Unix pathname remains: %v", err)
					}
				}
			})
		}
	}
}

func TestLowLevelFixtureAgentRunJS(t *testing.T) {
	work := lowLevelFixture(t)
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 0 || !strings.Contains(result.Stdout, "low-level roundtrip passed") {
					t.Errorf("generated run_js failed: %+v", result)
				}
				return agentModelReply{Content: "fixture complete"}
			}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("low-level", "// Exercise indexed I/O over TCP.\n"+lowLevelRoundTrip("tcp"))}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	got := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";
const a=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"exercise low-level modules");
if(a.lastResult?.state!=="completed")throw Error(JSON.stringify(a));console.log(a.lastResult.answer);`)
	requireExit(t, got, 0)
	data, err := os.ReadFile(filepath.Join(work, "index.bin"))
	if err != nil || data[4] != 251 || data[7] != 99 || !strings.Contains(got.stdout, "fixture complete") {
		t.Fatalf("fixture did not execute run_js: data=%v err=%v result=%+v", data, err, got)
	}
}
