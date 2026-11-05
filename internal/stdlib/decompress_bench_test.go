package stdlib_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"math/rand"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func BenchmarkDecompressionStream(b *testing.B) {
	for _, size := range []int{1024, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) { benchmarkDecompressionStream(b, size) })
	}
}

func benchmarkDecompressionStream(b *testing.B, size int) {
	data := make([]byte, size)
	rand.New(rand.NewSource(1)).Read(data)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	w.Write(data)
	w.Close()
	rt := quickjs.New()
	defer rt.Close()
	if err := stdlib.Streams(rt); err != nil {
		b.Fatal(err)
	}
	if err := stdlib.Compression(rt); err != nil {
		b.Fatal(err)
	}
	input := rt.NewBytes(compressed.Bytes())
	batch, err := rt.Eval(`(async function(input) {
  let at = 0, total = 0;
  const source = new ReadableStream({pull(controller) {
    if (at === input.length) { controller.close(); return; }
    const end = Math.min(at + 16384, input.length);
    controller.enqueue(input.subarray(at, end));
    at = end;
  }});
  for await (const chunk of source.pipeThrough(new DecompressionStream("gzip"))) total += chunk.length;
  return total;
})`)
	if err != nil {
		b.Fatal(err)
	}
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		promise, err := batch.Call(input)
		if err != nil {
			b.Fatal(err)
		}
		result, err := promise.Await(context.Background())
		if err != nil || result.Int() != len(data) {
			b.Fatalf("decoded=%v error=%v", result, err)
		}
	}
}
