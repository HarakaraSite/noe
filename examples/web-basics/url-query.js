const url = new URL("https://EXAMPLE.test:00443/a/../books/☃?title=a b~");
const params = url.searchParams;
url.search += "&broken=%E0%A4%A";
if (params !== url.searchParams || params.get("broken") !== "�%A") {
	throw new Error("Expected the same parameter object with replacement decoding");
}
params.set("page", "2");
const request = new Request(url, { method: "POST", body: params });
if (
	url.pathname !== "/books/%E2%98%83" ||
	url.port !== "" ||
	(await request.text()) !== "title=a+b%7E&broken=%EF%BF%BD%25A&page=2"
) {
	throw new Error("URL normalization or form encoding failed");
}
console.log("URL normalization and query encoding passed");
