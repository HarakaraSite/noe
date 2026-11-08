const baseURL = "https://example.test/api/";
const route = new URLPattern({ pathname: "books/:id", baseURL });
const matched = route.exec({ pathname: "books/42", baseURL });
if (matched?.pathname.groups.id !== "42") {
	throw new Error("Expected an object input resolved against baseURL");
}

const body = JSON.stringify({ book: matched.pathname.groups.id });
const bytes = new TextEncoder().encode(body);
const compressed = await new Response(
	ReadableStream.from([bytes]).pipeThrough(new CompressionStream("gzip")),
).bytes();
const restored = await new Response(
	ReadableStream.from([compressed]).pipeThrough(new DecompressionStream("gzip")),
).text();
if (restored !== body) throw new Error("Compression round-trip failed");
console.log("Compression and URLPattern inputs passed");
