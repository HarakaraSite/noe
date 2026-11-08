const controller = new AbortController();
controller.abort();
if (
	!(controller.signal.reason instanceof DOMException) ||
	controller.signal.reason.name !== "AbortError"
) {
	throw new Error("Expected the standard default abort reason");
}

const decoder = new TextDecoderStream("utf-8", { fatal: true });
let text = "";
for await (const chunk of ReadableStream.from(["\ud83d", "", "\ude00!"])
	.pipeThrough(new TextEncoderStream())
	.pipeThrough(decoder)) {
	text += chunk;
}
if (text !== "😀!" || !decoder.fatal || decoder.ignoreBOM) {
	throw new Error("Text stream state or metadata is incorrect");
}

const timeout = AbortSignal.timeout(0);
await new Promise((resolve) => {
	timeout.addEventListener("abort", resolve, { once: true });
});
if (
	!(timeout.reason instanceof DOMException) ||
	timeout.reason.name !== "TimeoutError"
) {
	throw new Error("Expected the standard timeout reason");
}
console.log("Abort interfaces and text streams passed");
