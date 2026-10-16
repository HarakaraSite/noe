//go:build e2e

package e2e_test

import (
	"path/filepath"
	"testing"
)

func TestStdlibThroughCLI(t *testing.T) {
	work := t.TempDir()
	script := filepath.Join(work, "stdlib.mjs")
	writeFixture(t, script, `import {writeFile,readFile} from "node:fs/promises";
 import {setTimeout as sleep} from "timers/promises";
 import {Worker} from "node:worker_threads";
 if (typeof globalThis.noe !== "undefined") throw new Error("legacy global remains");
 await writeFile("stdlib.txt", "available");
 await sleep(1);
 const worker = new Worker('import {parentPort} from "node:worker_threads"; parentPort.postMessage(42)', {eval:true,type:"module"});
 const answer = await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
 console.log(await readFile("stdlib.txt","utf8"),answer,process.argv.slice(2).join(","));`)
	got := runCLI(t, work, nil, "run", script, "one", "two")
	requireExit(t, got, 0)
	if got.stdout != "available 42 one,two\n" || got.stderr != "" {
		t.Fatalf("stdlib CLI=%+v", got)
	}
}

func TestStdlibCommandTimeoutThroughCLI(t *testing.T) {
	for _, call := range []string{`execFileSync(process.argv[0],["eval","while(true){}"])`, `await execFile(process.argv[0],["eval","while(true){}"])`} {
		source := `import {execFile,execFileSync} from "node:child_process";console.log("STARTED");` + call + `;console.log("AFTER");`
		requireExecutionTimeout(t, runCLI(t, t.TempDir(), nil, "eval", "--timeout=200ms", source))
	}
}
