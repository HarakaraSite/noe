package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const eventListenerContract = `
const target = new EventTarget();
let calls = [];
function callback(event) { check(this === target && event.currentTarget === target && event.eventPhase === 2, "listener target"); calls.push("callback"); }
target.addEventListener("test", callback);
target.addEventListener("test", callback, {once: true, passive: true});
target.addEventListener("test", callback, true);
target.removeEventListener("test", callback, {capture: true});
target.dispatchEvent(new Event("test"));
target.dispatchEvent(new Event("test"));
check(calls.join() === "callback,callback", "duplicate keeps original options and capture identity");
target.removeEventListener("test", callback, false);

calls = [];
const removed = () => calls.push("removed");
target.addEventListener("remove", () => { calls.push("first"); target.removeEventListener("remove", removed); target.addEventListener("remove", removed); }, {once: true});
target.addEventListener("remove", removed);
target.dispatchEvent(new Event("remove"));
check(calls.join() === "first", "removal and re-add do not revive snapshot");
target.dispatchEvent(new Event("remove"));
check(calls.join() === "first,removed", "new listener runs next dispatch");

calls = [];
target.addEventListener("phase", () => calls.push("bubble"));
target.addEventListener("phase", () => { calls.push("capture"); target.addEventListener("phase", () => calls.push("added bubble"), {once: true}); }, {capture: true, once: true});
target.dispatchEvent(new Event("phase"));
check(calls.join() === "capture,bubble,added bubble", "per-phase snapshots and capture first");
target.addEventListener("stop", event => { calls.push("capture stop"); event.stopPropagation(); }, true);
target.addEventListener("stop", () => calls.push("capture next"), true);
target.addEventListener("stop", () => { throw Error("bubble after capture stop"); });
target.dispatchEvent(new Event("stop"));
check(calls.slice(-2).join() === "capture stop,capture next", "stopPropagation keeps current phase listeners");

let once = 0;
target.addEventListener("once", () => { once++; target.dispatchEvent(new Event("once")); }, {once: true});
target.removeEventListener = () => { throw Error("public removal must not be called internally"); };
target.dispatchEvent(new Event("once"));
check(once === 1, "once removed before recursive dispatch");
delete target.removeEventListener;

const passive = new Event("passive", {cancelable: true});
target.addEventListener("passive", event => { event.preventDefault(); event.returnValue = false; }, {passive: true});
check(target.dispatchEvent(passive) && !passive.defaultPrevented, "passive cannot cancel");
target.addEventListener("passive", event => event.preventDefault(), {once: true});
check(!target.dispatchEvent(new Event("passive", {cancelable: true})), "active listener can cancel after passive");
passive.preventDefault();
check(passive.defaultPrevented, "passive flag cleared after dispatch");

const controller = new AbortController();
let bound = 0;
const boundCallback = () => bound++;
target.addEventListener("bound", boundCallback, {signal: controller.signal});
controller.signal.dispatchEvent(new Event("abort"));
target.dispatchEvent(new Event("bound"));
check(bound === 1 && !controller.signal.aborted, "synthetic abort does not remove listener");
controller.signal.addEventListener("abort", () => target.dispatchEvent(new Event("bound")));
controller.abort("done");
target.dispatchEvent(new Event("bound"));
check(bound === 1, "real abort removes before abort event");
target.addEventListener("bound", boundCallback, {signal: controller.signal});
target.dispatchEvent(new Event("bound"));
check(bound === 1, "pre-aborted registration ignored");
const shadowed = new AbortController();
Object.defineProperty(shadowed.signal, "aborted", {value: true});
target.addEventListener("shadowed", () => { throw Error("shadowed signal bypassed private abort"); }, {signal: shadowed.signal});
shadowed.abort("private"); target.dispatchEvent(new Event("shadowed"));
check(Object.getOwnPropertyDescriptor(AbortSignal.prototype, "aborted").get.call(shadowed.signal), "controller uses private abort state");

const first = new AbortController(), second = new AbortController();
let duplicate = 0;
const duplicateCallback = () => duplicate++;
target.addEventListener("duplicate", duplicateCallback, {signal: first.signal});
target.addEventListener("duplicate", duplicateCallback, {signal: second.signal, once: true});
second.abort(); target.dispatchEvent(new Event("duplicate"));
first.abort(); target.dispatchEvent(new Event("duplicate"));
check(duplicate === 1, "duplicate signal does not replace original");
const source = new AbortController(), composite = AbortSignal.any([source.signal]);
target.addEventListener("composite", () => { throw Error("composite listener survived abort"); }, {signal: composite});
source.abort(); target.dispatchEvent(new Event("composite"));

let objects = 0;
const object = {};
target.addEventListener("object", object);
object.handleEvent = function(event) { check(this === object && event.type === "object", "callback object receiver"); objects++; };
target.dispatchEvent(new Event("object"));
object.handleEvent = function() { objects += 2; };
target.dispatchEvent(new Event("object"));
check(objects === 3, "callback method looked up during dispatch");

const order = [];
target.addEventListener({toString() {order.push("type"); return "convert";}}, null, {
  get capture() {order.push("capture"); return false;},
  get once() {order.push("once"); return false;},
  get passive() {order.push("passive"); return false;},
  get signal() {order.push("signal"); return undefined;},
});
check(order.join() === "type,capture,once,passive,signal", "argument and dictionary order even for null callback");
target.removeEventListener("convert", null, {get capture() {order.push("remove capture");}, get once() {throw Error("remove reads once");}});
check(order.at(-1) === "remove capture", "removal only converts capture");
let defaults = 0; const defaultCallback = () => defaults++;
Object.defineProperty(Object.prototype, "capture", {configurable: true, get() { throw Error("default capture read prototype"); }});
try {
  target.addEventListener("defaults", defaultCallback);
  target.addEventListener("defaults", defaultCallback, null);
  target.removeEventListener("defaults", defaultCallback);
} finally {delete Object.prototype.capture;}
target.dispatchEvent(new Event("defaults"));
check(defaults === 0, "empty default options do not read the prototype");
const early = new AbortController(); early.abort();
let getterRead = false;
target.addEventListener("early", null, {get once() {getterRead = true;}, signal: early.signal});
check(getterRead, "options converted before pre-aborted/null-callback return");
throws("TypeError", () => target.addEventListener("x"));
throws("TypeError", () => target.removeEventListener("x"));
throws("TypeError", () => target.addEventListener(Symbol(), null));
throws("TypeError", () => target.addEventListener("x", 1));
for (const signal of [null, {}, Object.create(AbortSignal.prototype)]) throws("TypeError", () => target.addEventListener("x", null, {signal}));
throws("TypeError", () => EventTarget.prototype.addEventListener.call({}, "x", null));
throws("TypeError", () => EventTarget.prototype.dispatchEvent.call({}, new Event("x")));
check(Object.keys(target).length === 0, "listener state is private");
`

func TestEventListenerContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, eventListenerContract)
}

func TestDecoderAndListenersInStreamsAndMessaging(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
const chunks = [new Uint8Array([239]), new Uint8Array([187,191,226]), new Uint8Array([130]), new Uint8Array([172,192,128])];
const source = new ReadableStream({start(controller) {for(const chunk of chunks) controller.enqueue(chunk); controller.close();}});
let text = "";
for await (const chunk of source.pipeThrough(new TextDecoderStream())) text += chunk;
check(text === "€��", "stream wrapper shares UTF-8 and BOM state");
const channel = new MessageChannel(), controller = new AbortController();
channel.port2.addEventListener("message", () => { throw Error("aborted message listener"); }, {signal: controller.signal});
controller.abort();
let messages = 0, received;
const ready = new Promise(resolve => {received = resolve;});
const receive = event => {check(event.data === "ok", "message payload"); messages++; channel.port1.close(); channel.port2.close(); received();};
channel.port2.addEventListener("message", receive, {once:true});
channel.port2.addEventListener("message", receive);
channel.port2.start();
channel.port1.postMessage("ok");
await ready;
check(messages === 1, "MessagePort uses shared listener identity");
`)
}
