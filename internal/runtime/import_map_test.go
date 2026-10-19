package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noe/internal/modulemap"
)

func importMapFixture(t *testing.T) (string, *modulemap.Snapshot) {
	t.Helper()
	dir := t.TempDir()
	for name, source := range map[string]string{
		"vendor/lib.mjs": `import {value} from "./dep.js"; export {value}; export const marker={};`,
		"vendor/dep.js":  `export const value=42;`,
		"maps/map.json":  `{"imports":{"app/lib":"../vendor/lib.mjs","app/again":"../vendor/lib.mjs"}}`,
	} {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := modulemap.Load(filepath.Join(dir, "maps/map.json"))
	if err != nil {
		t.Fatal(err)
	}
	return dir, snapshot
}

func TestImportMapRuntimeAndInMemoryEntry(t *testing.T) {
	dir, snapshot := importMapFixture(t)
	var out bytes.Buffer
	source := `import * as lib from "app/lib"; export {value} from "app/lib";
 const again=await import("app/again");
 console.log(lib.value,lib===again,typeof (await import("node:fs")).readFileSync);`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "consumer/main.js"), source}, Options{Cwd: dir, Stdout: &out, ImportMap: snapshot})
	if err != nil || code != 0 || out.String() != "42 true function\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}

	entry := filepath.Join(dir, "entry.js")
	if err := os.WriteFile(entry, []byte(`throw Error("disk entry");`), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]any{"imports": map[string]string{"app/entry": entry}})
	snapshot, err = modulemap.Parse(data, dir)
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	code, err = Execute(context.Background(), Source{entry, `import * as self from "app/entry"; export const value=7; console.log(self.value);`}, Options{Stdout: &out, ImportMap: snapshot})
	if err != nil || code != 0 || out.String() != "7\n" {
		t.Fatalf("entry: code=%d err=%v out=%q", code, err, out.String())
	}
}

func TestImportMapCachesCanonicalSourceWithinExecution(t *testing.T) {
	dir, snapshot := importMapFixture(t)
	source := `const first=await import("app/lib");
const {unlinkSync}=await import("node:fs");
unlinkSync(` + jsString(t, filepath.Join(dir, "vendor/lib.mjs")) + `);
const second=await import("app/again"), third=await import("app/lib");
if(first!==second || second!==third)throw Error("canonical module cache lost");`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "cache.mjs"), source}, Options{Cwd: dir, ImportMap: snapshot})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestImportMapWorkersAndBuiltinRestrictions(t *testing.T) {
	dir, snapshot := importMapFixture(t)
	worker := filepath.Join(dir, "worker.mjs")
	if err := os.WriteFile(worker, []byte(`import {value} from "app/lib"; import {parentPort} from "node:worker_threads";
 let unsupported=false;try{await import("noe:agent")}catch(e){unsupported=String(e).includes("not supported in Workers")}
 parentPort.postMessage({value,unsupported});`), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import {Worker} from "node:worker_threads";
 const worker=new Worker("./worker.mjs");
 const result=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
 console.log(JSON.stringify(result));await worker.terminate();`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := Execute(ctx, Source{filepath.Join(dir, "main.js"), source}, Options{Cwd: dir, Stdout: &out, ImportMap: snapshot})
	if err != nil || code != 0 || strings.TrimSpace(out.String()) != `{"value":42,"unsupported":true}` {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}
}

func TestImportMapWorkerRetainsImportedLibrary(t *testing.T) {
	dir, snapshot := importMapFixture(t)
	worker := filepath.Join(dir, "worker.mjs")
	source := `import * as first from "app/lib"; import {parentPort} from "node:worker_threads";
 parentPort.on("message",async()=>{const second=await import("app/again");parentPort.postMessage(first===second);parentPort.close()});
 parentPort.postMessage("ready");`
	if err := os.WriteFile(worker, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	main := `import {Worker} from "node:worker_threads";import {unlinkSync} from "node:fs";
 const worker=new Worker("./worker.mjs");
 await new Promise((resolve,reject)=>{worker.on("error",reject);worker.on("message",message=>{if(message==="ready"){unlinkSync("vendor/lib.mjs");worker.postMessage("continue")}else{if(message!==true)reject(Error("namespace changed"));else resolve()}})});
 await worker.terminate();console.log("retained");`
	libraryPath, _ := json.Marshal(filepath.Join(dir, "vendor/lib.mjs"))
	main = strings.Replace(main, `"vendor/lib.mjs"`, string(libraryPath), 1)
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(dir, "main.js"), main}, Options{Cwd: dir, Stdout: &out, ImportMap: snapshot})
	if err != nil || code != 0 || out.String() != "retained\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, out.String())
	}
}
