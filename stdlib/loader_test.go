package stdlib_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestFileLoaderUsesRootedFilesystemAndFreshCachesForEntryAndWorker(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "workspace", "lib")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	main := filepath.Join(dir, "main.mjs")
	if err := os.WriteFile(main, []byte(`import {value} from "./value.mjs";export const result=value+1;`), 0600); err != nil {
		t.Fatal(err)
	}
	worker := `import {parentPort} from "node:worker_threads";import {result} from "calc";import * as self from "worker-self";export const marker=7;if(self.marker!==7)throw Error("worker entry identity");parentPort.postMessage(result);parentPort.close();`
	if err := os.WriteFile(filepath.Join(root, "workspace", "worker.mjs"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	mappings := map[string]string{"calc": "./lib/main.mjs", "self": "./entry.mjs", "worker-self": "./worker.mjs"}
	factory := stdlib.FileLoader(mappings)
	mappings["calc"] = "./missing.mjs"
	profile := stdlib.Profile{Environment: stdlib.Environment{Cwd: "/workspace"}, Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}, Workers: &stdlib.WorkerOptions{Max: 1}}, NewLoader: factory}
	for _, value := range []int{40, 41} {
		if err := os.WriteFile(filepath.Join(dir, "value.mjs"), []byte(fmt.Sprintf("export const value=%d", value)), 0600); err != nil {
			t.Fatal(err)
		}
		code := fmt.Sprintf(`import {result} from "calc";import * as self from "self";import * as relative from "./entry.mjs";import {Worker} from "node:worker_threads";export const marker=7;
if(self!==relative||self.marker!==7||result!==%d)throw Error("entry/loader cache");
const worker=new Worker("./worker.mjs",{type:"module"});const resultFromWorker=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});if(resultFromWorker!==result)throw Error("worker loader");await worker.terminate();`, value+1)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "/workspace/entry.mjs", Code: code, Module: true}, profile)
		cancel()
		if err != nil {
			t.Fatal(stdlib.Diagnostic(err))
		}
	}
}
func TestFileLoaderDoesNotGrantFilesystemAndRejectsRootEscape(t *testing.T) {
	profile := stdlib.Profile{NewLoader: stdlib.FileLoader(map[string]string{"self": "./entry.mjs"})}
	_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: `import * as self from "self";export const value=1;if(self.value!==1)throw Error("memory source");`}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: `import "./missing.mjs";`}, profile)
	if err == nil || !strings.Contains(err.Error(), "filesystem is unavailable") {
		t.Fatalf("unselected filesystem=%v", err)
	}
	if runtime.GOOS == "windows" {
		return
	}
	root, outside := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "escape.mjs"), []byte(`export const secret=1;`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "escape.mjs"), filepath.Join(root, "escape.mjs")); err != nil {
		t.Fatal(err)
	}
	profile = stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}, NewLoader: stdlib.FileLoader(nil)}
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "[root escape]", Virtual: true, Module: true, Code: `import "./escape.mjs";`}, profile)
	if err == nil {
		t.Fatal("loader bypassed selected root")
	}
}
