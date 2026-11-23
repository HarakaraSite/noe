package stdlib_test

import (
	"fmt"
	"runtime"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

var binaryFormCases = []struct{ name, code string }{
	{"conversion", `
for (const parts of ["abc", null, 1, {}]) throws("TypeError", () => new Blob(parts));
check(new Blob().size === 0 && new Blob(undefined).size === 0, "empty Blob");
throws("TypeError", () => new Blob([Symbol()]));
throws("TypeError", () => new Blob([], 1));
throws("TypeError", () => new Blob([], {type: Symbol()}));
throws("TypeError", () => new Blob([], {endings: "invalid"}));
check(new Blob([], {type: "TEXT/PLAIN"}).type === "text/plain", "ASCII MIME type");
check(new Blob([], {type: "text/\u0000plain"}).type === "" && new Blob([], {type: "text/é"}).type === "", "invalid MIME type");
check(await new Blob(["\ud800"]).text() === "\ufffd", "USVString parts");
check(await new Blob(["a\r\nb\rc\n"],{endings:"native"}).text() === ["a","b","c",""].join(nativeBinaryEOL), "native text endings");
check(await new Blob(["a\r\nb\rc\n"]).text() === "a\r\nb\rc\n", "transparent text endings");
const backing = new Uint8Array([0,65,66,0]);
check(await new Blob([new DataView(backing.buffer,1,2)]).text() === "AB", "DataView byte offset and length");
const detached = new ArrayBuffer(4);
const detachedParts = [detached, new Uint8Array(detached, 1, 2), new DataView(detached, 1, 2)];
detached.transfer();
for (const part of detachedParts) {
  check(new Blob([part]).size === 0, "detached Blob part is empty");
  check(await new File(["a", part, "b"], "test").text() === "ab", "detached File part is empty");
}
const detachedInOptions = new Uint8Array([65]);
check(await new Blob([detachedInOptions, "b"], {get type() {detachedInOptions.buffer.transfer(); return "text/plain";}}).text() === "b",
  "Blob samples bytes after options conversion");
const detachedInName = new ArrayBuffer(2);
check(new File([new DataView(detachedInName)], {toString() {detachedInName.transfer(); return "test";}}).size === 0,
  "File samples bytes after filename conversion");
const resizable = new ArrayBuffer(4, {maxByteLength: 8});
const outOfBounds = new DataView(resizable, 2, 2);
resizable.resize(1);
for (const part of [resizable, new Uint8Array(resizable), outOfBounds, new Uint8Array(new SharedArrayBuffer(2))]) {
  throws("TypeError", () => new Blob([part]));
  throws("TypeError", () => new File([part], "test"));
}
throws("TypeError", () => new File()); throws("TypeError", () => new File([]));
const file = new File(["value"], "\ud800.txt", {type: "TEXT/PLAIN", lastModified: 42});
check(file.name === "\ufffd.txt" && file.lastModified === 42 && file.type === "text/plain", "File metadata");
const order = [];
new File([], {toString() {order.push("name"); return "x";}}, {
  get endings() {order.push("endings"); return "transparent";},
  get type() {order.push("type"); return "text/plain";},
  get lastModified() {order.push("lastModified"); return 42;},
});
check(order.join() === "name,endings,type,lastModified", "File conversion order");
`},
	{"ownership", `
const input = new Uint8Array([0,65,66,67,0]);
const blob = new Blob([input.subarray(1,4)]), copy = new Blob([blob]), sliced = blob.slice(1,3);
input.fill(90);
check(await blob.text() === "ABC" && await copy.text() === "ABC" && await sliced.text() === "BC", "input and derived snapshots");
blob._bytes = new Uint8Array([88]); blob._type = "changed";
check(await blob.text() === "ABC" && blob.size === 3 && blob.type === "", "private blob state");
const bytes = await blob.bytes(); bytes.fill(88);
const buffer = await blob.arrayBuffer(); new Uint8Array(buffer).fill(88);
const chunks = []; for await (const chunk of blob.stream()) {chunks.push(chunk); chunk.fill(88);}
check(await blob.text() === "ABC" && await copy.text() === "ABC", "read results cannot mutate storage");
const reason = {}; const reader = blob.stream().getReader(); await reader.cancel(reason); reader.releaseLock();
check(await blob.text() === "ABC", "stream cancellation preserves Blob");
const originalEncode = TextEncoder.prototype.encode, originalDecode = TextDecoder.prototype.decode;
try {
  TextEncoder.prototype.encode = () => {throw Error("author encoder");};
  TextDecoder.prototype.decode = () => {throw Error("author decoder");};
  check(await blob.text() === "ABC" && await new Blob(["snapshot"]).text() === "snapshot", "Blob codecs do not expose private storage to prototype hooks");
} finally {TextEncoder.prototype.encode = originalEncode; TextDecoder.prototype.decode = originalDecode;}
for (const method of ["text", "bytes", "arrayBuffer"]) {
  let error; try {await Blob.prototype[method].call({_bytes: new Uint8Array([65])});} catch (value) {error = value;}
  check(error instanceof TypeError, "Blob method receiver " + method);
}
throws("TypeError", () => Blob.prototype.slice.call({}));
throws("TypeError", () => Blob.prototype.stream.call({}));
const cloned = structuredClone(new File([blob], "original.txt", {lastModified:42, type:"text/plain"}));
check(cloned instanceof File && cloned.name === "original.txt" && cloned.lastModified === 42 && await cloned.text() === "ABC", "File clone state");
`},
	{"form", `
const form = new FormData();
for (const method of ["append", "set"]) {
  throws("TypeError", () => form[method]("x"));
  throws("TypeError", () => form[method]("x", "text", "file.txt"));
  throws("TypeError", () => form[method]("x", "text", undefined));
}
for (const method of ["get", "getAll", "has", "delete"]) throws("TypeError", () => form[method]());
throws("TypeError", () => form.forEach(null));
throws("TypeError", () => FormData.prototype.entries.call({}));
form.append("\ud800", "\udc00");
check(form.get("\ufffd") === "\ufffd", "FormData USVString conversion");
const file = new File(["file"], "source.txt", {lastModified:42, type:"text/plain"});
form.append("identity", file);
form.append("renamed", file, "renamed.txt");
check(form.get("identity") === file, "File identity without rename");
check(form.get("renamed").name === "renamed.txt" && form.get("renamed").lastModified === 42 && await form.get("renamed").text() === "file", "renamed File snapshot");
form._entries = [["injected", "bad"]];
check(!form.has("injected"), "private FormData entries");
`},
	{"iteration", `
for (const mode of ["delete", "set"]) {
  const form = new FormData();
  form.append("a","1"); form.append("b","2"); form.append("b","3"); form.append("c","4");
  const iterator = form.entries(); iterator.next();
  if (mode === "delete") form.delete("b"); else form.set("b","new");
  form.append("d","5");
  const actual = Array.from(iterator).map(([name,value]) => name+":"+value).join();
  check(actual === (mode === "delete" ? "c:4,d:5" : "b:new,c:4,d:5"), "live iterator after " + mode);
}
const form = new FormData(); form.append("a","1"); form.append("b","2");
const seen = [];
form.forEach((value,name,target) => {seen.push(name); check(target === form,"forEach receiver"); if(name === "a") {form.delete("b"); form.append("c","3");}});
check(seen.join() === "a,c", "live forEach");
`},
	{"bodies", `
const blob = new Blob(["original"], {type:"text/plain"});
blob._bytes = new Uint8Array([88]);
Object.defineProperty(blob,"type",{value:"changed"});
for (const response of [new Response(blob), new Request("https://example.test/",{method:"POST",body:blob})]) {
  check(response.headers.get("content-type") === "text/plain" && await response.text() === "original", "body extraction uses private Blob state");
}
const form = new FormData();
const file = new File([blob],"source.txt",{type:"text/plain",lastModified:42});
Object.defineProperty(file,"name",{value:"changed"});
form.append("file",file,"copied.txt"); form.append("text","a\nb");
form[Symbol.iterator] = () => {throw Error("author iterator must not serialize FormData");};
const response = new Response(form), parsed = await response.formData();
check(parsed.get("file").name === "copied.txt" && await parsed.get("file").text() === "original", "multipart File snapshot");
check(parsed.get("text") === "a\r\nb", "multipart normalization is preserved");
check(await blob.text() === "original", "body extraction cannot mutate Blob");
`},
}

func TestBlobFileFormData(t *testing.T) {
	for _, fixture := range binaryFormCases {
		t.Run(fixture.name, func(t *testing.T) { executeWebBasics(t, stdlib.Profile{}, binaryFormPrelude()+fixture.code) })
	}
}

func binaryFormPrelude() string {
	eol := "\n"
	if runtime.GOOS == "windows" {
		eol = "\r\n"
	}
	return fmt.Sprintf("const nativeBinaryEOL = %q;\n", eol)
}

func TestBlobFileFormDataWorker(t *testing.T) {
	for _, fixture := range binaryFormCases {
		t.Run(fixture.name, func(t *testing.T) {
			worker := webBasicsPrelude + binaryFormPrelude() + "import {parentPort} from 'node:worker_threads';\n" + fixture.code + "\nparentPort.postMessage('passed'); parentPort.close();"
			executeWebBasics(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker = new Worker(%q, {eval:true,type:"module"});
const result = await new Promise((resolve,reject) => {worker.once("message",resolve); worker.once("error",reject);});
await worker.terminate(); check(result === "passed", "Worker binary/form contracts");
`, worker))
		})
	}
}
