package stdlib_test

import (
	"context"
	json "encoding/json/v2"
	"fmt"
	"io"
	"os"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestAwaitUsingFileHandleDrainsWritesAndClosesOnThrow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:file-disposal", Virtual: true, Module: true, Code: `
import {open,readFile} from "node:fs/promises";
let file,writing;
{
  await using owned=await open("value","wx+");
  file=owned;
  writing=file.write("retained bytes");
}
if ((await writing).bytesWritten!==14 || await readFile("value","utf8")!=="retained bytes") throw Error("disposal lost an admitted write");
if ((await file.stat().then(()=>null,error=>error))?.code!=="ERR_FILE_CLOSED") throw Error("file survived disposal");
if(await file[Symbol.asyncDispose]()!==undefined) throw Error("disposal returned a value");
const reason={body:"expected"};
let failed;
try {
  await using owned=await open("value");
  failed=owned;
  throw reason;
} catch (error) { if (error!==reason) throw error; }
if ((await failed.read().then(()=>null,error=>error))?.code!=="ERR_FILE_CLOSED") throw Error("exception bypassed disposal");
`}, stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: t.TempDir()}}})
	if err != nil || result.Code != 0 {
		t.Fatalf("file disposal result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestAsyncDisposalProcessHelper(t *testing.T) {
	if os.Getenv("NOE_ASYNC_DISPOSAL_HELPER") != "1" {
		return
	}
	if _, err := fmt.Fprint(os.Stdout, "ready"); err != nil {
		os.Exit(8)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		os.Exit(9)
	}
	os.Exit(0)
}

func TestAwaitUsingChildClosesLiveProcessOnThrow(t *testing.T) {
	program, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(program)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:child-disposal", Virtual: true, Module: true, Code: `
import {spawn} from "node:child_process";
let child;
const reason={body:"expected"};
try {
  await using owned=await spawn(` + string(encoded) + `,["-test.run=^TestAsyncDisposalProcessHelper$"],{
    env:{NOE_ASYNC_DISPOSAL_HELPER:"1"},stderr:"ignore"
  });
  child=owned;
  const reader=child.stdout.getReader();
  try { if(new TextDecoder().decode((await reader.read()).value)!=="ready") throw Error("child not running"); }
  finally { reader.releaseLock(); }
  throw reason;
} catch (error) { if(error!==reason) throw error; }
const status=await child.exited;
if(status.code===null && status.signal===null) throw Error("child was not reaped");
await child[Symbol.asyncDispose]();
`}, stdlib.Profile{Modules: stdlib.Modules{Commands: &stdlib.CommandOptions{Allow: func(name string, _ []string) error {
		if name != program {
			return fmt.Errorf("unexpected command %q", name)
		}
		return nil
	}}}})
	if err != nil || result.Code != 0 {
		t.Fatalf("child disposal result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestAwaitUsingNetworkAndSignalResourcesReleaseWaiters(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "memory:native-disposal", Virtual: true, Module: true, Code: `
import {listen,connect} from "noe:sockets";
import {subscribe} from "noe:signals";
let pendingAccept,pendingRead,reader,subscription,pendingSignal;
{
  await using listener=await listen({network:"tcp",host:"127.0.0.1",port:0});
  const accepting=listener.accept();
  {
    await using outgoing=await connect(listener.address);
    await using incoming=await accepting;
    const writer=outgoing.writable.getWriter();
    try { await writer.write(new Uint8Array([42])); } finally { writer.releaseLock(); }
    reader=incoming.readable.getReader();
    if((await reader.read()).value[0]!==42) throw Error("socket was not live");
    pendingRead=reader.read().then(()=>false,()=>true);
  }
  if(!await pendingRead) throw Error("socket read survived disposal");
  reader.releaseLock();
  pendingAccept=listener.accept().then(()=>false,()=>true);
}
if(!await pendingAccept) throw Error("listener accept survived disposal");
{
  await using owned=subscribe("SIGINT");
  subscription=owned;
  pendingSignal=subscription.next();
}
if(!(await pendingSignal).done) throw Error("signal iterator survived disposal");
await subscription[Symbol.asyncDispose]();
`}, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true, Signals: true}})
	if err != nil || result.Code != 0 {
		t.Fatalf("native disposal result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}
