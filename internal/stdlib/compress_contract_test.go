package stdlib

import (
	"bytes"
	"fmt"
	"testing"
)

func contractCompressed(t *testing.T, format string, plain []byte) []byte {
	t.Helper()
	var packed bytes.Buffer
	writer, err := writerFor(format, &packed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.Clone(packed.Bytes())
}

func withZlibPresetDictionary(data []byte) []byte {
	flg := (data[1] | 0x20) & 0xe0
	for check := byte(0); check < 32; check++ {
		candidate := flg | check
		if ((int(data[0])<<8)|int(candidate))%31 == 0 {
			return append([]byte{data[0], candidate, 0, 0, 0, 1}, data[2:]...)
		}
	}
	panic("could not construct a valid zlib FDICT header")
}

func TestCompressionConstructorContract(t *testing.T) {
	rt, _ := compressionRuntime(t, false)
	value, err := rt.Eval(`(() => {
  const attempt = fn => { try { fn(); return "accepted"; } catch (error) { return error.name; } };
  const conversions = [];
  new CompressionStream({[Symbol.toPrimitive]() { conversions.push("compression"); return "gzip"; }});
  new DecompressionStream({toString() { conversions.push("decompression"); return "deflate"; }});
  return JSON.stringify({
    missing: [attempt(() => new CompressionStream()), attempt(() => new DecompressionStream())],
    undefined: [attempt(() => new CompressionStream(undefined)), attempt(() => new DecompressionStream(undefined))],
    symbol: [attempt(() => new CompressionStream(Symbol("gzip"))), attempt(() => new DecompressionStream(Symbol("gzip")))],
    unsupported: [attempt(() => new CompressionStream("brotli")), attempt(() => new DecompressionStream("brotli"))],
    arity: [CompressionStream.length, DecompressionStream.length],
    conversions
  });
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"missing":["TypeError","TypeError"],"undefined":["TypeError","TypeError"],"symbol":["TypeError","TypeError"],"unsupported":["TypeError","TypeError"],"arity":[1,1],"conversions":["compression","decompression"]}`
	if got := value.String(); got != want {
		t.Fatalf("constructor conversions = %s, want %s", got, want)
	}
}

func TestCompressionStreamInterfaceBrandAndClone(t *testing.T) {
	rt, _ := compressionRuntime(t, false)
	if err := WebAPIs(rt, nil); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(`(() => {
  const attempt = fn => { try { fn(); return "accepted"; } catch (error) { return error.name; } };
  return JSON.stringify([CompressionStream, DecompressionStream].map((Constructor, index) => {
    const name = index === 0 ? "CompressionStream" : "DecompressionStream";
    const stream = new Constructor("gzip");
    const readable = Object.getOwnPropertyDescriptor(Constructor.prototype, "readable");
    const writable = Object.getOwnPropertyDescriptor(Constructor.prototype, "writable");
    return {
      tag: Object.prototype.toString.call(stream),
      readableEnumerable: readable.enumerable,
      writableEnumerable: writable.enumerable,
      readonly: readable.set === undefined && writable.set === undefined &&
        attempt(() => { (function() { "use strict"; stream.readable = null; })(); }) === "TypeError",
      ownAttributes: Object.prototype.hasOwnProperty.call(stream, "readable") ||
        Object.prototype.hasOwnProperty.call(stream, "writable"),
      sameStreams: stream.readable === stream.readable && stream.writable === stream.writable,
      wrongReceiver: attempt(() => readable.get.call({})),
      transformSubclass: stream instanceof TransformStream,
      cloned: attempt(() => structuredClone(stream)),
      constructorName: name
    };
  }));
})()`)
	if err != nil {
		t.Fatal(err)
	}
	want := `[{"tag":"[object CompressionStream]","readableEnumerable":true,"writableEnumerable":true,"readonly":true,"ownAttributes":false,"sameStreams":true,"wrongReceiver":"TypeError","transformSubclass":false,"cloned":"DataCloneError","constructorName":"CompressionStream"},{"tag":"[object DecompressionStream]","readableEnumerable":true,"writableEnumerable":true,"readonly":true,"ownAttributes":false,"sameStreams":true,"wrongReceiver":"TypeError","transformSubclass":false,"cloned":"DataCloneError","constructorName":"DecompressionStream"}]`
	if got := value.String(); got != want {
		t.Fatalf("stream interface shape = %s, want %s", got, want)
	}
	ns, err := rt.EvalModule("<compression-global-module-identity>", `
import * as zlib from "zlib";
import * as nodeZlib from "node:zlib";
export const same = zlib.createGzip() instanceof CompressionStream &&
  zlib.createGunzip() instanceof DecompressionStream &&
  nodeZlib.createDeflate() instanceof CompressionStream &&
  nodeZlib.createInflate() instanceof DecompressionStream &&
  zlib.createGzip === nodeZlib.createGzip;
`)
	if err != nil {
		t.Fatal(err)
	}
	same, err := ns.Get("same")
	if err != nil || !same.Bool() {
		t.Fatalf("global and zlib stream identity: %v", err)
	}
}

func TestCompressionBufferSourceContract(t *testing.T) {
	rt, _ := compressionRuntime(t, false)
	value, err := rt.Eval(`(async () => {
  async function collect(readable) {
    const reader = readable.getReader(), chunks = [];
    let length = 0;
    for (;;) {
      const result = await reader.read();
      if (result.done) break;
      chunks.push(result.value);
      length += result.value.byteLength;
    }
    const output = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) { output.set(chunk, offset); offset += chunk.byteLength; }
    return output;
  }
  function bytes(value) {
    if (ArrayBuffer.isView(value)) return new Uint8Array(value.buffer, value.byteOffset, value.byteLength).slice();
    return new Uint8Array(value).slice();
  }
  function equal(left, right) {
    return left.length === right.length && left.every((byte, index) => byte === right[index]);
  }
  async function roundTrip(format, chunk) {
    const expected = bytes(chunk);
    const source = new ReadableStream({start(controller) { controller.enqueue(chunk); controller.close(); }});
    const packed = await collect(source.pipeThrough(new CompressionStream(format)));
    const recovered = await collect(ReadableStream.from([packed]).pipeThrough(new DecompressionStream(format)));
    if (!equal(expected, recovered)) throw new Error(format + " changed the BufferSource byte span");
  }
  async function writeResult(Constructor, chunk) {
    try {
      await ReadableStream.from([chunk]).pipeThrough(new Constructor("gzip")).pipeTo(
        new WritableStream({write() {}}));
      return "accepted";
    } catch (error) { return error.name; }
  }

  const backing = new Uint8Array([99, 1, 2, 3, 88]);
  const samples = [
    backing.buffer,
    backing.subarray(1, 4),
    new DataView(backing.buffer, 2, 2),
    new Uint16Array(new Uint8Array([4, 5, 6, 7]).buffer),
  ];
  const sharedInputs = [];
  if (typeof SharedArrayBuffer === "function") {
    const shared = new SharedArrayBuffer(3);
    new Uint8Array(shared).set([8, 9, 10]);
    sharedInputs.push(shared, new Uint8Array(shared), new DataView(shared));
  }
  for (const format of ["gzip", "deflate", "deflate-raw"]) {
    for (const sample of samples) await roundTrip(format, sample);
  }

  const invalid = [undefined, null, "bytes", [1, 2], {}, 1];
  const invalidCompression = [];
  const invalidDecompression = [];
  for (const chunk of invalid) {
    invalidCompression.push(await writeResult(CompressionStream, chunk));
    invalidDecompression.push(await writeResult(DecompressionStream, chunk));
  }
  const sharedCompression = [];
  const sharedDecompression = [];
  for (const chunk of sharedInputs) {
    sharedCompression.push(await writeResult(CompressionStream, chunk));
    sharedDecompression.push(await writeResult(DecompressionStream, chunk));
  }

  const detached = new ArrayBuffer(4);
  new Uint8Array(detached).set([1, 2, 3, 4]);
  detached.transfer();
  const detachedResult = await writeResult(CompressionStream, detached);

  const resizable = new ArrayBuffer(4, {maxByteLength: 8});
  const resizableResult = resizable.resizable
    ? await writeResult(CompressionStream, resizable) : "unsupported-runtime";

  if (invalidCompression.some(name => name !== "TypeError") ||
      invalidDecompression.some(name => name !== "TypeError")) {
    throw new Error("non-BufferSource stream chunks were accepted: " +
      JSON.stringify({invalidCompression, invalidDecompression}));
  }
  if (sharedCompression.some(name => name !== "TypeError") ||
      sharedDecompression.some(name => name !== "TypeError")) {
    throw new Error("SharedArrayBuffer stream chunks were accepted: " +
      JSON.stringify({sharedCompression, sharedDecompression}));
  }
  if (detachedResult !== "accepted") throw new Error("detached BufferSource was not treated as empty");
  if (resizableResult !== "unsupported-runtime" && resizableResult !== "TypeError") {
    throw new Error("resizable BufferSource result = " + resizableResult);
  }
  return true;
})()`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := awaitCompression(t, value)
	if err != nil || !result.Bool() {
		t.Fatalf("BufferSource contract: %v", err)
	}
}

func TestDecompressionRejectsMalformedAndTrailingInput(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		plain := []byte("compression stream contract")
		fixture := contractCompressed(t, format, plain)
		cases := map[string][]byte{
			"invalid":   {0xff, 0xff, 0xff, 0xff},
			"truncated": bytes.Clone(fixture[:len(fixture)-1]),
			"trailing":  append(bytes.Clone(fixture), 0),
		}
		if format == "gzip" {
			cases["multiple-members"] = append(bytes.Clone(fixture), fixture...)
			for _, flag := range []byte{0x20, 0x40, 0x80} {
				reserved := bytes.Clone(fixture)
				reserved[3] |= flag
				cases[fmt.Sprintf("reserved-flg-%02x", flag)] = reserved
			}
		}
		if format == "deflate" {
			cases["preset-dictionary"] = withZlibPresetDictionary(fixture)
		}
		for kind, data := range cases {
			t.Run(format+"/"+kind, func(t *testing.T) {
				rt, _ := compressionRuntime(t, false)
				rt.Set("format", format)
				rt.Set("packed", rt.NewBytes(data))
				value, err := rt.Eval(`(async () => {
  const stream = new DecompressionStream(format);
  const reader = stream.readable.getReader(), writer = stream.writable.getWriter();
  const reading = (async () => { while (!(await reader.read()).done) {} })();
  const writing = (async () => { await writer.write(packed); await writer.close(); })();
  const outcomes = await Promise.allSettled([reading, writing]);
  if (outcomes.some(result => result.status !== "rejected" || result.reason.name !== "TypeError")) {
    throw new Error("malformed input outcomes: " + JSON.stringify(outcomes.map(result =>
      result.status === "rejected" ? result.reason.name : result.status)));
  }
  return true;
})()`)
				if err != nil {
					t.Fatal(err)
				}
				result, err := awaitCompression(t, value)
				if err != nil || !result.Bool() {
					t.Fatalf("malformed input: %v", err)
				}
			})
		}
	}
}

func TestDecompressionTrailingWriteRejectsItsChunk(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		t.Run(format, func(t *testing.T) {
			valid := contractCompressed(t, format, []byte("valid member"))
			data := append(bytes.Clone(valid), 0)
			rt, _ := compressionRuntime(t, false)
			rt.Set("format", format)
			rt.Set("packed", rt.NewBytes(data))
			rt.Set("valid", rt.NewBytes(valid))
			value, err := rt.Eval(`(async () => {
  function drain(reader) {
    return (async () => { while (!(await reader.read()).done) {} })();
  }

  const sameChunk = new DecompressionStream(format);
  const sameWriter = sameChunk.writable.getWriter(), sameReader = sameChunk.readable.getReader();
  const sameReading = drain(sameReader).then(() => "accepted", error => error.name);
  const sameWriting = await sameWriter.write(packed).then(() => "accepted", error => error.name);
  if (sameWriting !== "TypeError" || await sameReading !== "TypeError") {
    throw new Error("same-chunk trailing results: " + JSON.stringify({sameWriting}));
  }

  const separate = new DecompressionStream(format);
  const separateWriter = separate.writable.getWriter(), separateReader = separate.readable.getReader();
  const separateReading = drain(separateReader).then(() => "accepted", error => error.name);
  const firstWrite = await separateWriter.write(valid).then(() => "accepted", error => error.name);
  const trailingWrite = await separateWriter.write(new Uint8Array([0])).then(
    () => "accepted", error => error.name);
  if (firstWrite !== "accepted" || trailingWrite !== "TypeError" ||
      await separateReading !== "TypeError") {
    throw new Error("separate trailing results: " + JSON.stringify({firstWrite, trailingWrite}));
  }
  return true;
})()`)
			if err != nil {
				t.Fatal(err)
			}
			result, err := awaitCompression(t, value)
			if err != nil || !result.Bool() {
				t.Fatalf("trailing chunk was not rejected by write: %v", err)
			}
		})
	}
}

func TestDecompressionByteAtATimeAndEmptyClose(t *testing.T) {
	for _, format := range []string{"gzip", "deflate", "deflate-raw"} {
		t.Run(format, func(t *testing.T) {
			plain := []byte("byte by byte")
			rt, _ := compressionRuntime(t, false)
			rt.Set("format", format)
			rt.Set("packed", rt.NewBytes(contractCompressed(t, format, plain)))
			rt.Set("plain", rt.NewBytes(plain))
			value, err := rt.Eval(`(async () => {
  const stream = new DecompressionStream(format);
  const writer = stream.writable.getWriter(), reader = stream.readable.getReader();
  const reading = (async () => {
    const chunks = [];
    for (;;) {
      const result = await reader.read();
      if (result.done) break;
      chunks.push(result.value);
    }
    const length = chunks.reduce((total, chunk) => total + chunk.byteLength, 0);
    const output = new Uint8Array(length);
    let offset = 0;
    for (const chunk of chunks) { output.set(chunk, offset); offset += chunk.byteLength; }
    return output;
  })();
  for (const byte of packed) await writer.write(new Uint8Array([byte]));
  await writer.close();
  const output = await reading;
  if (output.length !== plain.length || !output.every((byte, index) => byte === plain[index])) {
    throw new Error("byte-at-a-time input changed output");
  }

  const empty = new DecompressionStream(format);
  const emptyWriter = empty.writable.getWriter(), emptyReader = empty.readable.getReader();
  const emptyRead = emptyReader.read().then(() => "accepted", error => error.name);
  let emptyClose;
  try { await emptyWriter.close(); emptyClose = "accepted"; }
  catch (error) { emptyClose = error.name; }
  if (await emptyRead !== "TypeError" || emptyClose !== "TypeError") {
    throw new Error("empty close results: " + JSON.stringify({emptyClose}));
  }
  return true;
})()`)
			if err != nil {
				t.Fatal(err)
			}
			result, err := awaitCompression(t, value)
			if err != nil || !result.Bool() {
				t.Fatalf("byte-at-a-time decompression or empty close: %v", err)
			}
		})
	}
}
