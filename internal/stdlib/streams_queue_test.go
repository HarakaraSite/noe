package stdlib_test

import (
	"context"
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestStreamsQueueOrderAndSize(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  let controller;
  const stream = new ReadableStream({start(c) { controller = c; }}, {
    highWaterMark: 1024, size: chunk => chunk === undefined ? 0 : chunk.size,
  });
  const reader = stream.getReader();
  let queued = 0;
  const checkSize = () => {
    if (controller.desiredSize !== 1024 - queued) throw Error("queue size");
  };
  const read = async expected => {
    const result = await reader.read();
    if (result.done || result.value !== expected) throw Error("chunk order or identity");
    queued -= expected.size;
    checkSize();
  };
  for (let round = 0; round < 3; round++) {
    const chunks = Array.from({length: 257}, (_, i) => ({round, i, size: i % 3}));
    for (const chunk of chunks) { controller.enqueue(chunk); queued += chunk.size; }
    checkSize();
    for (let i = 0; i < 191; i++) await read(chunks[i]);
    const tail = Array.from({length: 37}, (_, i) => ({round, i, size: 2}));
    for (const chunk of tail) { controller.enqueue(chunk); queued += chunk.size; }
    for (let i = 191; i < chunks.length; i++) await read(chunks[i]);
    for (const chunk of tail) await read(chunk);
  }
  controller.enqueue(undefined);
  const empty = await reader.read();
  if (empty.done || empty.value !== undefined) throw Error("undefined chunk");
  controller.close();
  if (!(await reader.read()).done) throw Error("EOF");
  await reader.closed;
  reader.releaseLock();
  if (stream.locked) throw Error("reader lock");
  console.log("queue order and size ok");
})()
`)
	if out != "queue order and size ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsTeeSlowReader(t *testing.T) {
	out, _ := run(t, stdlib.Config{}, `
;(async () => {
  const chunks = Array.from({length: 257}, (_, i) => new Uint8Array([i & 255, i >> 8]));
  const source = new ReadableStream({start(controller) {
    for (const chunk of chunks) controller.enqueue(chunk);
    controller.close();
  }});
  const [fast, slow] = source.tee();
  const drain = async stream => {
    let index = 0;
    for await (const chunk of stream) {
      if (chunk !== chunks[index++]) throw Error("tee order or chunk identity");
    }
    if (index !== chunks.length) throw Error("tee EOF");
  };
  await drain(fast);
  await drain(slow);
  console.log("slow tee reader ok");
})()
`)
	if out != "slow tee reader ok" {
		t.Fatalf("output = %q", out)
	}
}

func TestStreamsPendingReadsTerminalState(t *testing.T) {
	for _, terminal := range []string{"close", "cancel", "error"} {
		t.Run(terminal, func(t *testing.T) {
			out, _ := run(t, stdlib.Config{}, fmt.Sprintf(`
;(async () => {
  let controller, cancelled;
  const stream = new ReadableStream({
    start(c) { controller = c; }, cancel(reason) { cancelled = reason; },
  });
  const reader = stream.getReader();
  const reason = {terminal: %q};
  const pending = Array.from({length: 257}, () => reader.read());
  let refused = false;
  try { reader.releaseLock(); } catch (error) { refused = error instanceof TypeError; }
  if (!refused) throw Error("pending reader was released");
  for (let i = 0; i < 191; i++) controller.enqueue(i);
  const results = pending.map(p => p.then(value => ({value}), error => ({error})));
  if (reason.terminal === "close") controller.close();
  else if (reason.terminal === "cancel") await reader.cancel(reason);
  else controller.error(reason);
  const settled = await Promise.all(results);
  for (let i = 0; i < settled.length; i++) {
    if (i < 191) {
      if (settled[i].value.done || settled[i].value.value !== i) throw Error("pending read order");
    } else if (reason.terminal === "error") {
      if (settled[i].error !== reason) throw Error("error identity");
    } else if (!settled[i].value.done) throw Error("pending EOF");
  }
  if (reason.terminal === "cancel" && cancelled !== reason) throw Error("cancel reason");
  if (reason.terminal === "error") {
    if (await reader.closed.then(() => false, error => error === reason) !== true) throw Error("closed error");
  } else await reader.closed;
  reader.releaseLock();
  console.log("pending reads ok");
})()
`, terminal))
			if out != "pending reads ok" {
				t.Fatalf("output = %q", out)
			}
		})
	}
}

// Measure complete public stream operations, including small queues where a
// different queue representation must still handle the usual streaming case.
func BenchmarkStreamsQueue(b *testing.B) {
	for _, pending := range []bool{false, true} {
		mode := "buffered"
		if pending {
			mode = "pending"
		}
		for _, chunks := range []int{1, 64, 1024} {
			b.Run(fmt.Sprintf("%s/%d", mode, chunks), func(b *testing.B) {
				rt := quickjs.New()
				defer rt.Close()
				if err := stdlib.Streams(rt); err != nil {
					b.Fatal(err)
				}
				batch, err := rt.Eval(`(async function(count, pending) {
  let controller;
  const stream = new ReadableStream({start(c) { controller = c; }});
  const reader = stream.getReader();
  let sum = 0;
  if (pending) {
    const reads = Array.from({length: count}, () => reader.read());
    for (let i = 0; i < count; i++) controller.enqueue(i);
    controller.close();
    const results = await Promise.all(reads);
    for (let i = 0; i < count; i++) {
      if (results[i].done || results[i].value !== i) throw Error("pending FIFO");
      sum += results[i].value;
    }
  } else {
    for (let i = 0; i < count; i++) controller.enqueue(i);
    controller.close();
    for (let i = 0; i < count; i++) {
      const result = await reader.read();
      if (result.done || result.value !== i) throw Error("buffered FIFO");
      sum += result.value;
    }
  }
  if (!(await reader.read()).done) throw Error("EOF");
  reader.releaseLock();
  return sum;
})`)
				if err != nil {
					b.Fatal(err)
				}
				b.ReportAllocs()
				b.ResetTimer()
				for range b.N {
					promise, err := batch.Call(chunks, pending)
					if err != nil {
						b.Fatal(err)
					}
					value, err := promise.Await(context.Background())
					if err != nil || value.Int() != chunks*(chunks-1)/2 {
						b.Fatalf("sum = %v, error = %v", value, err)
					}
				}
			})
		}
	}
}
