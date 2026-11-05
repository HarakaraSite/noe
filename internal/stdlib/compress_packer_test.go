package stdlib

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestCompressionPackerOutputOwnership(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		t.Run(format, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			packer, err := (&compressHost{rt: rt}).packer(format)
			if err != nil {
				t.Fatal(err)
			}
			write, _ := packer.Get("write")
			closePacker, _ := packer.Get("close")
			data := make([]byte, 64<<10)
			rand.New(rand.NewSource(1)).Read(data)
			input := rt.NewBytes(data)
			var values []quickjs.Value
			var snapshots [][]byte
			for range 4 {
				part, err := write.Call(input)
				if err != nil {
					t.Fatal(err)
				}
				contents, _ := part.Bytes()
				values = append(values, part)
				snapshots = append(snapshots, bytes.Clone(contents))
			}
			tail, err := closePacker.Call()
			if err != nil {
				t.Fatal(err)
			}
			var compressed bytes.Buffer
			for i, value := range values {
				contents, _ := value.Bytes()
				if !bytes.Equal(contents, snapshots[i]) {
					t.Fatal("a later write or close changed earlier output")
				}
				compressed.Write(contents)
			}
			contents, _ := tail.Bytes()
			compressed.Write(contents)
			reader, err := readerFor(format, &compressed)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := io.ReadAll(reader)
			reader.Close()
			if err != nil || !bytes.Equal(decoded, bytes.Repeat(data, 4)) {
				t.Fatalf("round trip: %v", err)
			}
			empty, err := closePacker.Call()
			contents, _ = empty.Bytes()
			if err != nil || len(contents) != 0 {
				t.Fatal("a repeated close should produce no data")
			}
		})
	}
}

func BenchmarkCompressionPacker(b *testing.B) {
	for _, size := range []int{64 << 10, 1 << 20} {
		b.Run(fmt.Sprint(size), func(b *testing.B) {
			rt := quickjs.New()
			defer rt.Close()
			data := make([]byte, size)
			rand.New(rand.NewSource(1)).Read(data)
			input := rt.NewBytes(data)
			c := &compressHost{rt: rt}
			b.SetBytes(int64(size))
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				packer, err := c.packer("gzip")
				if err != nil {
					b.Fatal(err)
				}
				write, _ := packer.Get("write")
				closePacker, _ := packer.Get("close")
				if _, err := write.Call(input); err != nil {
					b.Fatal(err)
				}
				if _, err := closePacker.Call(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
