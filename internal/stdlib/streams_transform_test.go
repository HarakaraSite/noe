package stdlib

import (
	"context"
	"fmt"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func transformRuntime(t *testing.T) *quickjs.Runtime {
	t.Helper()
	rt := quickjs.New()
	t.Cleanup(func() { rt.Close() })
	if err := Streams(rt); err != nil {
		t.Fatal(err)
	}
	return rt
}

func evalTransform(t *testing.T, rt *quickjs.Runtime, source string) quickjs.Value {
	t.Helper()
	value, err := rt.Eval(source)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func drainTransformJobs(t *testing.T, rt *quickjs.Runtime) {
	t.Helper()
	if err := rt.RunJobs(); err != nil {
		t.Fatal(err)
	}
}

func awaitTransform(t *testing.T, value quickjs.Value) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := value.Await(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTransformStreamBackpressure(t *testing.T) {
	for _, test := range []struct {
		name, strategy string
		beforeReading  int
	}{
		{"default", "{}", 0},
		{"zero", "{highWaterMark: 0}", 0},
		{"one", "{highWaterMark: 1}", 1},
		{"byte-size", "{highWaterMark: 131072, size: chunk => chunk.byteLength}", 2},
		{"unbounded", "{highWaterMark: Infinity}", 256},
	} {
		t.Run(test.name, func(t *testing.T) {
			rt := transformRuntime(t)
			evalTransform(t, rt, fmt.Sprintf(`
const stream = new TransformStream({}, {}, %s);
const writer = stream.writable.getWriter();
let completed = 0;
const producing = (async () => {
  for (let i = 0; i < 256; i++) {
    const chunk = new Uint8Array(65536);
    chunk[0] = i;
    await writer.write(chunk);
    completed++;
  }
  await writer.close();
})();
`, test.strategy))
			drainTransformJobs(t, rt)
			if got := evalTransform(t, rt, "completed").Int(); got != test.beforeReading {
				t.Fatalf("writes completed without reading = %d, want %d", got, test.beforeReading)
			}
			t.Logf("without reading: %d completed writes, %d bytes of queued output", test.beforeReading, test.beforeReading*65536)
			result := awaitTransform(t, evalTransform(t, rt, `(async () => {
  const reader = stream.readable.getReader();
  let chunks = 0, bytes = 0;
  for (;;) {
    const next = await reader.read();
    if (next.done) break;
    if (next.value[0] !== chunks) throw Error("chunk order");
    chunks++;
    bytes += next.value.byteLength;
  }
  await producing;
  await reader.closed;
  await writer.closed;
  return chunks === 256 && bytes === 16777216;
})()`))
			if !result.Bool() {
				t.Fatal("stream did not resume and drain all output")
			}
		})
	}
}

func TestTransformStreamTerminalReleasesWrites(t *testing.T) {
	for _, action := range []string{"cancel", "abort", "error", "terminate"} {
		for _, reason := range []string{"({message: 'stop'})", "undefined", "0", "false"} {
			if action == "terminate" && reason != "undefined" {
				continue
			}
			t.Run(action+"/"+reason, func(t *testing.T) {
				rt := transformRuntime(t)
				evalTransform(t, rt, fmt.Sprintf(`
const reason = %s;
let controller, calls = 0, cancelled = 0, cancelReason;
const stream = new TransformStream({
  start(c) { controller = c; },
  transform(chunk, c) { calls++; c.enqueue(chunk); },
  cancel(reason) { cancelled++; cancelReason = reason; },
});
const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
const writing = Promise.allSettled([writer.write(1), writer.write(2), writer.ready, writer.close(), writer.closed]);
const readingClosed = Promise.allSettled([reader.closed]);
`, reason))
				drainTransformJobs(t, rt)
				if got := evalTransform(t, rt, "calls").Int(); got != 0 {
					t.Fatalf("transform ran without demand: %d", got)
				}
				source := map[string]string{
					"cancel":    "reader.cancel(reason)",
					"abort":     "writer.abort(reason)",
					"error":     "controller.error(reason)",
					"terminate": "controller.terminate()",
				}[action]
				cancelCalls := 0
				if action == "cancel" || action == "abort" {
					cancelCalls = 1
				}
				evalTransform(t, rt, "const stopping = Promise.resolve("+source+");")
				result := awaitTransform(t, evalTransform(t, rt, fmt.Sprintf(`(async () => {
  await stopping;
  const results = await writing;
  const isReason = %s ? error => error instanceof TypeError : error => error === reason;
  if (results.some(result => result.status !== "rejected" || !isReason(result.reason))) throw Error("write, close, ready, or closed did not reject with the reason");
  const terminalRead = await reader.read().then(value => ({value}), error => ({error}));
  const closed = (await readingClosed)[0];
  if (%s) {
    if (!terminalRead.value?.done || closed.status !== "fulfilled") throw Error("readable should close");
  } else if (terminalRead.error !== reason || closed.status !== "rejected" || closed.reason !== reason) throw Error("readable should reject with the reason");
  return calls === 0 && cancelled === %d && (cancelled === 0 || cancelReason === reason);
})()`, fmt.Sprint(action == "terminate"), fmt.Sprint(action == "cancel" || action == "terminate"), cancelCalls)))
				if !result.Bool() {
					t.Fatal("unexpected transform or cancel callback")
				}
			})
		}
	}
}

func TestTransformStreamCallbackFailures(t *testing.T) {
	for _, callback := range []string{"start", "transform", "flush"} {
		for _, asynchronous := range []bool{false, true} {
			if callback == "start" && !asynchronous {
				continue // A synchronous start failure throws from the constructor.
			}
			t.Run(fmt.Sprintf("%s/async=%t", callback, asynchronous), func(t *testing.T) {
				rt := transformRuntime(t)
				async := ""
				if asynchronous {
					async = "async "
				}
				result := awaitTransform(t, evalTransform(t, rt, fmt.Sprintf(`(async () => {
  const reason = {message: "callback failed"};
  const stream = new TransformStream({%s%s() { throw reason; }});
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const reading = (async () => { while (!(await reader.read()).done) {} })();
  const writing = (async () => { await writer.write("chunk"); await writer.close(); })();
  const results = await Promise.allSettled([reading, writing, reader.closed, writer.closed]);
  return results.every(result => result.status === "rejected" && result.reason === reason);
})()`, async, callback)))
				if !result.Bool() {
					t.Fatal("callback failure did not reach both sides")
				}
			})
		}
	}
}

func TestTransformStreamStartThrows(t *testing.T) {
	rt := transformRuntime(t)
	if !evalTransform(t, rt, `(() => {
  const reason = {message: "start failed"};
  try { new TransformStream({start() { throw reason; }}); }
  catch (error) { return error === reason; }
  return false;
})()`).Bool() {
		t.Fatal("a synchronous start failure did not throw from the constructor")
	}
}

func TestTransformStreamCancelCallbackFailure(t *testing.T) {
	for _, action := range []string{"cancel", "abort"} {
		t.Run(action, func(t *testing.T) {
			rt := transformRuntime(t)
			result := awaitTransform(t, evalTransform(t, rt, fmt.Sprintf(`(async () => {
  const stop = {message: "stop"}, failure = {message: "cleanup failed"};
  const stream = new TransformStream({async cancel() { throw failure; }});
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const writes = Promise.allSettled([writer.write(1), writer.closed]);
  const reads = Promise.allSettled([reader.closed]);
  const stopping = %s;
  const stopped = (await Promise.allSettled([stopping]))[0];
  if (stopped.status !== "rejected" || stopped.reason !== failure) throw Error("cancel callback failure was lost");
  const writeReason = %s ? failure : stop;
  if ((await writes).some(result => result.status !== "rejected" || result.reason !== writeReason)) throw Error("blocked write did not settle");
  const read = (await reads)[0];
  return %s ? read.status === "fulfilled" : read.status === "rejected" && read.reason === failure;
})()`, map[string]string{"cancel": "reader.cancel(stop)", "abort": "writer.abort(stop)"}[action], fmt.Sprint(action == "cancel"), fmt.Sprint(action == "cancel"))))
			if !result.Bool() {
				t.Fatal("cancel callback failure reached the wrong side")
			}
		})
	}
}

func TestTransformStreamAsyncStart(t *testing.T) {
	rt := transformRuntime(t)
	evalTransform(t, rt, `
let release, calls = 0;
const stream = new TransformStream({
  start() { return new Promise(resolve => { release = resolve; }); },
  transform(chunk, c) { calls++; c.enqueue(chunk); },
});
const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
const reading = reader.read();
const writing = writer.write("chunk");
`)
	drainTransformJobs(t, rt)
	if evalTransform(t, rt, "calls").Int() != 0 {
		t.Fatal("transform ran before asynchronous start completed")
	}
	evalTransform(t, rt, "release()")
	result := awaitTransform(t, evalTransform(t, rt, `(async () => {
  if ((await reading).value !== "chunk") throw Error("missing output");
  await writing;
  await writer.close();
  return (await reader.read()).done && calls === 1;
})()`))
	if !result.Bool() {
		t.Fatal("asynchronous start did not resume the pipe")
	}
}

func TestTransformStreamEnqueueFailure(t *testing.T) {
	rt := transformRuntime(t)
	result := awaitTransform(t, evalTransform(t, rt, `(async () => {
  const reason = {message: "size failed"};
  const stream = new TransformStream({}, {}, {highWaterMark: 1, size() { throw reason; }});
  const writer = stream.writable.getWriter(), reader = stream.readable.getReader();
  const writes = Promise.allSettled([writer.write(1), writer.write(2), writer.closed]);
  const results = await writes;
  results.push(...await Promise.allSettled([reader.read(), reader.closed]));
  return results.every(result => result.status === "rejected" && result.reason === reason);
})()`))
	if !result.Bool() {
		t.Fatal("enqueue failure did not reach both sides")
	}
}

func TestTransformStreamSlowSink(t *testing.T) {
	rt := transformRuntime(t)
	evalTransform(t, rt, `
let made = 0, taken = 0, release;
const source = new ReadableStream({pull(c) {
  if (made === 256) { c.close(); return; }
  const chunk = new Uint8Array(65536);
  chunk[0] = made++;
  c.enqueue(chunk);
}}, {highWaterMark: 0});
const pipeline = source.pipeThrough(new TransformStream()).pipeTo(new WritableStream({
  write(chunk) {
    if (chunk[0] !== taken++) throw Error("sink order");
    if (taken === 1) return new Promise(resolve => { release = resolve; });
  },
}));
`)
	drainTransformJobs(t, rt)
	if made := evalTransform(t, rt, "made").Int(); made > 2 || made < 1 {
		t.Fatalf("source chunks produced while sink is stalled = %d, want 1 or 2", made)
	}
	if taken := evalTransform(t, rt, "taken").Int(); taken != 1 {
		t.Fatalf("sink accepted %d chunks, want 1", taken)
	}
	evalTransform(t, rt, "release()")
	awaitTransform(t, evalTransform(t, rt, "pipeline"))
	if taken := evalTransform(t, rt, "taken").Int(); taken != 256 {
		t.Fatalf("sink accepted %d chunks after release, want 256", taken)
	}
}

func TestTransformStreamEmptyOutputAndFlush(t *testing.T) {
	rt := transformRuntime(t)
	result := awaitTransform(t, evalTransform(t, rt, `(async () => {
  let calls = 0;
  const transform = new TransformStream({
    transform(chunk, c) {
      calls++;
      if (chunk === 3) { c.enqueue("a"); c.enqueue("b"); }
    },
    flush(c) { c.enqueue("tail"); },
  });
  const result = [];
  await ReadableStream.from([1, 2, 3, 4, 5]).pipeThrough(transform)
    .pipeTo(new WritableStream({write(chunk) { result.push(chunk); }}));
  return calls === 5 && result.join(",") === "a,b,tail";
})()`))
	if !result.Bool() {
		t.Fatal("empty-output transform or flush stalled the pipe")
	}
}

func TestTransformStreamDerivedStreams(t *testing.T) {
	for _, test := range []struct {
		factory, format string
	}{
		{"new TextEncoderStream()", ""}, {"new TextDecoderStream()", ""},
		{`new CompressionStream("gzip")`, "gzip"},
		{`new CompressionStream("deflate")`, "deflate"},
		{`new CompressionStream("deflate-raw")`, "deflate-raw"},
	} {
		t.Run(test.factory, func(t *testing.T) {
			rt := transformRuntime(t)
			if err := WebAPIs(rt, nil); err != nil {
				t.Fatal(err)
			}
			if err := Compression(rt); err != nil {
				t.Fatal(err)
			}
			evalTransform(t, rt, fmt.Sprintf(`
const stream = %s;
const writer = stream.writable.getWriter();
const plain = "hé😀";
const input = stream instanceof TextEncoderStream ? plain : new TextEncoder().encode(plain);
let completed = false;
const writing = writer.write(input).then(() => { completed = true; });
`, test.factory))
			drainTransformJobs(t, rt)
			if evalTransform(t, rt, "completed").Bool() {
				t.Fatal("derived stream accepted a write without output demand")
			}
			result := awaitTransform(t, evalTransform(t, rt, fmt.Sprintf(`(async () => {
  const readable = stream instanceof CompressionStream
    ? stream.readable.pipeThrough(new DecompressionStream(%q)) : stream.readable;
  const chunks = [];
  const reading = (async () => { for await (const chunk of readable) chunks.push(chunk); })();
  await writing;
  await writer.close();
  await reading;
  let text;
  if (stream instanceof TextDecoderStream) text = chunks.join("");
  else {
    const bytes = new Uint8Array(chunks.reduce((sum, chunk) => sum + chunk.length, 0));
    let at = 0;
    for (const chunk of chunks) { bytes.set(chunk, at); at += chunk.length; }
    text = new TextDecoder().decode(bytes);
  }
  return text === plain;
})()`, test.format)))
			if !result.Bool() {
				t.Fatal("derived stream did not drain correctly")
			}
		})
	}
}
