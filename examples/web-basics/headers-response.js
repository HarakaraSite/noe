const headers = new Headers(new Map([["X-Trace", " request-1 "]]));
headers.append("Set-Cookie", "session=one; Path=/; HttpOnly");
headers.append("Set-Cookie", "theme=dark; Path=/");

const response = Response.json({ message: "東京の雪" }, { status: 201, headers });
const copy = response.clone();
if (
	response.type !== "default" ||
	response.headers.get("x-trace") !== "request-1" ||
	[...response.headers].filter(([name]) => name === "set-cookie").length !== 2 ||
	(await copy.json()).message !== "東京の雪"
) {
	throw new Error("Headers and Response contract failed");
}

const redirect = Response.redirect("https://EXAMPLE.test:00443/a/../☃", 303);
if (redirect.headers.get("location") !== "https://example.test/%E2%98%83") {
	throw new Error("Redirect URL normalization failed");
}
console.log(response.status, response.headers.getSetCookie());
console.log(redirect.status, redirect.headers.get("location"));
console.log(await response.json());
