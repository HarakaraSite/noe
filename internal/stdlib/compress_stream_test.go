package stdlib

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func compressionRuntime(t *testing.T, withLoop bool) (*quickjs.Runtime, *Loop) {
	t.Helper()
	rt := quickjs.New()
	t.Cleanup(func() { rt.Close() })
	var loop *Loop
	if withLoop {
		loop = NewLoop(rt)
		t.Cleanup(loop.Close)
	}
	if err := Streams(rt); err != nil {
		t.Fatal(err)
	}
	if err := compression(rt, loop); err != nil {
		t.Fatal(err)
	}
	return rt, loop
}

func awaitCompression(t *testing.T, value quickjs.Value) (quickjs.Value, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return value.Await(ctx)
}

func compressedFixture(t *testing.T, format string) (front, tail, plain []byte) {
	t.Helper()
	plain = make([]byte, 128<<10)
	rand.New(rand.NewSource(1)).Read(plain)
	var packed bytes.Buffer
	w, err := writerFor(format, &packed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain[:64<<10]); err != nil {
		t.Fatal(err)
	}
	if err := w.(interface{ Flush() error }).Flush(); err != nil {
		t.Fatal(err)
	}
	front = bytes.Clone(packed.Bytes())
	if _, err := w.Write(plain[64<<10:]); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	tail = bytes.Clone(packed.Bytes()[len(front):])
	return
}

func TestDecompressionBeforeInputEOF(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		for _, withLoop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/loop=%t", format, withLoop), func(t *testing.T) {
				rt, _ := compressionRuntime(t, withLoop)
				front, tail, plain := compressedFixture(t, format)
				rt.Set("front", rt.NewBytes(front))
				rt.Set("tail", rt.NewBytes(tail))
				rt.Set("format", format)
				promise, err := rt.Eval(`(async () => {
  const stream = new DecompressionStream(format);
  const writer = stream.writable.getWriter(), reader = stream.readable.getReader();
  const writing = writer.write(front);
  const first = await reader.read();
  if (first.done || !first.value.length) throw Error("no output before input EOF");
  const chunks = [first.value];
  const draining = (async () => {
    for (;;) { const next = await reader.read(); if (next.done) return; chunks.push(next.value); }
  })();
  await writing;
  await writer.write(tail);
  await writer.close();
  await draining;
  await reader.closed;
  const total = chunks.reduce((sum, chunk) => sum + chunk.length, 0);
  const bytes = new Uint8Array(total);
  let at = 0;
  for (const chunk of chunks) { bytes.set(chunk, at); at += chunk.length; }
  return bytes;
})()`)
				if err != nil {
					t.Fatal(err)
				}
				result, err := awaitCompression(t, promise)
				data, _ := result.Bytes()
				if err != nil || !bytes.Equal(data, plain) {
					t.Fatalf("stream round trip: bytes=%d error=%v", len(data), err)
				}
			})
		}
	}
}

func TestDecompressionInputFailures(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		for _, kind := range []string{"invalid", "truncated", "checksum"} {
			if kind == "checksum" && format == "deflate-raw" {
				continue
			}
			t.Run(format+"/"+kind, func(t *testing.T) {
				rt, _ := compressionRuntime(t, false)
				front, tail, _ := compressedFixture(t, format)
				data := append(front, tail...)
				if kind == "invalid" {
					data = []byte{255, 255, 255, 255}
				} else if kind == "truncated" {
					data = data[:len(data)-9]
				} else {
					data[len(data)-1] ^= 255
				}
				rt.Set("packed", rt.NewBytes(data))
				rt.Set("format", format)
				promise, err := rt.Eval(`(async () => {
  const stream = new DecompressionStream(format);
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const reading = (async () => { while (!(await reader.read()).done) {} })();
  const writing = (async () => { await writer.write(packed); await writer.close(); })();
  const results = await Promise.allSettled([reading, writing]);
  if (results.some(result => result.status !== "rejected" || !(result.reason instanceof Error))) throw Error("missing decode rejection");
  return true;
})()`)
				if err != nil {
					t.Fatal(err)
				}
				value, err := awaitCompression(t, promise)
				if err != nil || !value.Bool() {
					t.Fatalf("decode failure propagation: %v", err)
				}
			})
		}
	}
}

func TestDecompressionCancelUnblocksInput(t *testing.T) {
	for _, how := range []string{"cancel", "abort"} {
		t.Run(how, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				rt, _ := compressionRuntime(t, false)
				front, tail, _ := compressedFixture(t, "gzip")
				rt.Set("packed", rt.NewBytes(append(front, tail...)))
				if _, err := rt.Eval(`
const stream = new DecompressionStream("gzip");
const writer = stream.writable.getWriter();
const reason = {cancelled: true};
globalThis.writeSettled = false;
const writing = writer.write(packed).then(() => { writeSettled = true; return false; }, error => { writeSettled = true; return error === reason; });
`); err != nil {
					t.Fatal(err)
				}
				synctest.Wait()
				if err := rt.RunJobs(); err != nil {
					t.Fatal(err)
				}
				value, _ := rt.Get("writeSettled")
				if value.Bool() {
					t.Fatal("input was consumed without output demand")
				}
				source := `stream.readable.cancel(reason)`
				if how == "abort" {
					source = `writer.abort(reason)`
				}
				promise, err := rt.Eval(`(async () => { await ` + source + `; return await writing; })()`)
				if err != nil {
					t.Fatal(err)
				}
				value, err = awaitCompression(t, promise)
				if err != nil || !value.Bool() {
					t.Fatalf("pending write did not reject with cancellation reason: %v", err)
				}
			})
		})
	}
}

func TestDecompressionCloseJoinsWorkers(t *testing.T) {
	for _, scoped := range []bool{false, true} {
		t.Run(fmt.Sprintf("scope=%t", scoped), func(t *testing.T) {
			rt, loop := compressionRuntime(t, scoped)
			var scope *ExecutionScope
			if scoped {
				var err error
				scope, err = NewExecutionScope(loop, context.Background())
				if err != nil {
					t.Fatal(err)
				}
			}
			u := &streamInflater{rt: rt, loop: loop, format: "gzip", limit: maxInflated, notice: rt.NewPromise()}
			u.read()
			if scoped {
				scope.Close()
			} else {
				rt.Close()
			}
			select {
			case <-u.done:
			default:
				t.Fatal("decoder survived its owner closing")
			}
			if scoped {
				if err := rt.RunJobs(); err != nil {
					t.Fatal(err)
				}
				value, err := rt.Eval(`6 * 7`)
				if err != nil || value.Int() != 42 {
					t.Fatal("scope cleanup damaged the borrowed runtime")
				}
			}
		})
	}
}

func TestDecompressionInputSnapshot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		rt, _ := compressionRuntime(t, false)
		front, tail, plain := compressedFixture(t, "gzip")
		input := rt.NewBytes(append(front, tail...))
		u := &streamInflater{rt: rt, format: "gzip", limit: maxInflated, notice: rt.NewPromise()}
		defer u.abort()
		writing := u.write(input)
		// Both workers are blocked by absent output demand. Mutating the
		// original JS buffer must not change the Go-owned pending input.
		synctest.Wait()
		original, _ := input.Bytes()
		clear(original)
		var decoded []byte
		for len(decoded) < len(plain) {
			value, err := awaitCompression(t, u.read().Value())
			if err != nil {
				t.Fatal(err)
			}
			data, _ := value.Bytes()
			decoded = append(decoded, data...)
		}
		if _, err := awaitCompression(t, writing.Value()); err != nil {
			t.Fatal(err)
		}
		closing := u.closeInput()
		if _, err := awaitCompression(t, u.read().Value()); err != nil {
			t.Fatal(err)
		}
		if _, err := awaitCompression(t, closing.Value()); err != nil || !bytes.Equal(decoded, plain) {
			t.Fatalf("input snapshot changed: %v", err)
		}
	})
}

func TestDecompressionUnusedIsIdle(t *testing.T) {
	rt, _ := compressionRuntime(t, false)
	if _, err := rt.Eval(`new DecompressionStream("gzip")`); err != nil {
		t.Fatal(err)
	}
	if rt.Busy() {
		t.Fatal("an unused decoder keeps the runtime busy")
	}
}

func TestDecompressionCumulativeLimit(t *testing.T) {
	rt, _ := compressionRuntime(t, false)
	var compressed bytes.Buffer
	w, _ := writerFor("gzip", &compressed)
	w.Write(bytes.Repeat([]byte("x"), 96<<10))
	w.Close()
	u := &streamInflater{rt: rt, format: "gzip", limit: 48 << 10, notice: rt.NewPromise()}
	defer u.abort()
	if _, err := awaitCompression(t, u.write(rt.NewBytes(compressed.Bytes())).Value()); err != nil {
		t.Fatal(err)
	}
	closing := u.closeInput()
	var total int
	for {
		value, err := awaitCompression(t, u.read().Value())
		if err != nil {
			if !strings.Contains(err.Error(), "RangeError") || total <= 0 || int64(total) > u.limit {
				t.Fatalf("limit: total=%d error=%v", total, err)
			}
			break
		}
		data, _ := value.Bytes()
		total += len(data)
	}
	if _, err := awaitCompression(t, closing.Value()); err == nil || !strings.Contains(err.Error(), "RangeError") {
		t.Fatalf("close did not report the cumulative limit: %v", err)
	}
}
