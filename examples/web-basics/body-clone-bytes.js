const response = new Response("東京の雪");
const copy = response.clone();
const reader = response.body.getReader();
const chunk = await reader.read();
chunk.value.fill(0);
reader.releaseLock();
if ((await copy.text()) !== "東京の雪") {
	throw new Error("Editing one body changed its clone");
}

const request = new Request("https://example.test/", {
	method: "POST",
	body: new Uint8Array([65, 66]),
});
const retry = request.clone();
const uploadReader = request.body.getReader();
const upload = await uploadReader.read();
structuredClone(upload.value.buffer, { transfer: [upload.value.buffer] });
uploadReader.releaseLock();
if (upload.value.byteLength !== 0 || (await retry.text()) !== "AB") {
	throw new Error("Transferring one body buffer changed its clone");
}
console.log("Body clone bytes isolated");
