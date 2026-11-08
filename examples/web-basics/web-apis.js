const encoder = new TextEncoder();
const tooSmall = new Uint8Array(1);
const partial = encoder.encodeInto("あ", tooSmall);
if (partial.read !== 0 || partial.written !== 0) {
	throw new Error("The destination cannot fit a complete UTF-8 character");
}
const destination = new Uint8Array(3);
const encoded = encoder.encodeInto("あ", destination);
if (encoded.read !== 1 || encoded.written !== 3) {
	throw new Error("Unexpected UTF-16/UTF-8 counts");
}

const target = new EventTarget();
const decoder = new TextDecoder();
decoder.decode(new Uint8Array([239]), { stream: true });
if (decoder.decode(new Uint8Array([187, 191, 226, 130, 172])) !== "€") {
	throw new Error("Streaming UTF-8 or BOM handling failed");
}
const controller = new AbortController();
let calls = 0;
const callback = () => calls++;
target.addEventListener("temporary", callback, { signal: controller.signal });
target.addEventListener("temporary", callback);
target.dispatchEvent(new Event("temporary"));
controller.abort();
target.dispatchEvent(new Event("temporary"));
if (calls !== 1) throw new Error("Listener identity or abort removal failed");
let received;
target.addEventListener("encoded", (event) => {
	received = event.detail;
});
performance.mark("start");
target.dispatchEvent(new CustomEvent("encoded", { detail: destination }));
performance.mark("end");
const measurement = performance.measure("dispatch", "start", "end");
if (received !== destination || measurement.duration < 0) {
	throw new Error("Event dispatch or User Timing failed");
}
if (performance.getEntriesByName("dispatch", "measure")[0] !== measurement) {
	throw new Error("The measurement was not recorded");
}
performance.clearMarks();
performance.clearMeasures();
console.log("Encoding, event listeners, User Timing and CustomEvent passed");
