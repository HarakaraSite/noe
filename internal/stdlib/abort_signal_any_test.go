package stdlib_test

import (
	"runtime"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestAbortSignalAnyConsumesAndValidatesSequence(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const alreadyAborted = AbortSignal.abort("first");
  let consumed = 0;
  function* validInputs() {
    consumed++;
    yield alreadyAborted;
    consumed++;
    yield new AbortController().signal;
    consumed++;
  }
  const first = AbortSignal.any(validInputs());
  if (consumed !== 3 || !first.aborted || first.reason !== "first") {
    throw Error("any did not consume the full iterable before selecting the pre-aborted signal");
  }

  let invalidConsumed = 0;
  let iteratorClosed = false;
  function* invalidLaterInput() {
    try {
      invalidConsumed++;
      yield alreadyAborted;
      invalidConsumed++;
      yield {};
      invalidConsumed++;
    } finally {
      iteratorClosed = true;
    }
  }
  let invalidFailure;
  try { AbortSignal.any(invalidLaterInput()); }
  catch (error) { invalidFailure = error; }
  if (!(invalidFailure instanceof TypeError) || invalidConsumed !== 2 || !iteratorClosed) {
    throw Error("an invalid later sequence entry was skipped");
  }

  for (const primitive of ["", "signals", 0, false, null, undefined]) {
    let failure;
    try { AbortSignal.any(primitive); }
    catch (error) { failure = error; }
    if (!(failure instanceof TypeError)) throw Error("a primitive sequence input was accepted");
  }
  let arrayLikeFailure;
  try { AbortSignal.any({0: alreadyAborted, length: 1}); }
  catch (error) { arrayLikeFailure = error; }
  if (!(arrayLikeFailure instanceof TypeError)) throw Error("an array-like non-iterable was accepted");

  let fakeFailure;
  try { AbortSignal.any([alreadyAborted, Object.create(AbortSignal.prototype)]); }
  catch (error) { fakeFailure = error; }
  if (!(fakeFailure instanceof TypeError)) throw Error("a prototype-only object passed the AbortSignal brand check");

  const empty = AbortSignal.any([]);
  const unrelated = new AbortController();
  unrelated.abort("unrelated");
  if (empty.aborted) throw Error("empty any signal aborted");
  console.log("sequence conversion and empty any ok");
})()
`)
	if out != "sequence conversion and empty any ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyPreservesReasonIdentityAndInputOrder(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const firstReason = {source: "first"};
  const secondReason = {source: "second"};
  const firstPreAborted = AbortSignal.abort(firstReason);
  const secondPreAborted = AbortSignal.abort(secondReason);
  const preAborted = AbortSignal.any([firstPreAborted, secondPreAborted]);
  if (!preAborted.aborted || preAborted.reason !== firstReason) {
    throw Error("any did not select the first pre-aborted input in sequence order");
  }

  for (const reason of [false, 0, null, ""]) {
    const left = new AbortController();
    const right = new AbortController();
    const combined = AbortSignal.any([left.signal, right.signal]);
    let events = 0;
    combined.addEventListener("abort", () => { events++; });
    left.abort(reason);
    right.abort("later");
    if (!combined.aborted || combined.reason !== reason || events !== 1) {
      throw Error("any lost the first runtime abort reason or fired more than once");
    }
  }

  const syntheticSource = new AbortController();
  const syntheticCombined = AbortSignal.any([syntheticSource.signal]);
  syntheticSource.signal.dispatchEvent(new Event("abort"));
  if (syntheticCombined.aborted) throw Error("synthetic abort event propagated as a signal abort");
  syntheticSource.abort("real abort");
  if (!syntheticCombined.aborted || syntheticCombined.reason !== "real abort") {
    throw Error("real abort did not propagate after a synthetic event");
  }
  console.log("any reason identity and ordering ok");
})()
`)
	if out != "any reason identity and ordering ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyFlattensNestedAndDuplicateInputs(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const a = new AbortController();
  const b = new AbortController();
  const c = new AbortController();
  let overriddenMethods = 0;
  for (const signal of [a.signal, b.signal, c.signal]) {
    signal.addEventListener = () => { overriddenMethods++; throw Error("must not be called"); };
    signal.removeEventListener = () => { overriddenMethods++; throw Error("must not be called"); };
  }

  const inner = AbortSignal.any([a.signal, a.signal, b.signal]);
  const outer = AbortSignal.any([inner, inner, c.signal]);
  const reason = {source: "a"};
  const events = [];
  let sourceObservedCombinedState = false;
  a.signal.addEventListener = EventTarget.prototype.addEventListener;
  a.signal.addEventListener("abort", () => {
    sourceObservedCombinedState = inner.aborted && outer.aborted &&
      inner.reason === reason && outer.reason === reason;
    events.push("source");
    c.abort("reentrant");
  });
  inner.addEventListener("abort", () => events.push("inner"));
  inner.onabort = () => events.push("inner-handler");
  outer.addEventListener("abort", () => events.push("outer"));

  a.abort(reason);
  b.abort("later");
  c.abort("later still");
  if (!sourceObservedCombinedState) throw Error("composite state was not established before source abort listeners");
  if (events.join(",") !== "source,inner,inner-handler,outer") throw Error("nested composite event order or duplicate suppression failed: " + events);
  if (outer.reason !== reason || overriddenMethods !== 0) throw Error("nested composition changed reason or used public source listeners");
  console.log("nested and duplicate any signals ok");
})()
`)
	if out != "nested and duplicate any signals ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyTimeoutAndDependencyCleanup(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const timeout = AbortSignal.timeout(0);
  const combined = AbortSignal.any([timeout]);
  await new Promise(resolve => combined.addEventListener("abort", resolve, {once: true}));
  if (!timeout.aborted || !combined.aborted || combined.reason !== timeout.reason) {
    throw Error("timeout abort did not propagate through any");
  }

  const one = new AbortController();
  const two = new AbortController();
  let sourceListenerCalls = 0;
  for (const signal of [one.signal, two.signal]) {
    signal.addEventListener = () => { sourceListenerCalls++; };
    signal.removeEventListener = () => { sourceListenerCalls++; };
  }
  const joined = AbortSignal.any([one.signal, two.signal]);
  let events = 0;
  const reason = {winner: 1};
  joined.addEventListener("abort", () => { events++; });
  one.abort(reason);
  two.abort({winner: 2});
  if (joined.reason !== reason || events !== 1 || sourceListenerCalls !== 0) {
    throw Error("settled dependencies were retained or controlled by source event methods");
  }
  console.log("timeout and dependency cleanup ok");
})()
`)
	if out != "timeout and dependency cleanup ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyAbortsPipeTo(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const started = deferred();
  const controller = new AbortController();
  const reason = false;
  const combined = AbortSignal.any([controller.signal]);
  const source = new ReadableStream({pull() { started.resolve(); return new Promise(() => {}); }}, {highWaterMark: 0});
  const sink = new WritableStream({abort(value) { if (value !== reason) throw Error("wrong abort reason"); }});
  const piping = source.pipeTo(sink, {signal: combined});
  await started.promise;
  controller.abort(reason);
  const failure = await piping.then(() => undefined, error => error);
  if (failure !== reason || source.locked || sink.locked) throw Error("pipeTo did not observe the composite abort");
  console.log("pipeTo observes any signal ok");
})()
`)
	if out != "pipeTo observes any signal ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyContinuesDependentDispatchAfterRemovalErrors(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const controller = new AbortController();
  const first = AbortSignal.any([controller.signal]);
  const second = AbortSignal.any([controller.signal]);
  const sourceFailure = {kind: "source remove"};
  let firstCalls = 0, secondCalls = 0;
  controller.signal.addEventListener("abort", () => {}, {once: true});
  controller.signal.removeEventListener = () => { throw sourceFailure; };
  first.addEventListener("abort", () => { firstCalls++; }, {once: true});
  first.removeEventListener = () => { throw Error("composite remove"); };
  second.addEventListener("abort", () => { secondCalls++; });

  let failure;
  try { controller.abort("stop"); }
  catch (error) { failure = error; }
  if (failure !== sourceFailure) throw Error("existing source dispatch failure was not preserved");
  if (!first.aborted || !second.aborted || firstCalls !== 0 || secondCalls !== 1) {
    throw Error("a dispatch cleanup failure skipped later dependent abort events");
  }
  console.log("dependent dispatch continues after cleanup errors ok");
})()
`)
	if out != "dependent dispatch continues after cleanup errors ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestAbortSignalAnyKeepsObservedSignalsAlive(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := stdlib.NewLoop(rt)
	if err := stdlib.Install(rt, stdlib.Config{Loop: loop}); err != nil {
		t.Fatal(err)
	}
	if err := requireWorkerThreads(rt); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
globalThis.listenerSource = new AbortController();
globalThis.handlerSource = new AbortController();
globalThis.inactiveSource = new AbortController();
globalThis.removedSource = new AbortController();
globalThis.abortCalls = 0;
globalThis.listenerRef = (() => {
  const signal = AbortSignal.any([listenerSource.signal]);
  signal.addEventListener = () => { throw Error("overridden addEventListener was called"); };
  EventTarget.prototype.addEventListener.call(signal, "abort", () => { abortCalls++; });
  return new WeakRef(signal);
})();
globalThis.handlerRef = (() => {
  const signal = AbortSignal.any([handlerSource.signal]);
  signal.onabort = () => { abortCalls++; };
  return new WeakRef(signal);
})();
globalThis.inactiveRef = (() => new WeakRef(AbortSignal.any([inactiveSource.signal])))();
globalThis.removedRef = (() => {
  const signal = AbortSignal.any([removedSource.signal]);
  const listener = () => {};
  signal.addEventListener = () => { throw Error("overridden addEventListener was called"); };
  signal.removeEventListener = () => { throw Error("overridden removeEventListener was called"); };
  EventTarget.prototype.addEventListener.call(signal, "abort", listener);
  EventTarget.prototype.removeEventListener.call(signal, "abort", listener);
  return new WeakRef(signal);
})();
`); err != nil {
		t.Fatal(err)
	}
	collectUntil(t, rt, `inactiveRef.deref() === undefined && removedRef.deref() === undefined`)
	v, err := rt.Eval(`listenerRef.deref() !== undefined && handlerRef.deref() !== undefined`)
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "true" {
		t.Fatal("a dependent signal with abort observers was collected")
	}
	if _, err := rt.Eval(`listenerSource.abort("listener"); handlerSource.abort("handler");`); err != nil {
		t.Fatal(err)
	}
	if v, err := rt.Eval(`abortCalls`); err != nil || v.Int() != 2 {
		t.Fatalf("abort observer calls = %v, err = %v; want 2", v, err)
	}
	collectUntil(t, rt, `listenerRef.deref() === undefined && handlerRef.deref() === undefined`)
}

func collectUntil(t *testing.T, rt *quickjs.Runtime, expression string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		runtime.GC()
		runtime.GC()
		runtime.Gosched()
		v, err := rt.Eval(expression)
		if err != nil {
			t.Fatal(err)
		}
		if v.String() == "true" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition did not become true after garbage collection: %s", expression)
}
