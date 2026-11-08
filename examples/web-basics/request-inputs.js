const request = new Request("https://EXAMPLE.test:00443/a/../☃?q=a b", {
	method: "patch",
	body: "payload",
});
const copy = request.clone();
if (
	request.url !== "https://example.test/%E2%98%83?q=a%20b" ||
	request.method !== "patch" ||
	Reflect.set(request, "method", "POST") ||
	(await copy.text()) !== "payload"
) {
	throw new Error("Request input contract failed");
}
console.log("Request URL and method inputs passed");
