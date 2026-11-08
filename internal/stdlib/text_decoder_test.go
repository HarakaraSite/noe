package stdlib

import (
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func textDecoderRuntime(t *testing.T) *quickjs.Runtime {
	t.Helper()
	rt := quickjs.New()
	t.Cleanup(func() { _ = rt.Close() })
	constructor, err := newTextDecoder(rt, rt.NewObject())
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("TextDecoder", constructor); err != nil {
		t.Fatal(err)
	}
	return rt
}

func evalTextDecoder(t *testing.T, rt *quickjs.Runtime, source string) string {
	t.Helper()
	value, err := rt.Eval(source)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return value.String()
}

func TestTextDecoderUTF8ReplacementBoundaries(t *testing.T) {
	rt := textDecoderRuntime(t)
	got := evalTextDecoder(t, rt, `(() => {
  const points = bytes => Array.from(
    new TextDecoder().decode(new Uint8Array(bytes)),
    character => character.codePointAt(0));
  return JSON.stringify({
    overlongLead: points([0xc0, 0x80]),
    truncated: points([0xe2, 0x82]),
    badContinuation: points([0xe2, 0x28, 0xa1]),
    tooLowThreeByte: points([0xe0, 0x80, 0x80]),
    surrogate: points([0xed, 0xa0, 0x80]),
    tooHighFourByte: points([0xf4, 0x90, 0x80, 0x80]),
    validBoundaries: points([
      0xc2, 0x80, 0xe0, 0xa0, 0x80, 0xed, 0x9f, 0xbf,
      0xf0, 0x90, 0x80, 0x80, 0xf4, 0x8f, 0xbf, 0xbf
    ])
  });
})()`)
	want := `{"overlongLead":[65533,65533],"truncated":[65533],"badContinuation":[65533,40,65533],"tooLowThreeByte":[65533,65533,65533],"surrogate":[65533,65533,65533],"tooHighFourByte":[65533,65533,65533,65533],"validBoundaries":[128,2048,55295,65536,1114111]}`
	if got != want {
		t.Fatalf("replacement boundaries = %s, want %s", got, want)
	}
}

func TestTextDecoderStreamingEOFAndBOM(t *testing.T) {
	rt := textDecoderRuntime(t)
	got := evalTextDecoder(t, rt, `(() => {
  const decoder = new TextDecoder();
  const scalar = [
    decoder.decode(new Uint8Array([0xe2]), {stream: true}),
    decoder.decode(undefined, {stream: true}),
    decoder.decode(new Uint8Array([0x82, 0xac]), {stream: true}),
    decoder.decode()
  ].join("|");

  const incomplete = new TextDecoder();
  const flushed = [
    incomplete.decode(new Uint8Array([0xe2, 0x82]), {stream: true}),
    incomplete.decode(),
    incomplete.decode(new Uint8Array([0x41]))
  ].join("|");

  const bom = new TextDecoder();
  const splitBOM = [
    bom.decode(new Uint8Array([0xef]), {stream: true}),
    bom.decode(new Uint8Array([0xbb]), {stream: true}),
    bom.decode(new Uint8Array([0xbf, 0x41]), {stream: true}),
    bom.decode()
  ].join("|");

  const normalBOM = new TextDecoder().decode(new Uint8Array([0xef, 0xbb, 0xbf, 0x41]));
  const ignoredBOM = new TextDecoder("utf-8", {ignoreBOM: true})
    .decode(new Uint8Array([0xef, 0xbb, 0xbf, 0x41]));
  const onlyLeading = new TextDecoder().decode(
    new Uint8Array([0x41, 0xef, 0xbb, 0xbf, 0x42]));
  return JSON.stringify({scalar, flushed, splitBOM, normalBOM, ignoredBOM, onlyLeading});
})()`)
	want := `{"scalar":"||€|","flushed":"|�|A","splitBOM":"||A|","normalBOM":"A","ignoredBOM":"` + "\ufeff" + `A","onlyLeading":"A` + "\ufeff" + `B"}`
	if got != want {
		t.Fatalf("streaming and BOM output = %s, want %s", got, want)
	}
}

func TestTextDecoderBufferSourceAndOptions(t *testing.T) {
	rt := textDecoderRuntime(t)
	got := evalTextDecoder(t, rt, `(() => {
  const buffer = new Uint8Array([0x58, 0x41, 0x42, 0x59]);
  const view = new TextDecoder().decode(buffer.subarray(1, 3));
  const dataView = new TextDecoder().decode(new DataView(buffer.buffer, 2, 1));
  const labels = [
    new TextDecoder(" \tUTF8\r\n").encoding,
    new TextDecoder("unicode-1-1-utf-8").encoding,
    new TextDecoder("unicode11utf8").encoding,
    new TextDecoder("unicode20utf8").encoding,
    new TextDecoder("x-unicode20utf8").encoding,
    new TextDecoder({toString() { return " UTF-8 " }}).encoding
  ];
  const optionOrder = [];
  const decoder = new TextDecoder("utf-8", {
    get fatal() { optionOrder.push("fatal"); return 1; },
    get ignoreBOM() { optionOrder.push("ignoreBOM"); return 0; }
  });
  const invalid = value => {
    try { decoder.decode(value); return "accepted"; }
    catch (error) { return error.name; }
  };
  const labelsErrors = [
    (() => { try { new TextDecoder("latin1"); } catch (error) { return error.name; } })(),
    (() => { try { new TextDecoder(Symbol("utf-8")); } catch (error) { return error.name; } })(),
    (() => { try { new TextDecoder("latin1", 1); } catch (error) { return error.name; } })()
  ];
  const conversionOrder = [];
  try {
    new TextDecoder("latin1", {
      get fatal() { conversionOrder.push("fatal"); return false; },
      get ignoreBOM() { conversionOrder.push("ignoreBOM"); return false; }
    });
  } catch (error) { conversionOrder.push(error.name); }
  let primitiveDecodeOptions;
  try { new TextDecoder().decode(new Uint8Array([0x41]), 1); }
  catch (error) { primitiveDecodeOptions = error.name; }
  const originalFatal = Object.getOwnPropertyDescriptor(Object.prototype, "fatal");
  const originalStream = Object.getOwnPropertyDescriptor(Object.prototype, "stream");
  let defaultDictionaries;
  try {
    Object.defineProperty(Object.prototype, "fatal", {configurable: true, get() { throw Error("fatal default lookup"); }});
    Object.defineProperty(Object.prototype, "stream", {configurable: true, get() { throw Error("stream default lookup"); }});
    defaultDictionaries = [
      new TextDecoder("utf8").fatal,
      new TextDecoder("utf8", undefined).fatal,
      new TextDecoder("utf8", null).fatal,
      new TextDecoder().decode(new Uint8Array([0x41]))
    ];
  } finally {
    if (originalFatal) Object.defineProperty(Object.prototype, "fatal", originalFatal);
    else delete Object.prototype.fatal;
    if (originalStream) Object.defineProperty(Object.prototype, "stream", originalStream);
    else delete Object.prototype.stream;
  }
  const shared = typeof SharedArrayBuffer === "undefined" ? "unavailable" : (() => {
    const bytes = new Uint8Array(new SharedArrayBuffer(1));
    bytes[0] = 0x51;
    return new TextDecoder().decode(bytes);
  })();
  const resizable = "resizable" in ArrayBuffer.prototype
    ? (() => {
        const bytes = new ArrayBuffer(1, {maxByteLength: 2});
        try { new TextDecoder().decode(bytes); return "accepted"; }
        catch (error) { return error.name; }
      })()
    : "unavailable";
  const growable = typeof SharedArrayBuffer !== "undefined" && "growable" in SharedArrayBuffer.prototype
    ? (() => {
        const bytes = new SharedArrayBuffer(1, {maxByteLength: 2});
        try { new TextDecoder().decode(bytes); return "accepted"; }
        catch (error) { return error.name; }
      })()
    : "unavailable";
  return JSON.stringify({
    view, dataView, labels, optionOrder, fatal: decoder.fatal,
    ignoreBOM: decoder.ignoreBOM, labelsErrors, shared, resizable, growable,
    conversionOrder, primitiveDecodeOptions, defaultDictionaries,
    plainArray: invalid([0x41]), nullInput: invalid(null),
    plainObject: invalid({0: 0x41, length: 1})
  });
})()`)
	want := `{"view":"AB","dataView":"B","labels":["utf-8","utf-8","utf-8","utf-8","utf-8","utf-8"],"optionOrder":["fatal","ignoreBOM"],"fatal":true,"ignoreBOM":false,"labelsErrors":["RangeError","TypeError","TypeError"],"shared":"Q","resizable":"TypeError","growable":"TypeError","conversionOrder":["fatal","ignoreBOM","RangeError"],"primitiveDecodeOptions":"TypeError","defaultDictionaries":[false,false,false,"A"],"plainArray":"TypeError","nullInput":"TypeError","plainObject":"TypeError"}`
	if got != want {
		t.Fatalf("BufferSource and options = %s, want %s", got, want)
	}
}

func TestTextDecoderFatalResetAndConversionErrors(t *testing.T) {
	rt := textDecoderRuntime(t)
	got := evalTextDecoder(t, rt, `(() => {
  const fatal = new TextDecoder("utf-8", {fatal: true});
  const first = fatal.decode(new Uint8Array([0x41]));
  let malformed;
  try { fatal.decode(new Uint8Array([0xff])); }
  catch (error) { malformed = error.name; }
  const afterFatal = fatal.decode(new Uint8Array([0x42]));

  fatal.decode(new Uint8Array([0xe2]), {stream: true});
  let incomplete;
  try { fatal.decode(); }
  catch (error) { incomplete = error.name; }
  const afterEOF = fatal.decode(new Uint8Array([0x43]));

  const streaming = new TextDecoder();
  streaming.decode(new Uint8Array([0xe2]), {stream: true});
  let invalidInput;
  try { streaming.decode([0x41]); }
  catch (error) { invalidInput = error.name; }
  const afterInvalidInput = streaming.decode(new Uint8Array([0x82, 0xac]), {stream: true});

  const optionsError = new TextDecoder();
  optionsError.decode(new Uint8Array([0xe2]), {stream: true});
  let invalidOptions;
  try {
    optionsError.decode(new Uint8Array([0x41]), {
      get stream() { throw new Error("options conversion"); }
    });
  } catch (error) { invalidOptions = error.message; }
  const afterInvalidOptions = optionsError.decode(new Uint8Array([0x82, 0xac]), {stream: true});

  const streamFatal = new TextDecoder("utf-8", {fatal: true});
  let streamError;
  try { streamFatal.decode(new Uint8Array([0x41, 0xff, 0x42]), {stream: true}); }
  catch (error) { streamError = error.name; }
  const afterStreamError = streamFatal.decode(new Uint8Array([0x44]), {stream: true});

  const continuationFatal = new TextDecoder("utf-8", {fatal: true});
  continuationFatal.decode(new Uint8Array([0xe2]), {stream: true});
  let continuationError;
  try { continuationFatal.decode(new Uint8Array([0x28, 0x41]), {stream: true}); }
  catch (error) { continuationError = error.name; }
  const afterContinuationError = continuationFatal.decode(new Uint8Array([0x42]), {stream: true});

  return JSON.stringify({first, malformed, afterFatal, incomplete, afterEOF,
    invalidInput, afterInvalidInput, invalidOptions, afterInvalidOptions,
    streamError, afterStreamError, continuationError, afterContinuationError});
})()`)
	want := `{"first":"A","malformed":"TypeError","afterFatal":"B","incomplete":"TypeError","afterEOF":"C","invalidInput":"TypeError","afterInvalidInput":"€","invalidOptions":"options conversion","afterInvalidOptions":"€","streamError":"TypeError","afterStreamError":"BD","continuationError":"TypeError","afterContinuationError":"(AB"}`
	if got != want {
		t.Fatalf("fatal and conversion state = %s, want %s", got, want)
	}
}

func TestTextDecoderBrandedReadonlyMetadata(t *testing.T) {
	rt := textDecoderRuntime(t)
	got := evalTextDecoder(t, rt, `(() => {
  const decoder = new TextDecoder("utf8", {fatal: true, ignoreBOM: true});
  let getterReceiver, methodReceiver;
  try { Object.getOwnPropertyDescriptor(TextDecoder.prototype, "fatal").get.call({}); }
  catch (error) { getterReceiver = error.name; }
  try { TextDecoder.prototype.decode.call({}); }
  catch (error) { methodReceiver = error.name; }
  const writable = Reflect.set(decoder, "fatal", false);
  return JSON.stringify({
    encoding: decoder.encoding, fatal: decoder.fatal, ignoreBOM: decoder.ignoreBOM,
    ownKeys: Object.keys(decoder), writable, fatalAfterWrite: decoder.fatal,
    getterReceiver, methodReceiver,
    encodingArity: TextDecoder.length,
    decodeArity: TextDecoder.prototype.decode.length,
    prototypeKeys: Object.keys(TextDecoder.prototype),
    tag: Object.prototype.toString.call(decoder)
  });
})()`)
	want := `{"encoding":"utf-8","fatal":true,"ignoreBOM":true,"ownKeys":[],"writable":false,"fatalAfterWrite":true,"getterReceiver":"TypeError","methodReceiver":"TypeError","encodingArity":0,"decodeArity":0,"prototypeKeys":["encoding","fatal","ignoreBOM","decode"],"tag":"[object TextDecoder]"}`
	if got != want {
		t.Fatalf("decoder metadata = %s, want %s", got, want)
	}
}

func TestTextDecoderDetachedBufferSourcesAreEmpty(t *testing.T) {
	rt := textDecoderRuntime(t)
	if _, err := rt.Eval(`
  globalThis.detachedArrayBuffer = new ArrayBuffer(2);
  globalThis.detachedViewBuffer = new ArrayBuffer(2);
  globalThis.detachedDataView = new DataView(detachedViewBuffer);
`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"detachedArrayBuffer", "detachedViewBuffer"} {
		buffer, err := rt.Eval(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.DetachArrayBuffer(buffer); err != nil {
			t.Fatalf("detach %s: %v", name, err)
		}
	}
	got := evalTextDecoder(t, rt, `(() => {
  const decoder = new TextDecoder();
  const partial = new TextDecoder();
  partial.decode(new Uint8Array([0xe2]), {stream: true});
  const detachedDuringConversion = typeof ArrayBuffer.prototype.transfer !== "function"
    ? "unavailable"
    : (() => {
        const input = new Uint8Array([0x41]);
        return decoder.decode(input, {
          get stream() { input.buffer.transfer(); return false; }
        });
      })();
  const detachedStream = partial.decode(detachedArrayBuffer, {stream: true});
  const pendingScalar = partial.decode(new Uint8Array([0x82, 0xac]), {stream: true});
  return JSON.stringify([
    decoder.decode(detachedArrayBuffer),
    decoder.decode(detachedDataView),
    detachedDuringConversion,
    detachedStream,
    pendingScalar
  ]);
})()`)
	transferSupported := evalTextDecoder(t, rt, `typeof ArrayBuffer.prototype.transfer === "function"`) == "true"
	want := `["","","","","€"]`
	if !transferSupported {
		want = `["","","unavailable","","€"]`
	}
	if got != want {
		t.Fatalf("detached BufferSource output = %s, want %s", got, want)
	}
}
