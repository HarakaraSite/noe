package stdlib

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestFSWatchPublicAliasesAndNotifications(t *testing.T) {
	rt, _, loop := fileRuntime(t, t.TempDir())
	evalFiles(t, rt, loop, `
import fs,{watch} from "noe:fs";
import {watch as events} from "noe:fs";
import {Buffer} from "node:buffer";
if(watch!==fs.watch||watch!==events)throw Error("watch exports differ");

fs.writeFileSync("created","initial");
const changes=watch("created");
const changed=changes.next();
fs.appendFileSync("created","updated");
const change=await changed;
if(change.done||change.value.eventType!=="change"||change.value.filename!=="created")throw Error("file change "+JSON.stringify(change));
await changes.return();
if(!(await changes.next()).done)throw Error("iterator return did not close");

const binary=watch(".","buffer");
const next=binary.next();
fs.renameSync("created","renamed");
const renamed=await next;
if(renamed.done||renamed.value.eventType!=="rename"||!Buffer.isBuffer(renamed.value.filename)||!["created","renamed"].includes(renamed.value.filename.toString()))throw Error("buffer rename event");
await binary.close();

const removed=watch(".");
const removal=removed.next();
fs.unlinkSync("renamed");
if((await removal).value.filename!=="renamed")throw Error("remove event");
await removed.close();
`)
}

func TestFSWatchRejectsEventEmitterCallbackForm(t *testing.T) {
	rt, _, loop := fileRuntime(t, t.TempDir())
	evalFiles(t, rt, loop, `
import {watch} from "noe:fs";
let error;try{watch(".",()=>{})}catch(e){error=e}
if(error?.code!=="ERR_INVALID_ARG")throw Error("callback form was not rejected");
`)
}

func TestFSWatchIteratorWithoutLoopOrWebAPIs(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	defer rt.Close()
	if err := Install(rt, Config{NoWebAPIs: true, FS: &FS{Cwd: cwd}}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("mutateWatchedFile", func() error {
		return os.WriteFile(filepath.Join(cwd, "file"), []byte("after"), 0o600)
	}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	entry, err := rt.EvalModuleContext(ctx, "watch-no-loop.mjs", `
import {watch} from "noe:fs";
await using changes=watch("file");
const pending=changes.next();mutateWatchedFile();
const event=await pending;
if(event.done||event.value.eventType!=="change"||event.value.filename!=="file")throw Error("no-loop iterator");
`)
	if err == nil {
		_, err = entry.Await(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestFSWatchCancellationAndDisposal(t *testing.T) {
	rt, _, loop := fileRuntime(t, t.TempDir())
	evalFiles(t, rt, loop, `
		import {watch} from "noe:fs";
const reason={source:"caller"},controller=new AbortController();
const iterator=watch(".",{signal:AbortSignal.any([controller.signal])});
const waiting=iterator.next().then(()=>null,error=>error);
controller.abort(reason);
const failure=await waiting;
if(failure?.name!=="AbortError"||failure.code!=="ABORT_ERR"||failure.cause!==reason)throw Error("abort reason");
if(!(await iterator.next()).done)throw Error("aborted iterator remains open");

const early=watch("missing",{signal:AbortSignal.abort(0)});
const earlyError=await early.next().then(()=>null,error=>error);
if(earlyError?.name!=="AbortError"||earlyError.cause!==0||!(await early.next()).done)throw Error("pre-aborted iterator");

let owned,pending;
{
 await using resource=watch(".");owned=resource;pending=resource.next();
}
if(!(await pending).done||!(await owned.next()).done)throw Error("iterator disposal left pending reads");

let disposed;
try{
 await using resource=watch(".");disposed=resource;
 throw new Error("body");
}catch(error){if(error.message!=="body")throw error}
if(!(await disposed.next()).done)throw Error("disposed iterator remained open");
`)
}

func TestFSWatchOptionsAndErrors(t *testing.T) {
	rt, _, loop := fileRuntime(t, t.TempDir())
	evalFiles(t, rt, loop, `
import fs,{watch} from "noe:fs";
import {watch as events} from "noe:fs";
if(watch!==events)throw Error("watch aliases");
for(const [call,code] of [
 [()=>watch(""),"ERR_INVALID_ARG"],
 [()=>watch("nul\0name"),"ERR_INVALID_ARG"],
 [()=>events(".",{maxQueue:0}),"ERR_INVALID_ARG"],
 [()=>watch(".",{persistent:"yes"}),"ERR_INVALID_ARG"],
 [()=>watch(".",{encoding:"hex"}),"ERR_INVALID_ARG"],
 [()=>events(".",{overflow:"discard"}),"ERR_INVALID_ARG"],
 [()=>watch(".",{recursive:true}),"ERR_NOT_SUPPORTED"],
 [()=>watch(".",{ignore:()=>true}),"ERR_NOT_SUPPORTED"],
 [()=>events("missing",{throwIfNoEntry:false}),"ERR_NOT_SUPPORTED"],
 [()=>watch(".",()=>{}),"ERR_INVALID_ARG"],
]){
 let failure;try{call()}catch(error){failure=error}
 if(failure?.code!==code)throw Error("watch validation "+code+" got "+failure?.code);
}

for(const signal of [null,{},Object.create(AbortSignal.prototype)]){
 let failure;try{events(".",{signal})}catch(error){failure=error}
 if(!(failure instanceof TypeError))throw Error("invalid signal accepted");
}
const marker=new Error("getter");
for(const create of [watch,events]){
 let failure;try{create(".",{get encoding(){throw marker}})}catch(error){failure=error}
 if(failure!==marker)throw Error("option exception identity");
}
const missing=events("missing");
const failure=await missing.next().then(()=>null,error=>error);
if(failure?.code!=="ENOENT"||typeof failure.errno!=="number"||!(await missing.next()).done)throw Error("iterator open error");

// Creating an unused iterator registers nothing and does not keep the loop alive.
events("missing");
const iterator=events(".");
const first=iterator.next(),second=iterator.next();
await iterator.close();
if(!(await first).done||!(await second).done)throw Error("close left concurrent next pending");

// Breaking for-await releases a live watch without requiring explicit close.
const each=events(".");const firstEvent=each.next();fs.writeFileSync("first","value");await firstEvent;
const iterating=(async()=>{for await(const event of each){if(event.filename!=="second")continue;break}})();
fs.writeFileSync("second","value");await iterating;
if(!(await each.next()).done)throw Error("for-await break left watcher open");
`)
}

func TestFSWatchReentrantCloseSettlesPendingReads(t *testing.T) {
	rt, _, loop := fileRuntime(t, t.TempDir())
	evalFiles(t, rt, loop, `
import fs from "noe:fs";
import {watch} from "noe:fs";
const iterator=watch(".");
const first=iterator.next(),second=iterator.next();
let entered=false;
Object.defineProperty(Object.prototype,"then",{configurable:true,get(){
 if(Object.hasOwn(this,"done")&&this.done===false){
  delete Object.prototype.then;
  entered=true;
  iterator.close();
 }
 return undefined;
}});
try{
 fs.writeFileSync("first","one");
 fs.writeFileSync("second","two");
 if(!(await first).done||!(await second).done||!entered)throw Error("reentrant close lost pending reads");
}finally{
 delete Object.prototype.then;
 await iterator.close();
}
`)
}
