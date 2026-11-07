package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const webStoragePrelude = `
function check(ok,message){if(!ok)throw Error(message)}
function throws(fn,name){let error;try{fn()}catch(caught){error=caught}check(error!==undefined,"expected "+(name||"an exception"));if(name)check(error.name===name,"exception name "+error.name+" expected "+name);return error}
`

func executeWebStorage(t *testing.T, profile stdlib.Profile, code string) stdlib.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[Web Storage]", Virtual: true, Module: true, Code: webStoragePrelude + code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return result
}

func TestWebStorageSessionAndPublicInterfaceSemantics(t *testing.T) {
	executeWebStorage(t, stdlib.Profile{}, `
check(typeof Storage==="function","Storage constructor missing");
check(typeof localStorage==="undefined","default profile granted localStorage");
const storage=sessionStorage;
check(storage===sessionStorage,"sessionStorage identity changed");
check(Object.getPrototypeOf(storage)===Storage.prototype,"Storage prototype");
check(Object.prototype.toString.call(storage)==="[object Storage]","Storage toStringTag");
const globalDescriptor=Object.getOwnPropertyDescriptor(globalThis,"sessionStorage");
check(globalDescriptor&&typeof globalDescriptor.get==="function"&&globalDescriptor.set===undefined,"sessionStorage must be getter-only");
throws(()=>new Storage(),"TypeError");
throws(()=>structuredClone(storage),"DataCloneError");

storage.clear();
check(storage.setItem(null,undefined)===undefined,"setItem result");
storage.setItem(true,17n);
storage.setItem({toString(){return "object-key"}}, {toString(){return "object-value"}});
check(storage.getItem("null")==="undefined","DOMString null/undefined conversions");
check(storage.getItem("true")==="17","DOMString boolean/BigInt conversions");
check(storage.getItem("object-key")==="object-value","DOMString object conversions");
throws(()=>storage.getItem(Symbol("key")),"TypeError");
throws(()=>storage.setItem(Symbol("key"),"value"),"TypeError");
throws(()=>storage.setItem("key",Symbol("value")),"TypeError");
check(storage.getItem("key")===null,"Symbol conversion partially stored an item");

throws(()=>storage.getItem(),"TypeError");
throws(()=>storage.key(),"TypeError");
throws(()=>storage.setItem("one"),"TypeError");
throws(()=>storage.removeItem(),"TypeError");
throws(()=>Storage.prototype.getItem.call({},"one"),"TypeError");
const lengthGetter=Object.getOwnPropertyDescriptor(Storage.prototype,"length").get;
throws(()=>lengthGetter.call({}),"TypeError");

storage.clear();
storage.setItem("first","1");storage.setItem("second","2");storage.setItem("third","3");
storage.setItem("first","updated");
check(storage.key(0)==="first"&&storage.key(1)==="second"&&storage.key(2)==="third","update changed key order");
check(storage.key(-1)===null&&storage.key(4294967297)==="second","key index must use unsigned 32-bit conversion");
throws(()=>storage.key(Symbol("index")),"TypeError");
check(storage.getItem("missing")===null&&storage.missing===undefined,"missing-item return values");

storage.named="one";
check(storage.getItem("named")==="one"&&storage.named==="one","named assignment did not write storage");
const namedDescriptor=Object.getOwnPropertyDescriptor(storage,"named");
check(namedDescriptor&&namedDescriptor.value==="one"&&namedDescriptor.writable&&namedDescriptor.enumerable&&namedDescriptor.configurable,"named property descriptor");
check(Object.keys(storage).includes("named"),"named property was not enumerable");
storage.named="updated";
check(storage.getItem("named")==="updated","named assignment did not update item");
check(delete storage.named&&storage.getItem("named")===null,"named deletion did not remove item");
Object.defineProperty(storage,"defined",{value:"from descriptor",writable:true,enumerable:true,configurable:true});
check(storage.getItem("defined")==="from descriptor","named defineProperty did not write item");
check(Object.getOwnPropertyDescriptor(storage,"defined").value==="from descriptor","defined named property descriptor");
Object.defineProperty(storage,"undefined-value",{value:undefined,configurable:true});
Object.defineProperty(storage,"missing-value",{writable:true,configurable:true});
check(storage.getItem("undefined-value")==="undefined"&&storage.getItem("missing-value")==="undefined","named defineProperty must convert an absent/undefined value");

const getItemMethod=storage.getItem;
storage.getItem="hidden named value";
check(storage.getItem===getItemMethod&&storage.getItem("getItem")==="hidden named value","named setter overrode a prototype method");
check(Object.getOwnPropertyDescriptor(storage,"getItem")===undefined,"method collision became an own property");
check(delete storage.getItem&&storage.getItem("getItem")==="hidden named value","hidden method-name item deletion behavior");
storage.removeItem("getItem");

for(const name of ["length","key","getItem","setItem","removeItem","clear"]){
  const before=storage[name];
  storage.setItem(name,"stored item");
  check(storage.getItem(name)==="stored item","method collision item "+name+" missing");
  if(name==="length")check(storage[name]===before+1,"named item hid Storage length");
  else check(storage[name]===before,"named item hid Storage member "+name);
}
storage.removeItem("first");
check(storage.getItem("first")===null,"removeItem failed");
check(storage.removeItem("missing")===undefined,"removeItem result");
check(storage.clear()===undefined,"clear result");
check(storage.length===0,"clear failed");
`)
}

func TestLocalStoragePersistsAcrossExecutionsAndUsesRootCwd(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/workspace"},
		Modules: stdlib.Modules{
			FS:           &stdlib.FileSystemOptions{Root: root},
			LocalStorage: &stdlib.LocalStorageOptions{Path: "state/local.json"},
		},
	}
	executeWebStorage(t, profile, `
check(localStorage instanceof Storage,"localStorage Storage brand");
throws(()=>structuredClone(localStorage),"DataCloneError");
const localDescriptor=Object.getOwnPropertyDescriptor(globalThis,"localStorage");
check(localDescriptor&&typeof localDescriptor.get==="function"&&localDescriptor.set===undefined,"localStorage must be getter-only");
check(sessionStorage.length===0,"initial sessionStorage is not empty");
localStorage.setItem("before","kept");
localStorage.setItem("surrogate-\ud800-key","value-\udfff");
sessionStorage.setItem("private","first runtime");
`)
	file := filepath.Join(workspace, "state", "local.json")
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("relative Cwd storage path was not created at %q: %v", file, err)
	}
	if _, err := os.Stat(filepath.Join(root, "state", "local.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("storage path ignored execution Cwd: stat error=%v", err)
	}
	executeWebStorage(t, profile, `
check(sessionStorage.length===0&&sessionStorage.getItem("private")===null,"sessionStorage leaked across executions");
check(localStorage.getItem("before")==="kept","localStorage did not persist across executions");
check(localStorage.getItem("surrogate-\ud800-key")==="value-\udfff","unpaired UTF-16 surrogate changed across executions");
check(localStorage.key(0)==="before"&&localStorage.key(1)==="surrogate-\ud800-key","persistent insertion order");
localStorage.setItem("after","committed");
`)
	// A completed execution must release its file handles; removing the file
	// also verifies that a later runtime treats it as empty and recreates it.
	removed := file + ".removed"
	if err := os.Rename(file, removed); err != nil {
		t.Fatalf("rename storage file after execution cleanup: %v", err)
	}
	if err := os.Remove(removed); err != nil {
		t.Fatalf("remove storage file after execution cleanup: %v", err)
	}
	executeWebStorage(t, profile, `
check(localStorage.length===0,"missing storage file did not start empty");
localStorage.setItem("fresh","recreated");
`)
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("later runtime did not recreate removed storage file: %v", err)
	}
}

func TestLocalStorageReadOnlyInvalidDataAndFailedCommitPreserveState(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "state"), 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "state", "store.json")
	profile := stdlib.Profile{Environment: stdlib.Environment{Cwd: "/"}, Modules: stdlib.Modules{
		FS:           &stdlib.FileSystemOptions{Root: root},
		LocalStorage: &stdlib.LocalStorageOptions{Path: "state/store.json"},
	}}
	executeWebStorage(t, profile, `localStorage.setItem("saved","original");`)
	original, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}

	readOnly := profile
	readOnly.Modules.FS = &stdlib.FileSystemOptions{Root: root, ReadOnly: true}
	executeWebStorage(t, readOnly, `
check(localStorage.getItem("saved")==="original","read-only profile could not read storage");
throws(()=>localStorage.setItem("saved","changed"),"SecurityError");
check(localStorage.getItem("saved")==="original","failed read-only write changed storage");
sessionStorage.setItem("allowed","session storage remains writable");
`)
	afterReadOnly, err := os.ReadFile(file)
	if err != nil || string(afterReadOnly) != string(original) {
		t.Fatalf("read-only mutation changed previous file: err=%v", err)
	}

	badFile := filepath.Join(root, "state", "invalid.json")
	bad := []byte("{not a storage document")
	if err := os.WriteFile(badFile, bad, 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := profile
	invalid.Modules.LocalStorage = &stdlib.LocalStorageOptions{Path: "state/invalid.json"}
	executeWebStorage(t, invalid, `throws(()=>localStorage.setItem("replacement","bad"),"InvalidStateError");`)
	afterInvalid, err := os.ReadFile(badFile)
	if err != nil || string(afterInvalid) != string(bad) {
		t.Fatalf("invalid storage data was reset or changed: err=%v", err)
	}

	missingDir := filepath.Join(root, "not-created")
	failedWrite := profile
	failedWrite.Modules.LocalStorage = &stdlib.LocalStorageOptions{Path: "not-created/store.json"}
	executeWebStorage(t, failedWrite, `
const failure=throws(()=>localStorage.setItem("unsaved","value"));
check(failure.code==="ENOENT","filesystem error code was not preserved: "+failure.code);
check(localStorage.getItem("unsaved")===null,"failed commit fell back to in-memory storage");
`)
	if _, err := os.Stat(missingDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("localStorage created a missing parent directory: %v", err)
	}
}

func TestLocalStorageRequiresFSAndRejectsOutwardSymlinks(t *testing.T) {
	if _, err := stdlib.NewRuntime(t.Context(), stdlib.Profile{Modules: stdlib.Modules{LocalStorage: &stdlib.LocalStorageOptions{Path: "state.json"}}}); err == nil || !strings.Contains(err.Error(), "requires FS") {
		t.Fatalf("localStorage without FS error=%v", err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root, outside := t.TempDir(), t.TempDir()
	outsideFile := filepath.Join(outside, "store.json")
	if err := os.WriteFile(outsideFile, []byte("outside remains untouched"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	_, err := stdlib.NewRuntime(t.Context(), stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}, LocalStorage: &stdlib.LocalStorageOptions{Path: "escape/store.json"}}})
	if err == nil {
		t.Fatal("localStorage accepted an outward symlink")
	}
	got, readErr := os.ReadFile(outsideFile)
	if readErr != nil || string(got) != "outside remains untouched" {
		t.Fatalf("outward symlink target changed: data=%q err=%v", got, readErr)
	}
}

func TestWebStorageWorkerIsolationSharedLocalUpdatesAndProfileSnapshot(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "work"), 0o700); err != nil {
		t.Fatal(err)
	}
	options := &stdlib.LocalStorageOptions{Path: "state.json"}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/work"},
		Modules: stdlib.Modules{
			FS:           &stdlib.FileSystemOptions{Root: root},
			LocalStorage: options,
			Workers:      &stdlib.WorkerOptions{Max: 2},
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	runtime, err := stdlib.NewRuntime(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	options.Path = "changed.json"
	code := webStoragePrelude + `
import {Worker} from "node:worker_threads";
sessionStorage.setItem("parent","private to parent");
localStorage.setItem("seed","parent value");
const workerSource=` + fmt.Sprintf("%q", `import {parentPort} from "node:worker_threads";
const id=await new Promise(resolve=>parentPort.once("message",resolve));
const local=localStorage.getItem("seed"),session=sessionStorage.length;
sessionStorage.setItem("only-"+id,"worker");
parentPort.postMessage({kind:"ready",id,local,session});
await new Promise(resolve=>parentPort.once("message",resolve));
for(let i=0;i<12;i++)localStorage.setItem("worker-"+id+"-"+i,String(i));
parentPort.postMessage({kind:"done",id,own:sessionStorage.getItem("only-"+id),other:sessionStorage.getItem("only-"+(1-id))});`) + `;
const workers=[0,1].map(()=>new Worker(workerSource,{eval:true,type:"module"}));
const nextMessage=worker=>new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
const ready=workers.map(nextMessage);
workers.forEach((worker,id)=>worker.postMessage(id));
for(const [id,message] of (await Promise.all(ready)).entries())check(message.kind==="ready"&&message.id===id&&message.local==="parent value"&&message.session===0,"Worker storage ownership/read: "+JSON.stringify(message));
const done=workers.map(nextMessage);
workers.forEach(worker=>worker.postMessage("write"));
for(const [id,message] of (await Promise.all(done)).entries())check(message.kind==="done"&&message.id===id&&message.own==="worker"&&message.other===null,"Worker write/session isolation");
check(sessionStorage.getItem("parent")==="private to parent","Worker changed parent sessionStorage");
for(let id=0;id<2;id++)for(let i=0;i<12;i++)check(localStorage.getItem("worker-"+id+"-"+i)===String(i),"lost shared Worker update "+id+"/"+i);
check(localStorage.length===25,"unexpected localStorage item count: "+localStorage.length);
await Promise.all(workers.map(worker=>worker.terminate()));
`
	result, runErr := runtime.Run(stdlib.Source{Name: "[storage workers]", Virtual: true, Module: true, Code: code})
	closeErr := runtime.Close()
	if runErr != nil || closeErr != nil || result.Code != 0 {
		t.Fatalf("result=%+v run error=%s close error=%v", result, stdlib.Diagnostic(runErr), closeErr)
	}
	stateFile := filepath.Join(root, "work", "state.json")
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("runtime did not keep the snapshotted relative storage path: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "work", "changed.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("mutating the caller's options changed the active runtime path: %v", err)
	}
	profile.Modules.LocalStorage = &stdlib.LocalStorageOptions{Path: "state.json"}
	executeWebStorage(t, profile, `
check(sessionStorage.length===0,"new runtime inherited Worker or parent session data");
check(localStorage.getItem("seed")==="parent value","main/Worker localStorage path did not share committed state");
for(let id=0;id<2;id++)for(let i=0;i<12;i++)check(localStorage.getItem("worker-"+id+"-"+i)===String(i),"Worker update was not committed");
`)
}
