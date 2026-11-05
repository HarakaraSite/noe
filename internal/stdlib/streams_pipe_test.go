package stdlib_test

import (
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestStreamsPipeToAbortPendingReadAndPreserveFalsyReason(t *testing.T) {
	for _, reason := range []string{"false", "0", "null", `""`} {
		t.Run(reason, func(t *testing.T) {
			out, _ := run(t, stdlib.Config{}, fmt.Sprintf(`
;(async () => {
  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const pulled = deferred();
  const finishPull = deferred();
  const controller = new AbortController();
  const reason = %s;
  let sourceCancelReason, sinkAbortReason;
  const source = new ReadableStream({
    pull() { pulled.resolve(); return finishPull.promise; },
    cancel(value) { sourceCancelReason = value; },
  }, {highWaterMark: 0});
  const sink = new WritableStream({abort(value) { sinkAbortReason = value; }});
  const piping = source.pipeTo(sink, {signal: controller.signal});
  await pulled.promise;
  controller.abort(reason);
  const failure = await piping.then(() => undefined, error => error);
  if (failure !== reason) throw Error("pipe did not preserve the falsy abort reason");
  if (sourceCancelReason !== reason || sinkAbortReason !== reason) throw Error("abort actions lost the reason");
  if (source.locked || sink.locked) throw Error("abort left a stream locked");
  finishPull.resolve();
  await Promise.resolve();
  console.log("pending read abort ok");
})()
`, reason))
			if out != "pending read abort ok" {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestStreamsPipeToAbortPendingWriteStartsActionsConcurrently(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const writeStarted = deferred();
  const finishWrite = deferred();
  const finishAbort = deferred();
  const finishCancel = deferred();
  const controller = new AbortController();
  const reason = {kind: "stop"};
  let sourceCancelStarted = false, sinkAbortStarted = false;
  let sourceController, emitted = false;
  const source = new ReadableStream({
    start(c) { sourceController = c; },
    pull() { if (!emitted) { emitted = true; sourceController.enqueue("chunk"); } },
    cancel(value) {
      if (value !== reason) throw Error("source cancel reason");
      sourceCancelStarted = true;
      return finishCancel.promise;
    },
  }, {highWaterMark: 0});
  const sink = new WritableStream({
    write() { writeStarted.resolve(); return finishWrite.promise; },
    abort(value) {
      if (value !== reason) throw Error("sink abort reason");
      sinkAbortStarted = true;
      return finishAbort.promise;
    },
  });
  const piping = source.pipeTo(sink, {signal: controller.signal});
  await writeStarted.promise;
  controller.abort(reason);
  if (!sourceCancelStarted || !sinkAbortStarted) throw Error("shutdown actions were not started together");
  let settled = false;
  piping.then(() => { settled = true; }, () => { settled = true; });
  await Promise.resolve();
  if (settled) throw Error("pipe completed before its cancellation actions settled");
  finishAbort.resolve();
  finishCancel.resolve();
	await Promise.resolve();
	await Promise.resolve();
	if (settled) throw Error("pipe completed before its in-flight write settled");
	finishWrite.resolve();
  const failure = await piping.then(() => undefined, error => error);
  if (failure !== reason) throw Error("pipe did not preserve abort reason");
  if (source.locked || sink.locked) throw Error("abort left a stream locked");
  console.log("pending write abort ok");
})()
`)
	if out != "pending write abort ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsPipeToPreventFlags(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  let closeCount = 0, writeCount = 0;
  const source = ReadableStream.from([1]);
  const sink = new WritableStream({write() { writeCount++; }, close() { closeCount++; }});
  await source.pipeTo(sink, {preventClose: true});
  if (writeCount !== 1 || closeCount !== 0) throw Error("preventClose was ignored");
  if (source.locked || sink.locked) throw Error("preventClose left a stream locked");

  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const pulled = deferred(), finishPull = deferred();
  const aborter = new AbortController();
  const reason = false;
  let cancels = 0, aborts = 0;
  let sourceController;
  const pendingSource = new ReadableStream({
    start(c) { sourceController = c; },
    pull() { pulled.resolve(); return finishPull.promise; },
    cancel() { cancels++; },
  }, {highWaterMark: 0});
  const pendingSink = new WritableStream({abort() { aborts++; }});
  const piping = pendingSource.pipeTo(pendingSink, {
    signal: aborter.signal,
    preventAbort: true,
    preventCancel: true,
  });
  await pulled.promise;
  aborter.abort(reason);
  const failure = await piping.then(() => undefined, error => error);
  if (failure !== reason) throw Error("prevent flags changed abort reason");
  if (cancels !== 0 || aborts !== 0) throw Error("prevent flags allowed propagation");
  if (pendingSource.locked || pendingSink.locked) throw Error("preventCancel left a stream locked");
  sourceController.enqueue("still readable");
  const reader = pendingSource.getReader();
  const result = await reader.read();
  if (result.done || result.value !== "still readable") throw Error("preventCancel changed the source");
  reader.cancel();
  finishPull.resolve();
  await Promise.resolve();
  console.log("prevent flags ok");
})()
`)
	if out != "prevent flags ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsPipeToUnlocksOnWriterAcquisitionFailure(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const source = new ReadableStream({start() {}});
  const sink = new WritableStream();
  const existingWriter = sink.getWriter();
  const failure = await source.pipeTo(sink).then(() => undefined, error => error);
  if (!(failure instanceof TypeError)) throw Error("writer acquisition failure was lost");
  if (source.locked) throw Error("writer acquisition failure leaked the source reader");
  if (!sink.locked) throw Error("writer acquisition failure disturbed the existing writer");
  existingWriter.releaseLock();
  console.log("writer acquisition cleanup ok");
})()
`)
	if out != "writer acquisition cleanup ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsPipeToUnlocksOnSignalListenerErrors(t *testing.T) {
	for _, phase := range []string{"register", "cleanup"} {
		t.Run(phase, func(t *testing.T) {
			out, _ := run(t, stdlib.Config{}, fmt.Sprintf(`
;(async () => {
  const reason = Error("%s failure");
  const signal = new AbortController().signal;
  if (%q === "register") {
    signal.addEventListener = () => { throw reason; };
  } else {
    const add = signal.addEventListener;
    signal.addEventListener = function(type, listener) { return add.call(this, type, listener); };
    signal.removeEventListener = () => { throw reason; };
  }
  const source = ReadableStream.from([]);
  const sink = new WritableStream();
  const failure = await source.pipeTo(sink, {signal}).then(() => undefined, error => error);
  if (failure !== reason) throw Error("signal method failure was lost");
  if (source.locked || sink.locked) throw Error("signal method failure leaked a stream lock");
  console.log("signal %s cleanup ok");
})()
`, phase, phase, phase))
			if out != "signal "+phase+" cleanup ok" {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

func TestStreamsPipeToHandlesSinkFailureAndClosureDuringPendingRead(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const pulled = deferred(), finishPull = deferred();
  const sourceReason = Error("source should be cancelled");
  let sourceCancelReason;
  const source = new ReadableStream({
    pull() { pulled.resolve(); return finishPull.promise; },
    cancel(reason) { sourceCancelReason = reason; },
  }, {highWaterMark: 0});
  let sinkController;
  const sink = new WritableStream({start(controller) { sinkController = controller; }});
  const piping = source.pipeTo(sink);
  await pulled.promise;
  sinkController.error(sourceReason);
  const failure = await piping.then(() => undefined, error => error);
  if (failure !== sourceReason || sourceCancelReason !== sourceReason) throw Error("sink error did not cancel source with its reason");
  if (source.locked || sink.locked) throw Error("sink error left a stream locked");
  finishPull.resolve();

  const pulledAgain = deferred(), finishPullAgain = deferred();
  let closeCancelReason;
  const pendingSource = new ReadableStream({
    pull() { pulledAgain.resolve(); return finishPullAgain.promise; },
    cancel(reason) { closeCancelReason = reason; },
  }, {highWaterMark: 0});
  let closed = 0;
  const pendingSink = new WritableStream({close() { closed++; }});
  const closing = pendingSource.pipeTo(pendingSink);
  await pulledAgain.promise;
  await pendingSink._close();
  const closeFailure = await closing.then(() => undefined, error => error);
  if (!(closeFailure instanceof TypeError) || closeCancelReason !== closeFailure) throw Error("sink closure did not cancel pending source");
  if (closed !== 1 || pendingSource.locked || pendingSink.locked) throw Error("sink closure left state or locks incorrect");
  finishPullAgain.resolve();
  await Promise.resolve();
  console.log("pending source failure and sink close ok");
})()
`)
	if out != "pending source failure and sink close ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsPipeToHandlesSourceClosureWhileWriterReadyIsPending(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const source = ReadableStream.from([]);
  let closeCount = 0;
  const sink = new WritableStream({close() { closeCount++; }}, {highWaterMark: 0});
  await source.pipeTo(sink);
  if (closeCount !== 1) throw Error("EOF did not close the sink exactly once");
  if (source.locked || sink.locked) throw Error("EOF left a stream locked");

  const unclosedSource = ReadableStream.from([]);
  let preventedCloseCount = 0;
  const unclosedSink = new WritableStream({
    write() {},
    close() { preventedCloseCount++; },
  }, {highWaterMark: 0});
  await unclosedSource.pipeTo(unclosedSink, {preventClose: true});
  if (preventedCloseCount !== 0) throw Error("preventClose was ignored after EOF");
  if (unclosedSource.locked || unclosedSink.locked) throw Error("preventClose after EOF left a stream locked");
  const writer = unclosedSink.getWriter();
  await writer.write("still writable");
  await writer.close();
  writer.releaseLock();

  const deferred = () => {
    let resolve;
    const promise = new Promise(r => { resolve = r; });
    return {promise, resolve};
  };
  const pulled = deferred(), finishPull = deferred();
  let sourceController;
  const delayedSource = new ReadableStream({
    start(controller) { sourceController = controller; },
    pull() { pulled.resolve(); return finishPull.promise; },
  }, {highWaterMark: 0});
  let delayedCloseCount = 0;
  const delayedSink = new WritableStream({close() { delayedCloseCount++; }});
  const piping = delayedSource.pipeTo(delayedSink);
  await pulled.promise;
  sourceController.close();
  await piping;
  if (delayedCloseCount !== 1) throw Error("source EOF raced into a duplicate sink close");
  if (delayedSource.locked || delayedSink.locked) throw Error("delayed EOF left a stream locked");
  finishPull.resolve();
  console.log("source EOF while writer ready is pending ok");
})()
`)
	if out != "source EOF while writer ready is pending ok" {
		t.Fatalf("output = %q", out)
	}
}
