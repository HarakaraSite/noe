package stdlib_test

import (
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

var streamIteratorCases = []struct {
	name string
	code string
}{
	{"terminal", `
for (const terminal of ["close", "error"]) {
  const reason = {terminal};
  const stream = new ReadableStream({start(controller) {
    if (terminal === "close") {controller.enqueue(42); controller.close();}
    else controller.error(reason);
  }});
  const values = []; let error;
  try {for await (const value of stream) values.push(value);} catch (value) {error = value;}
  check(!stream.locked, "iteration releases the lock after " + terminal);
  check(terminal === "close" ? values[0] === 42 && !error : error === reason, "terminal result");
  const reader = stream.getReader(); reader.releaseLock();
}
const stream = new ReadableStream({start(controller) {controller.close();}});
const iterator = stream.values();
check(stream.locked, "values acquires the lock immediately");
check((await iterator.next()).done && (await iterator.next()).done, "repeated EOF");
check((await iterator.return(7)).value === 7 && !stream.locked, "return after EOF");
`},
	{"cancel", `
for (const failure of [false, true]) {
  const reason = {failure}; let finish, received, calls = 0, settled = false;
  const stream = new ReadableStream({cancel(value) {
    calls++; received = value;
    return new Promise((resolve, reject) => {finish = () => failure ? reject(reason) : resolve();});
  }});
  const iterator = stream.values();
  const returned = iterator.return(reason).then(value => {settled = true; return {value};}, error => {settled = true; return {error};});
  check(!stream.locked && calls === 1 && received === reason, "cancel releases the lock before cleanup settles");
  await Promise.resolve(); check(!settled, "return waits for cleanup");
  finish(); const result = await returned;
  check(failure ? result.error === reason : result.value.done && result.value.value === reason, "cleanup result and identity");
  check((await iterator.next()).done && (await iterator.return()).done && calls === 1, "cancel runs once");
}
const reason = {sync: true};
const stream = new ReadableStream({cancel() {throw reason;}});
let error; try {await stream.values().return();} catch (value) {error = value;}
check(error === reason && !stream.locked, "synchronous cancellation failure propagates");
const breaking = new ReadableStream({start(c) {c.enqueue(1);}, cancel() {throw reason;}});
error = undefined;
try {for await (const value of breaking) {break;}} catch (value) {error = value;}
check(error === reason && !breaking.locked, "for-await break propagates cleanup failure");
`},
	{"prevent-cancel", `
let calls = 0, reads = 0;
const options = {get preventCancel() {reads++; return true;}};
const stream = new ReadableStream({start(c) {c.enqueue(1); c.enqueue(2);}, cancel() {calls++;}});
const iterator = stream.values(options);
check(reads === 1, "preventCancel is captured at iterator creation");
Object.defineProperty(options, "preventCancel", {value: false});
check((await iterator.next()).value === 1, "first chunk");
await iterator.return();
check(!stream.locked && calls === 0, "preventCancel releases without cancelling");
const reader = stream.getReader();
check((await reader.read()).value === 2, "remaining data stays readable");
await reader.cancel(); reader.releaseLock();
check(calls === 1, "new owner can cancel");
`},
	{"overlapping", `
for (const failure of [false, true]) {
  let controller, cancelled = 0;
  const reason = {failure};
  const stream = new ReadableStream({start(c) {controller = c;}, cancel() {cancelled++;}}, {highWaterMark: 0});
  const iterator = stream.values();
  const first = iterator.next(), second = iterator.next(), returned = iterator.return("stop"), after = iterator.next();
  check(cancelled === 0 && stream.locked, "return waits for earlier next calls");
  controller.enqueue(1);
  check((await first).value === 1 && cancelled === 0, "first read completes before cancellation");
  if (failure) controller.error(reason); else controller.enqueue(2);
  const result = await second.then(value => ({value}), error => ({error}));
  check(failure ? result.error === reason : result.value.value === 2, "second read result");
  check((await returned).value === "stop" && (await after).done, "queued return and completed next");
  check(!stream.locked && cancelled === (failure ? 0 : 1), "queued terminal lock and cancellation");
}
`},
}

func TestStreamAsyncIterator(t *testing.T) {
	for _, fixture := range streamIteratorCases {
		t.Run(fixture.name, func(t *testing.T) {
			executeWebBasics(t, stdlib.Profile{}, fixture.code)
		})
	}
}

func TestStreamAsyncIteratorWorker(t *testing.T) {
	worker := webBasicsPrelude + "\nimport {parentPort} from 'node:worker_threads';\n"
	for _, fixture := range streamIteratorCases {
		worker += "await (async () => {\n" + fixture.code + "\n})();\n"
	}
	worker += "parentPort.postMessage('passed'); parentPort.close();"
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker = new Worker(%q, {eval: true, type: "module"});
const result = await new Promise((resolve, reject) => {worker.once("message", resolve); worker.once("error", reject);});
await worker.terminate(); check(result === "passed", "Worker iterator checks");
`, worker)
	executeWebBasics(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, code)
}
