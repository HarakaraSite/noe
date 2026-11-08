const original = new Response("東京の雪");
const copy = original.clone();
// Start cancellation, then let the other branch finish the source.
const discarded = original.body.cancel("unused copy");
console.log(await copy.text());
await discarded;

let finishCleanup;
let reasons;
const source = new ReadableStream({
	cancel(value) {
		reasons = value;
		return new Promise(resolve => {
			finishCleanup = resolve;
		});
	},
}, { highWaterMark: 0 });
const [left, right] = source.tee();
const first = left.cancel("left");
const second = right.cancel("right");
finishCleanup();
await Promise.all([first, second]);
console.log("Tee cleanup completed", reasons.join(","));
