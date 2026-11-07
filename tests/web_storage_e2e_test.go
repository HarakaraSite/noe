//go:build e2e

package e2e_test

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestWebStorageCLIProcessPersistenceAndWorker(t *testing.T) {
	work := t.TempDir()
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"eval", `console.log(typeof Storage, typeof localStorage, sessionStorage.length)`}, "function undefined 0\n"},
		{[]string{"eval", "--local-storage=state.json", `sessionStorage.x="temporary";localStorage.setItem("counter","1");localStorage.setItem("surrogate","\ud800");console.log(localStorage.counter)`}, "1\n"},
		{[]string{"eval", "--local-storage", "state.json", `if(sessionStorage.length!==0||localStorage.getItem("surrogate")!=="\ud800")throw Error("persistence");console.log(localStorage.counter)`}, "1\n"},
	} {
		result := runCLI(t, work, nil, tc.args...)
		requireExit(t, result, 0)
		if result.stdout != tc.want || result.stderr != "" {
			t.Fatalf("args=%q result=%+v", tc.args, result)
		}
	}
	writeFixture(t, filepath.Join(work, "worker.mjs"), `
import {parentPort} from "node:worker_threads";
if(sessionStorage.length!==0)throw Error("shared session");
localStorage.counter=String(Number(localStorage.counter)+1);
parentPort.postMessage(localStorage.counter);parentPort.close();`)
	result := runCLI(t, work, nil, "eval", "--local-storage=state.json", `
import {Worker} from "node:worker_threads";
sessionStorage.parent="value";
const worker=new Worker("./worker.mjs");
const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();
if(result!=="2"||localStorage.counter!=="2"||sessionStorage.parent!=="value")throw Error("worker storage");
console.log("Worker storage passed");`)
	requireExit(t, result, 0)
	if result.stdout != "Worker storage passed\n" || result.stderr != "" {
		t.Fatalf("worker result=%+v", result)
	}
}

func TestWebStorageExample(t *testing.T) {
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "storage", "web-storage.js")
	for count := 1; count <= 2; count++ {
		result := runCLI(t, work, nil, "run", "--local-storage=example.json", example)
		requireExit(t, result, 0)
		want := fmt.Sprintf("session: This value belongs to this runtime.\npersistent runs: %d\n", count)
		if result.stdout != want || result.stderr != "" {
			t.Fatalf("execution %d result=%+v", count, result)
		}
	}
}
