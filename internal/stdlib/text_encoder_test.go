package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestTextEncoderEncodeContract(t *testing.T) {
	for name, install := range map[string]func(*quickjs.Runtime) error{
		"WebAPIs": func(rt *quickjs.Runtime) error { return stdlib.WebAPIs(rt, nil) },
		"Install": func(rt *quickjs.Runtime) error { return stdlib.Install(rt, stdlib.Config{}) },
	} {
		t.Run(name, func(t *testing.T) {
			rt := quickjs.New()
			defer rt.Close()
			if err := install(rt); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`
const encoder = new TextEncoder();
function check(value, label) { if (!value) throw Error(label); }
function caught(call) { try { call(); } catch (error) { return error; } }
let calls = 0;
const input = {
  [Symbol.toPrimitive](hint) {
    check(hint === "string", "DOMString conversion hint");
    calls++;
    return "A\0é\ud800\ud800😀\udc00\udc00";
  },
};
check(caught(() => encoder.encode.call(Object.create(TextEncoder.prototype), input)) instanceof TypeError &&
  calls === 0, "receiver brand check before coercion");
check(encoder.encode().length === 0 && encoder.encode(undefined).length === 0 &&
  encoder.encode(null).join() === "110,117,108,108", "default and null input");
check(caught(() => encoder.encode(Symbol())) instanceof TypeError &&
  caught(() => encoder.encode(Object(Symbol()))) instanceof TypeError, "Symbol conversion");
const reason = {};
check(caught(() => encoder.encode({toString() { throw reason; }})) === reason,
  "conversion exception identity");

const IntrinsicUint8Array = Uint8Array;
const IntrinsicString = String;
const toWellFormed = String.prototype.toWellFormed;
const charCodeAt = String.prototype.charCodeAt;
const apply = Reflect.apply;
const fail = () => { throw Error("replaced intrinsic"); };
let first;
try {
  IntrinsicString.prototype.toWellFormed = IntrinsicString.prototype.charCodeAt = Reflect.apply = fail;
  globalThis.String = globalThis.Uint8Array = fail;
  first = encoder.encode(input);
} finally {
  Reflect.apply = apply;
  globalThis.String = IntrinsicString;
  globalThis.Uint8Array = IntrinsicUint8Array;
  String.prototype.toWellFormed = toWellFormed;
  String.prototype.charCodeAt = charCodeAt;
}
const expected = "65,0,195,169,239,191,189,239,191,189,240,159,152,128,239,191,189,239,191,189";
check(Object.getPrototypeOf(first) === IntrinsicUint8Array.prototype && first.join() === expected &&
  first.byteOffset === 0 && first.byteLength === first.buffer.byteLength && !first.buffer.resizable,
  "ordinary UTF-8 byte view with one replacement per lone surrogate");
const second = encoder.encode(input);
first[0] = 90;
const moved = structuredClone(first, {transfer: [first.buffer]});
check(calls === 2 && first.byteLength === 0 && moved[0] === 90 && second.join() === expected,
  "one conversion per call and independent transferable storage");
`); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestTextEncoderEncodeWorkerTransfer(t *testing.T) {
	worker := `
const bytes = new TextEncoder().encode("あ\ud800😀");
postMessage(bytes, [bytes.buffer]);
postMessage(bytes.byteLength);
close();`
	out, stderr := run(t, stdlib.Config{Workers: workerFiles(map[string]string{"./encode.js": worker})}, `
const worker = new Worker("./encode.js");
worker.onmessage = ({data}) => console.log(data instanceof Uint8Array ? data.join() : data);
`)
	if out != "227,129,130,239,191,189,240,159,152,128\n0" || stderr != "" {
		t.Fatalf("output = %q, stderr = %q", out, stderr)
	}
}
