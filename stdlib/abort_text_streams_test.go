package stdlib_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const abortInterfaceContract = `
throws("TypeError", () => new AbortSignal());
throws("TypeError", () => new AbortSignal({}));
const controller = new AbortController(), signal = controller.signal;
check(signal === controller.signal && !signal.aborted && signal.reason === undefined, "initial SameObject signal");
check(!Reflect.set(controller, "signal", {}), "readonly controller signal");
check(Object.keys(controller).length === 0 && Object.keys(signal).length === 0, "private abort state");
const calls = [];
signal.addEventListener("abort", () => calls.push("before"));
signal.onabort = function(event) {check(this === signal && event.target === signal, "handler receiver"); calls.push("old");};
signal.addEventListener("abort", () => calls.push("after"));
signal.onabort = () => calls.push("handler");
controller.abort();
check(calls.join() === "before,handler,after", "handler keeps registration position");
check(signal.reason instanceof DOMException && signal.reason.name === "AbortError" && signal.reason.code === 20, "default abort DOMException");
const reason = signal.reason; controller.abort("later");
check(signal.reason === reason, "abort is idempotent");
try {signal.throwIfAborted(); throw Error("missing reason");} catch(error) {check(error === reason, "throwIfAborted identity");}
const staticSignal = AbortSignal.abort();
check(staticSignal.reason instanceof DOMException && staticSignal.reason.name === "AbortError", "static default reason");
for (const value of [false, 0, "", null, {kind:"explicit"}]) {
  const instance = new AbortController(); instance.abort(value);
  check(instance.signal.reason === value && AbortSignal.abort(value).reason === value, "explicit reason identity");
}
const shadowed = new AbortController(), actual = shadowed.signal;
Object.defineProperty(shadowed, "signal", {value: {aborted:false}});
shadowed.abort("private");
check(actual.aborted && actual.reason === "private", "abort uses private controller signal");
throws("TypeError", () => AbortController.prototype.abort.call({}));
throws("TypeError", () => Object.getOwnPropertyDescriptor(AbortController.prototype,"signal").get.call({}));
throws("TypeError", () => Object.getOwnPropertyDescriptor(AbortSignal.prototype,"onabort").get.call({}));
throws("TypeError", () => Object.getOwnPropertyDescriptor(AbortSignal.prototype,"onabort").set.call({},null));
check(AbortController.prototype.abort.length === 0 && AbortSignal.abort.length === 0 && AbortSignal.timeout.length === 1, "operation arities");
check(Object.prototype.toString.call(signal) === "[object AbortSignal]" && Object.prototype.toString.call(controller) === "[object AbortController]", "interface tags");
const removed = new AbortController(), order = [];
removed.signal.onabort = () => order.push("removed");
removed.signal.addEventListener("abort", () => order.push("listener"));
removed.signal.onabort = null; removed.signal.onabort = () => order.push("readded");
removed.abort(); check(order.join() === "listener,readded", "handler removal and re-registration");
const objectHandlerController = new AbortController(), objectHandler = {}, objectOrder = [];
objectHandlerController.signal.onabort = objectHandler;
check(objectHandlerController.signal.onabort === objectHandler, "object handler retained");
objectHandlerController.signal.addEventListener("abort", () => objectOrder.push("listener"));
Object.defineProperty(objectHandler, "call", {get(){throw Error("inert handler inspected call");}});
objectHandlerController.signal.dispatchEvent(new Event("abort"));
check(objectOrder.join() === "listener", "noncallable object handler is inert");
objectOrder.length = 0;
objectHandlerController.signal.onabort = () => objectOrder.push("handler");
objectHandlerController.abort(); check(objectOrder.join() === "handler,listener", "object handler preserves registration");
for (const value of [undefined, -1, NaN, Infinity, -Infinity, 9007199254740992, 18446744073709549568, 18446744073709551616, 1n, Symbol()]) {
  throws("TypeError", () => AbortSignal.timeout(value));
}
throws("TypeError", () => AbortSignal.timeout());
let conversions = 0;
const timed = AbortSignal.timeout({valueOf(){conversions++; return 0;}});
check(conversions === 1 && !timed.aborted, "timeout conversion and asynchronous task");
await new Promise(resolve => timed.addEventListener("abort", resolve, {once:true}));
check(timed.reason instanceof DOMException && timed.reason.name === "TimeoutError" && timed.reason.code === 23, "timeout reason");
for (const value of [null, -0.9, "0", 1.9]) {
  const timeout = AbortSignal.timeout(value);
  await new Promise(resolve => timeout.addEventListener("abort", resolve, {once:true}));
  check(timeout.reason.name === "TimeoutError", "EnforceRange truncation/coercion");
}
const source = new AbortController(), composite = AbortSignal.any([source.signal]);
let observed;
source.signal.onabort = () => {observed = composite.reason;};
source.abort();
check(observed === source.signal.reason && composite.reason === observed && observed.name === "AbortError", "dependent default reason ordering");
`

func TestAbortInterfaceContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, abortInterfaceContract)
}

func TestAbortStandardReasonsReachFetchAndPipeTo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	executeWebBasics(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, fmt.Sprintf(`
const aborted = new AbortController(); aborted.abort();
const failure = await fetch(%q, {signal:aborted.signal}).then(() => null, error => error);
check(failure === aborted.signal.reason && failure.name === "AbortError", "Fetch default abort reason");
const timeout = AbortSignal.timeout(0);
let canceled, abortedWith, start;
const started = new Promise(resolve => {start = resolve;});
const source = new ReadableStream({pull(){start(); return new Promise(()=>{});}, cancel(reason){canceled=reason;}},{highWaterMark:0});
const sink = new WritableStream({abort(reason){abortedWith=reason;}});
const piping = source.pipeTo(sink,{signal:timeout}); await started;
const result = await piping.then(() => null, error => error);
check(result === timeout.reason && result.name === "TimeoutError" && canceled === result && abortedWith === result, "pipeTo timeout reason identity");
check(!source.locked && !sink.locked, "pipeTo cleanup");
`, server.URL))
}

func TestTextStreamCancellationPreservesReasons(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, `
for (const [stream, partial] of [[new TextEncoderStream(), "\ud800"], [new TextDecoderStream(), new Uint8Array([226])]]) {
  throws("DataCloneError", () => structuredClone(stream));
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const reading = reader.read();
  await writer.write(partial);
  const reason = {kind:"readable cancel"};
  const closed = writer.closed.catch(error => error);
  await reader.cancel(reason);
  check((await reading).done && await closed === reason, "cancel releases pending codec read and writer");
}
for (const stream of [new TextEncoderStream(), new TextDecoderStream("utf-8", {fatal:true})]) {
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const reading = reader.read().catch(error => error);
  const reason = {kind:"writable abort"};
  await writer.abort(reason);
  check(await reading === reason, "abort rejects pending codec read with exact reason");
}
`)
}
