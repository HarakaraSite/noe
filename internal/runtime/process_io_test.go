package runtime

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// This target runs in an ordinary Go test executable, independently of the
// private process supervisor. No Node/Python/system shell is needed.
func TestNoeProcessIOHelper(t *testing.T) {}

func runProcessIOHelper() {
	mode := os.Getenv("NOE_PROCESS_IO_HELPER")
	if mode == "" {
		return
	}
	switch mode {
	case "echo":
		s := bufio.NewScanner(os.Stdin)
		for s.Scan() {
			fmt.Fprintln(os.Stdout, "out:"+s.Text())
			fmt.Fprintln(os.Stderr, "err:"+s.Text())
		}
		if s.Err() != nil {
			os.Exit(8)
		}
		os.Exit(0)
	case "env":
		cwd, _ := os.Getwd()
		fmt.Fprintf(os.Stdout, "%s|%s|%s|%s", cwd, os.Getenv("WORKER_SETTING"), os.Getenv("REMOVED_SETTING"), os.Getenv("CHANGED_SETTING"))
		os.Exit(7)
	case "wait":
		fmt.Fprintln(os.Stdout, "ready")
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		os.Exit(0)
	case "bytes":
		_, _ = os.Stdout.Write([]byte{0, 255, 128, 10})
		os.Exit(0)
	}
	os.Exit(9)
}

func processHelperJS(t *testing.T, mode string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return `spawn(` + jsString(t, exe) + `, ["-test.run=^TestNoeProcessIOHelper$"], {env:{...process.env,NOE_PROCESS_IO_HELPER:` + jsString(t, mode) + `}})`
}

func TestProcessStreamsMultipleLiveRequests(t *testing.T) {
	script := `import {spawn} from "noe:process";
const child=await ` + processHelperJS(t, "echo") + `;
const enc=new TextEncoder(), dec=new TextDecoder();
const writer=child.stdin.getWriter(), out=child.stdout.getReader(), err=child.stderr.getReader();
for (const line of ["first", "雪", "third"]) {
  await writer.write(enc.encode(line+"\n"));
  const [a,b]=await Promise.all([out.read(),err.read()]);
  if(dec.decode(a.value)!=="out:"+line+"\n" || dec.decode(b.value)!=="err:"+line+"\n") throw Error("live response");
}
await writer.close();
const [status,a,b]=await Promise.all([child.exited,out.read(),err.read()]);
if(status.code!==0 || status.signal!==null || !a.done || !b.done) throw Error("exit/EOF");
await child.close(); await child.close(); console.log("live requests passed");`
	if got := runStdlib(t, script, Options{}); got != "live requests passed\n" {
		t.Fatalf("output=%q", got)
	}
}

func TestProcessEnvironmentCwdAndExit(t *testing.T) {
	dir := t.TempDir()
	command := strings.Replace(processHelperJS(t, "env"), `}})`, `},cwd:".",stdin:"ignore"})`, 1)
	script := `import {spawn} from "noe:process";
process.env.REMOVED_SETTING="remove me"; process.env.CHANGED_SETTING="changed";
const child=await ` + strings.Replace(command, `NOE_PROCESS_IO_HELPER:"env"`, `NOE_PROCESS_IO_HELPER:"env",REMOVED_SETTING:null,WORKER_SETTING:"root"`, 1) + `;
const [text,errors]=await Promise.all([new Response(child.stdout).text(),new Response(child.stderr).text()]);
const result=await child.exited; await child.close();
if(text!==` + jsString(t, dir+"|root||changed") + ` || result.code!==7 || result.signal!==null) throw Error(text+JSON.stringify(result)+errors);`
	runStdlib(t, script, Options{Cwd: dir})
}

func TestProcessStartFailureAndValidation(t *testing.T) {
	runStdlib(t, `import {spawn} from "noe:process";
for (const fn of [()=>spawn("noe-command-that-does-not-exist"),()=>spawn("x",[3]),()=>spawn("x",[],{stdout:"invalid"})]) {
let failed=false;try{await fn()}catch(e){failed=true}if(!failed)throw Error("accepted invalid start");
}`, Options{})
}

func TestProcessCloseUnblocksPendingRead(t *testing.T) {
	runStdlib(t, `import {spawn} from "noe:process";
const child=await `+processHelperJS(t, "wait")+`;
const reader=child.stdout.getReader(); await reader.read();
const read=reader.read().catch(()=>({done:true}));
await child.close(); await read; const result=await child.exited;
if(result.code===null && result.signal===null)throw Error("missing exit status");`, Options{})
}

func TestNoeIOBorrowedInputAndOutput(t *testing.T) {
	input := strings.NewReader("one\ntwo\n")
	out := runStdlib(t, `import {stdin,stdout,pipe} from "noe:io";
await pipe(stdin,stdout); console.log("legacy still works");`, Options{Stdin: input})
	if out != "one\ntwo\nlegacy still works\n" {
		t.Fatalf("out=%q", out)
	}
}

func TestProcessModuleInWorkerUsesOwnEnvironment(t *testing.T) {
	dir := t.TempDir()
	worker := `import {parentPort} from "node:worker_threads"; import {spawn} from "noe:process";
const c=await ` + processHelperJS(t, "env") + `; const text=await new Response(c.stdout).text(); await c.exited;await c.close(); parentPort.postMessage(text); parentPort.close();`
	if err := os.WriteFile(filepath.Join(dir, "worker.js"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	script := `import {Worker} from "node:worker_threads";
const w=new Worker("./worker.js",{env:{WORKER_SETTING:"isolated"}});
const message=await new Promise((resolve,reject)=>{w.on("message",resolve);w.on("error",reject)});
if(!message.endsWith("|isolated||"))throw Error(message); await w.terminate();`
	runStdlib(t, script, Options{Cwd: dir})
}

func TestProcessIOSharedOutputAliasesAcrossWorkerAndLegacy(t *testing.T) {
	var output bytes.Buffer
	dir := t.TempDir()
	const writes = 50
	worker := `import {parentPort} from "node:worker_threads"; import {stdout,stderr} from "noe:io";
const a=stdout.getWriter(), b=stderr.getWriter(), chunk=new TextEncoder().encode("w".repeat(4096));
await Promise.all([0,1].map(async i=>{for(let n=0;n<50;n++)await (i?b:a).write(chunk)}));
for(let n=0;n<50;n++)process.stdout.write("l".repeat(4096));parentPort.postMessage("done");parentPort.close();`
	if err := os.WriteFile(filepath.Join(dir, "worker.js"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import {Worker} from "node:worker_threads"; import {stdout,stderr} from "noe:io";
const worker=new Worker("./worker.js");
const done=new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
const a=stdout.getWriter(),b=stderr.getWriter(),chunk=new TextEncoder().encode("m".repeat(4096));
await Promise.all([done,...[0,1].map(async i=>{for(let n=0;n<50;n++)await (i?b:a).write(chunk)})]);
for(let n=0;n<50;n++)process.stderr.write("l".repeat(4096));await worker.terminate();`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(dir, "main.js"), source}, Options{Stdout: &output, Stderr: &output, Cwd: dir})
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
	if output.Len() != 6*writes*4096 {
		t.Fatalf("lost output: %d bytes", output.Len())
	}
}
