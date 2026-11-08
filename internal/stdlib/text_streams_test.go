package stdlib

import (
	"context"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func textStreamsRuntime(t *testing.T) *quickjs.Runtime {
	t.Helper()
	rt := quickjs.New()
	t.Cleanup(func() { _ = rt.Close() })
	if err := Install(rt, Config{}); err != nil {
		t.Fatal(err)
	}
	return rt
}

func evalTextStreams(t *testing.T, rt *quickjs.Runtime, source string) quickjs.Value {
	t.Helper()
	value, err := rt.Eval(source)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return value
}

func awaitTextStreams(t *testing.T, value quickjs.Value) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := value.Await(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTextStreamsMetadataBrandingAndDefaultOptions(t *testing.T) {
	rt := textStreamsRuntime(t)
	got := evalTextStreams(t, rt, `(() => {
  const encoder = new TextEncoderStream();
  const decoder = new TextDecoderStream(" UTF8 ", {fatal: true, ignoreBOM: true});
  const protoKeys = value => Object.keys(Object.getPrototypeOf(value));
  const getter = (ctor, name) => Object.getOwnPropertyDescriptor(ctor.prototype, name);
  const receiver = (ctor, name) => {
    try { getter(ctor, name).get.call({}); return "accepted"; }
    catch (error) { return error.name; }
  };
  const oldFatal = Object.getOwnPropertyDescriptor(Object.prototype, "fatal");
  const oldIgnoreBOM = Object.getOwnPropertyDescriptor(Object.prototype, "ignoreBOM");
  let defaults;
  try {
    Object.defineProperty(Object.prototype, "fatal", {
      configurable: true, get() { throw Error("inherited fatal lookup"); }
    });
    Object.defineProperty(Object.prototype, "ignoreBOM", {
      configurable: true, get() { throw Error("inherited ignoreBOM lookup"); }
    });
    defaults = [
      new TextDecoderStream().fatal,
      new TextDecoderStream("utf-8", undefined).ignoreBOM,
      new TextDecoderStream("utf-8", null).fatal
    ];
  } finally {
    if (oldFatal) Object.defineProperty(Object.prototype, "fatal", oldFatal);
    else delete Object.prototype.fatal;
    if (oldIgnoreBOM) Object.defineProperty(Object.prototype, "ignoreBOM", oldIgnoreBOM);
    else delete Object.prototype.ignoreBOM;
  }
  return JSON.stringify({
    encoderLength: TextEncoderStream.length,
    decoderLength: TextDecoderStream.length,
    encoderKeys: Object.keys(encoder),
    decoderKeys: Object.keys(decoder),
    encoderMembers: protoKeys(encoder),
    decoderMembers: protoKeys(decoder),
    encoderTag: Object.prototype.toString.call(encoder),
    decoderTag: Object.prototype.toString.call(decoder),
    encoding: encoder.encoding,
    decoderEncoding: decoder.encoding,
    fatal: decoder.fatal,
    ignoreBOM: decoder.ignoreBOM,
    defaults,
    readonly: getter(TextEncoderStream, "readable").set === undefined &&
      getter(TextDecoderStream, "writable").set === undefined,
    enumerable: getter(TextEncoderStream, "encoding").enumerable &&
      getter(TextDecoderStream, "fatal").enumerable &&
      getter(TextDecoderStream, "readable").enumerable,
    receiver: receiver(TextEncoderStream, "encoding") + "/" +
      receiver(TextDecoderStream, "fatal"),
    transformIdentity: encoder instanceof TransformStream || decoder instanceof TransformStream,
    streamIdentity: encoder.readable === encoder.readable && encoder.writable === encoder.writable
  });
})()`).String()
	want := `{"encoderLength":0,"decoderLength":0,"encoderKeys":[],"decoderKeys":[],"encoderMembers":["encoding","readable","writable"],"decoderMembers":["encoding","fatal","ignoreBOM","readable","writable"],"encoderTag":"[object TextEncoderStream]","decoderTag":"[object TextDecoderStream]","encoding":"utf-8","decoderEncoding":"utf-8","fatal":true,"ignoreBOM":true,"defaults":[false,false,false],"readonly":true,"enumerable":true,"receiver":"TypeError/TypeError","transformIdentity":false,"streamIdentity":true}`
	if got != want {
		t.Fatalf("metadata = %s, want %s", got, want)
	}
}

func TestTextStreamsModuleAndGlobalIdentity(t *testing.T) {
	rt := textStreamsRuntime(t)
	ns, err := rt.EvalModule("<text-stream-module-test>", `
import * as web from "stream/web";
import * as nodeWeb from "node:stream/web";
export const same = web.TextEncoderStream === TextEncoderStream &&
  web.TextDecoderStream === TextDecoderStream &&
  web.default.TextEncoderStream === TextEncoderStream &&
  web.default.TextDecoderStream === TextDecoderStream &&
  nodeWeb.TextEncoderStream === TextEncoderStream &&
  nodeWeb.TextDecoderStream === TextDecoderStream;
`)
	if err != nil {
		t.Fatal(err)
	}
	same, err := ns.Get("same")
	if err != nil {
		t.Fatal(err)
	}
	if !same.Bool() {
		t.Fatal("stream module constructors do not match the globals")
	}
}

func TestTextEncoderStreamPreservesSplitSurrogates(t *testing.T) {
	rt := textStreamsRuntime(t)
	value := awaitTextStreams(t, evalTextStreams(t, rt, `(async () => {
  const stream = new TextEncoderStream();
  const chunks = [];
  await ReadableStream.from([
    "\ud83d", "", "\udca9", "A\ud800", "", "B\udc00", "Z\ud800"
  ]).pipeThrough(stream).pipeTo(new WritableStream({
    write(bytes) { chunks.push(new TextDecoder().decode(bytes)); }
  }));
  return JSON.stringify(chunks);
})()`))
	got := value.String()
	want := `["💩","A","�B�","Z","�"]`
	if got != want {
		t.Fatalf("encoded chunks = %s, want %s", got, want)
	}
}

func TestTextEncoderStreamRejectsSymbolChunks(t *testing.T) {
	rt := textStreamsRuntime(t)
	got := awaitTextStreams(t, evalTextStreams(t, rt, `(async () => {
  const stream = new TextEncoderStream();
  try {
    await ReadableStream.from([Symbol("text")]).pipeThrough(stream).pipeTo(
      new WritableStream({write() {}}));
    return "accepted";
  } catch (error) {
    return error.name;
  }
})()`)).String()
	if got != "TypeError" {
		t.Fatalf("symbol chunk result = %q, want TypeError", got)
	}
}

func TestTextDecoderStreamPreservesUTF8AndBOMState(t *testing.T) {
	rt := textStreamsRuntime(t)
	value := awaitTextStreams(t, evalTextStreams(t, rt, `(async () => {
  async function collect(stream, chunks) {
    const output = [];
    await ReadableStream.from(chunks).pipeThrough(stream).pipeTo(
      new WritableStream({write(text) { output.push(text); }}));
    return output;
  }
  const normal = await collect(new TextDecoderStream(), [
    new Uint8Array([0xef]), new Uint8Array([0xbb]),
    new Uint8Array([0xbf, 0xf0, 0x9f]), new Uint8Array([0x92, 0xa9, 0x41])
  ]);
  const ignore = await collect(new TextDecoderStream("utf8", {ignoreBOM: true}), [
    new Uint8Array([0xef, 0xbb, 0xbf]), new Uint8Array([0x41])
  ]);
  const bounded = await collect(new TextDecoderStream(), [
    new Uint8Array([0x58, 0x41, 0x59]).subarray(1, 2)
  ]);
  return JSON.stringify({normal, ignore, bounded});
})()`))
	want := `{"normal":["💩A"],"ignore":["` + "\ufeff" + `","A"],"bounded":["A"]}`
	if got := value.String(); got != want {
		t.Fatalf("decoded chunks = %s, want %s", got, want)
	}
}

func TestTextDecoderStreamRequiresBufferSourceAndFlushesErrors(t *testing.T) {
	rt := textStreamsRuntime(t)
	value := awaitTextStreams(t, evalTextStreams(t, rt, `(async () => {
  async function writeOne(chunk, options) {
    const stream = new TextDecoderStream("utf-8", options);
    try {
      await ReadableStream.from([chunk]).pipeThrough(stream).pipeTo(
        new WritableStream({write() {}}));
      return "accepted";
    } catch (error) {
      return error.name;
    }
  }
  async function pipe(chunks, options) {
    const stream = new TextDecoderStream("utf-8", options);
    const output = [];
    try {
      await ReadableStream.from(chunks).pipeThrough(stream).pipeTo(
        new WritableStream({write(text) { output.push(text); }}));
      return {result: "ok", output};
    } catch (error) {
      return {result: error.name, output};
    }
  }
  return JSON.stringify({
    invalid: await Promise.all([undefined, null, 1, {}].map(chunk => writeOne(chunk))),
    replacement: await pipe([new Uint8Array([0xe2])]),
    fatalByte: await pipe([new Uint8Array([0xff])], {fatal: true}),
    fatalEOF: await pipe([new Uint8Array([0xe2])], {fatal: true})
  });
})()`))
	want := `{"invalid":["TypeError","TypeError","TypeError","TypeError"],"replacement":{"result":"ok","output":["�"]},"fatalByte":{"result":"TypeError","output":[]},"fatalEOF":{"result":"TypeError","output":[]}}`
	if got := value.String(); got != want {
		t.Fatalf("buffer source and EOF results = %s, want %s", got, want)
	}
}

func TestTextDecoderStreamDelegatesConstructorConversions(t *testing.T) {
	rt := textStreamsRuntime(t)
	got := evalTextStreams(t, rt, `(() => {
  const order = [];
  try {
    new TextDecoderStream("latin1", {
      get fatal() { order.push("fatal"); return false; },
      get ignoreBOM() { order.push("ignoreBOM"); return false; }
    });
  } catch (error) { order.push(error.name); }
  let primitive;
  try { new TextDecoderStream("utf-8", 1); }
  catch (error) { primitive = error.name; }
  let symbol;
  try { new TextDecoderStream(Symbol("utf-8")); }
  catch (error) { symbol = error.name; }
  return JSON.stringify({order, primitive, symbol});
})()`).String()
	want := `{"order":["fatal","ignoreBOM","RangeError"],"primitive":"TypeError","symbol":"TypeError"}`
	if got != want {
		t.Fatalf("constructor conversions = %s, want %s", got, want)
	}
}

func TestTextStreamsCanBeInstalledBeforeTextCodecs(t *testing.T) {
	rt := quickjs.New()
	t.Cleanup(func() { _ = rt.Close() })
	if err := Streams(rt); err != nil {
		t.Fatal(err)
	}
	got := evalTextStreams(t, rt, `(() => {
  const installed = typeof TextEncoderStream === "function" &&
    typeof TextDecoderStream === "function";
  let encoderError, decoderError;
  try { new TextEncoderStream(); } catch (error) { encoderError = error.name; }
  try { new TextDecoderStream(); } catch (error) { decoderError = error.name; }
  return JSON.stringify({installed, encoderError, decoderError});
})()`).String()
	if got != `{"installed":true,"encoderError":"TypeError","decoderError":"TypeError"}` {
		t.Fatalf("Streams-only setup = %s", got)
	}
	if err := WebAPIs(rt, nil); err != nil {
		t.Fatal(err)
	}
	got = evalTextStreams(t, rt, `JSON.stringify([
  new TextEncoderStream().encoding,
  new TextDecoderStream().encoding
])`).String()
	if got != `[`+`"utf-8","utf-8"`+`]` {
		t.Fatalf("text streams after installing codecs = %s", got)
	}
}
