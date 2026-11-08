const response = Response.json({ message: "東京の雪" });
const reader = response.body.getReader();
if (response.bodyUsed) throw new Error("A reader lock does not consume the body");

try {
	await response.text();
	throw new Error("A locked body was read twice");
} catch (error) {
	if (!(error instanceof TypeError)) throw error;
} finally {
	reader.releaseLock();
}

const copy = response.clone();
const bytes = await response.bytes();
bytes.fill(0);
const value = await copy.json();
if (value.message !== "東京の雪" || !response.bodyUsed || !copy.bodyUsed) {
	throw new Error("Body consumption or clone isolation failed");
}
console.log(value.message);

const input = new Uint8Array([65, 66]);
const source = new Request("https://example.test/", { method: "POST", body: input });
input.fill(0);
const moved = new Request(source);
if (!source.bodyUsed || (await moved.text()) !== "AB") {
	throw new Error("Finite request body transfer failed");
}
console.log("Body consumption passed");
