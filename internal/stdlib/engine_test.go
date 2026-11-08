package stdlib_test

import (
	"strconv"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

// This loop returned "e,e" with go-quickjs v0.32.0: the tree tier treated
// continue before a later break condition as an exit from the inner loop.
// Exercise NOE's main, Worker and repeatedly executed realm paths together.
func TestEngineNestedLoopContinue(t *testing.T) {
	const source = `
function loopResult() {
  const log = [];
  for (let i = 0; i < 2; i++) {
    for (const y of [1, 2, 3]) {
      if (y === 1) continue;
      if (y === 9) break;
      log.push(y);
    }
    log.push("e");
  }
  return log.join();
}
`
	worker := `import {parentPort} from "node:worker_threads";` + source + `
parentPort.postMessage(loopResult());
parentPort.close();`
	out, errOut := run(t, stdlib.Config{
		Workers: workerFiles(map[string]string{"./loop.mjs": worker}),
	}, source+`
console.log("main", loopResult());
import("node:vm").then(vm => {
  const context = vm.createContext({});
  const script = new vm.Script(`+strconv.Quote(source+`loopResult()`)+`);
  for (let i = 0; i < 3; i++) {
    const result = script.runInContext(context);
    if (result !== "2,3,e,2,3,e") throw new Error("realm loop: " + result);
  }
  console.log("realm", script.runInContext(context));
  const worker = new require_worker_threads.Worker("./loop.mjs");
  worker.on("message", result => console.log("worker", result));
});`)
	want := "main 2,3,e,2,3,e\nrealm 2,3,e,2,3,e\nworker 2,3,e,2,3,e"
	if out != want || errOut != "" {
		t.Fatalf("loop results: stdout=%q stderr=%q, want %q", out, errOut, want)
	}
}
