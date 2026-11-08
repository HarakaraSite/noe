import { serve } from "noe:http";

const binary = new Uint8Array([0, 255, 10, 13, 10, 128]);
const server = serve({ port: 0 }, async request => {
	const form = await request.formData();
	const file = form.get("upload");
	return Response.json({
		note: form.get("note"),
		bytes: file instanceof File ? Array.from(await file.bytes()) : null,
	});
});

try {
	const form = new FormData();
	form.append("note", "one\ntwo\rthree");
	form.append("upload", new File([binary], "sample.bin"));
	const response = await fetch(server.url, { method: "POST", body: form });
	const result = await response.json();
	if (
		result.note !== "one\r\ntwo\r\nthree" ||
		result.bytes.join() !== binary.join() ||
		form.get("note") !== "one\ntwo\rthree"
	) {
		throw new Error("Multipart text or file bytes changed unexpectedly");
	}

	const raw = "--fixture\r\nContent-Disposition: form-data; name=\"note\"\r\n\r\n"
		+ "before--fixtureafter\r\n--fixture--\r\n";
	const inline = await fetch(server.url, {
		method: "POST",
		headers: { "content-type": "multipart/form-data; boundary=fixture" },
		body: raw,
	});
	if ((await inline.json()).note !== "before--fixtureafter") {
		throw new Error("Inline boundary marker truncated form data");
	}
	console.log("Multipart fields and binary file passed");
} finally {
	await server.close();
}
