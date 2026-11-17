package stdlib_test

import (
	"fmt"
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

// Exercise the upstream compiler fixes through NOE's execution modes, including
// a compiled Script reused in one realm. Expected results also match Node.
func TestEngineCompilerFixes(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   string
	}{
		{"captured block on continue", `(() => {
  const closures = [];
  for (var i = 0; i < 3; i++) {
    let value = i;
    closures.push(() => value);
    continue;
  }
  return closures.map(f => f()).join();
})()`, "0,1,2"},
		{"nested finally on continue", `(() => {
  const log = [];
  for (var i = 0; i < 2; i++) {
    try {
      try { if (i === 0) continue; }
      finally { log.push("a"); if (i === 1) continue; }
    } finally {
      log.push("b");
      try { log.push("c"); } finally { log.push("d"); }
    }
  }
  return log.join();
})()`, "a,b,c,d,a,b,c,d"},
		{"finally completion", `String(eval("for (var i = 0; i < 2; i++) { try { 3; break; } finally { 9; } }"))`, "3"},
		{"computed object rest", `(() => {
  let calls = 0;
  const key = () => { calls++; return "a"; };
  const {[key()]: picked, ...rest} = {a: 1, b: 2};
  return [calls, picked, JSON.stringify(rest)].join();
})()`, `1,1,{"b":2}`},
		{"optional delete", `(() => {
  const object = {branch: {leaf: 1}};
  const removed = delete object?.branch.leaf;
  return [removed, Object.hasOwn(object.branch, "leaf"), delete null?.leaf].join();
})()`, "true,false,true"},
		{"multiple loop labels", `(() => {
  let count = 0;
  outer: inner: for (let i = 0; i < 3; i++) {
    count++;
    continue outer;
  }
  return String(count);
})()`, "3"},
		{"break in labelled block", `(() => {
  let count = 0;
  for (;;) label: {
    if (++count > 3) return "looped";
    break;
  }
  return String(count);
})()`, "1"},
		{"async after line break", `typeof [0,
async function () {}][1]`, "function"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			worker := `import {parentPort} from "node:worker_threads";
parentPort.postMessage(` + tc.source + `);
parentPort.close();`
			out, errOut := run(t, stdlib.Config{
				Workers: workerFiles(map[string]string{"./engine.mjs": worker}),
			}, `console.log("main", `+tc.source+`);
import("node:vm").then(vm => {
  const context = vm.createContext({});
  const script = new vm.Script(`+strconv.Quote(tc.source)+`);
  for (let i = 0; i < 3; i++) {
    const result = script.runInContext(context);
    if (result !== `+strconv.Quote(tc.want)+`) throw new Error("realm result: " + result);
  }
  console.log("realm", script.runInContext(context));
  const worker = new require_worker_threads.Worker("./engine.mjs");
  worker.on("message", result => console.log("worker", result));
});`)
			want := fmt.Sprintf("main %s\nrealm %s\nworker %s", tc.want, tc.want, tc.want)
			if out != want || errOut != "" {
				t.Fatalf("engine results: stdout=%q stderr=%q, want %q", out, errOut, want)
			}
		})
	}
}
