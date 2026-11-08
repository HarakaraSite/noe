package stdlib_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const webBasicsPrelude = `
function check(value, label) { if (!value) throw Error(label); }
function throws(name, call) {
  let error;
  try { call(); } catch (value) { error = value; }
  check(error && error.name === name, "expected " + name + ", got " + (error && error.name));
}
`

func executeWebBasics(t *testing.T, profile stdlib.Profile, code string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[Web API basics]", Virtual: true, Module: true, Code: webBasicsPrelude + code}, profile)
	if err != nil {
		t.Fatalf("Web API execution: %s", stdlib.Diagnostic(err))
	}
	if result.Code != 0 {
		t.Fatalf("Web API exit %d: %+v", result.Code, result.Diagnostics)
	}
}

const webBasicsEncoding = `
const encoder = new TextEncoder();
for (const [text, capacity, read, expected] of [
  ["", 0, 0, []], ["abc", 2, 2, [97, 98]],
  ["あ", 1, 0, []], ["あ", 2, 0, []], ["あ", 3, 1, [227, 129, 130]],
  ["😀", 3, 0, []], ["😀", 4, 2, [240, 159, 152, 128]],
  ["a😀z", 4, 1, [97]], ["a😀z", 5, 3, [97, 240, 159, 152, 128]],
  ["\ud800x", 2, 0, []], ["\ud800x", 4, 2, [239, 191, 189, 120]],
  ["\udc00", 3, 1, [239, 191, 189]],
]) {
  const backing = new Uint8Array(capacity + 2).fill(0x55);
  const destination = backing.subarray(1, capacity + 1);
  const result = encoder.encodeInto(text, destination);
  check(result.read === read && result.written === expected.length, "UTF-16/UTF-8 counts");
  check(expected.every((value, i) => destination[i] === value), "complete encoded sequence");
  check(destination.subarray(expected.length).every(value => value === 0x55), "unwritten tail");
  check(backing[0] === 0x55 && backing[backing.length - 1] === 0x55, "destination view bounds");
}
const shared = new Uint8Array(new SharedArrayBuffer(3));
check(encoder.encodeInto("あ", shared).written === 3 && shared[0] === 227, "shared destination");
const detachedBuffer = new ArrayBuffer(2), detached = new Uint8Array(detachedBuffer);
structuredClone(detachedBuffer, {transfer: [detachedBuffer]});
const empty = encoder.encodeInto("a", detached);
check(empty.read === 0 && empty.written === 0, "detached destination is empty");
throws("TypeError", () => encoder.encodeInto("a"));
throws("TypeError", () => encoder.encodeInto(Symbol(), new Uint8Array(8)));
throws("TypeError", () => TextEncoder.prototype.encodeInto.call({}, "a", new Uint8Array(1)));
for (const destination of [[], new Uint16Array(2), new Uint8ClampedArray(2), new DataView(new ArrayBuffer(2))]) {
  throws("TypeError", () => encoder.encodeInto("a", destination));
}
throws("TypeError", () => encoder.encodeInto("a", new Uint8Array(new ArrayBuffer(2, {maxByteLength: 4}))));
`

func TestWebBasicsEncodeInto(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, webBasicsEncoding)
}

const webBasicsCustomEvent = `
const detail = {value: 1};
const event = new CustomEvent("update", {detail, bubbles: true, cancelable: true, composed: true});
check(event instanceof Event && event instanceof CustomEvent && event.detail === detail, "CustomEvent inheritance/detail identity");
check(event.bubbles && event.cancelable && event.composed && !event.defaultPrevented, "event flags");
check(new CustomEvent("empty").detail === null && new CustomEvent("empty", {detail: undefined}).detail === null, "default detail");
check(new CustomEvent("null", null).detail === null, "null dictionary");
for (const name of ["detail", "type", "bubbles", "cancelable", "composed"]) {
  throws("TypeError", () => { event[name] = "changed"; });
}
throws("TypeError", () => new CustomEvent());
throws("TypeError", () => new CustomEvent(Symbol()));
throws("TypeError", () => new CustomEvent("invalid", 1));
throws("TypeError", () => Object.getOwnPropertyDescriptor(CustomEvent.prototype, "detail").get.call({}));
throws("DataCloneError", () => structuredClone(event));
const order = [];
new CustomEvent({toString() { order.push("type"); return "ordered"; }}, {
  get bubbles() { order.push("bubbles"); return true; },
  get cancelable() { order.push("cancelable"); return false; },
  get composed() { order.push("composed"); return true; },
  get detail() { order.push("detail"); return detail; },
});
check(order.join(",") === "type,bubbles,cancelable,composed,detail", "dictionary conversion order");
const target = new EventTarget();
let calls = 0;
target.addEventListener("update", received => {
  calls++;
  check(received === event && received.target === target && received.currentTarget === target, "event dispatch identity");
  check(received.detail === detail, "listener detail identity");
  received.initCustomEvent("ignored", false, false, "ignored");
  check(received.type === "update" && received.detail === detail, "initializer is inert during dispatch");
  throws("InvalidStateError", () => target.dispatchEvent(received));
  received.preventDefault(); received.stopImmediatePropagation();
});
target.addEventListener("update", () => { throw Error("stopped listener ran"); });
check(target.dispatchEvent(event) === false && calls === 1 && event.defaultPrevented, "cancelable dispatch");
check(event.currentTarget === null && event.target === target && event.eventPhase === 0, "dispatch cleanup");
event.initCustomEvent("reset", false, false, 42);
check(event.type === "reset" && event.detail === 42 && !event.defaultPrevented && event.target === null, "legacy reinitialization");
check(!event.bubbles && !event.cancelable && event.composed, "initializer preserves composed");
`

func TestWebBasicsCustomEvent(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, webBasicsCustomEvent)
}

const webBasicsPerformance = `
performance.clearMarks(); performance.clearMeasures();
const detail = {value: 1};
const first = performance.mark("start", {startTime: 2, detail});
detail.value = 9;
check(first instanceof PerformanceMark && first instanceof PerformanceEntry && first.detail.value === 1, "mark entry and detail copy");
performance.mark("start", {startTime: 5});
const measure = performance.measure("work", {start: "start", end: 8, detail: [1, 2]});
check(measure instanceof PerformanceMeasure && measure.startTime === 5 && measure.duration === 3, "latest named mark");
const entries = performance.getEntries();
check(entries.length === 3 && entries[0] === first && entries[2] === measure, "chronological entries");
entries.length = 0;
check(performance.getEntries().length === 3, "result array is a copy");
const standalone = new PerformanceMark("standalone", {startTime: 1});
check(standalone.startTime === 1 && performance.getEntriesByName("standalone").length === 0, "standalone mark is not buffered");
throws("TypeError", () => new Performance());
throws("TypeError", () => new PerformanceEntry());
throws("TypeError", () => new PerformanceMeasure());
throws("TypeError", () => { first.startTime = 999; });
throws("DataCloneError", () => structuredClone(performance));
throws("DataCloneError", () => structuredClone(first));
throws("SyntaxError", () => performance.measure("missing", "absent"));
throws("DataCloneError", () => performance.mark("bad", {detail() {}}));
check(performance.getEntriesByName("missing").length === 0 && performance.getEntriesByName("bad").length === 0, "failed operations leave no entry");
check(JSON.parse(JSON.stringify(first)).detail.value === 1, "entry serialization");
performance.clearMarks("start");
check(performance.getEntriesByType("mark").length === 0 && performance.getEntriesByType("measure").length === 1, "selective clear");
performance.clearMeasures();
check(performance.getEntries().length === 0 && Number.isFinite(performance.timeOrigin) && performance.now() >= 0, "clear and clock");
`

func TestWebBasicsPerformanceContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, webBasicsPerformance)
}

func TestWebBasicsPerformanceDefaultOptionsIgnorePrototype(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
Object.defineProperty(Object.prototype, "start", {value: 5, configurable: true});
try {
  check(performance.measure("omitted").startTime === 0, "omitted options do not read a prototype");
  check(performance.measure("undefined", undefined).startTime === 0, "undefined options do not read a prototype");
  check(performance.measure("null", null).startTime === 0, "null options do not read a prototype");
  check(performance.measure("object", {}).startTime === 5, "explicit object dictionaries read inherited members");
} finally {
  delete Object.prototype.start;
}
`)
}

func TestWebBasicsMarkDefaultOptionsIgnorePrototype(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
Object.defineProperties(Object.prototype, {
  startTime: {value: -1, configurable: true},
  detail: {value() {}, configurable: true},
});
try {
  for (const mark of [performance.mark("omitted"), performance.mark("undefined", undefined),
    performance.mark("null", null), new PerformanceMark("standalone"),
    new PerformanceMark("standalone-undefined", undefined), new PerformanceMark("standalone-null", null)]) {
    check(mark.startTime >= 0 && mark.detail === null, "default mark options do not read a prototype");
  }
  throws("TypeError", () => performance.mark("explicit-object", {}));
} finally {
  delete Object.prototype.startTime;
  delete Object.prototype.detail;
}
`)
}

func TestWebBasicsMainAndWorker(t *testing.T) {
	worker := webBasicsPrelude + `
import {parentPort} from "node:worker_threads";
if (typeof fetch !== "undefined") throw Error("Worker gained network capability");
check(performance.getEntries().length === 0, "Worker timeline starts empty");
const destination = new Uint8Array(1);
check(new TextEncoder().encodeInto("あ", destination).written === 0, "Worker encoding");
const target = new EventTarget(); let detail;
target.addEventListener("message", event => {detail = event.detail;});
target.dispatchEvent(new CustomEvent("message", {detail: "worker"}));
const mark = performance.mark("worker", {startTime: 1});
parentPort.postMessage({detail, type: mark.entryType, entries: performance.getEntries().length});
parentPort.close();`
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
check(typeof fetch === "undefined", "main gained network capability");
performance.mark("parent", {startTime: 0});
const worker = new Worker(%q, {eval: true, type: "module"});
const result = await new Promise((resolve, reject) => {worker.once("message", resolve); worker.once("error", reject);});
await worker.terminate();
check(result.detail === "worker" && result.type === "mark" && result.entries === 1, "Worker API result");
check(performance.getEntriesByName("worker").length === 0 && performance.getEntriesByName("parent").length === 1, "independent timelines");
`, worker)
	executeWebBasics(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, code)
}
