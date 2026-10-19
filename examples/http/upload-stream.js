import { fetch, serve } from "noe:http";
import { writeFile } from "node:fs/promises";

const args = process.argv.slice(2);
if (args.length > 2) {
  console.error("Usage: noe run examples/http/upload-stream.js [URL [output]]");
  process.exit(2);
}

// The first view has a non-zero byteOffset; only its selected bytes are sent.
const storage = new Uint8Array([99, 11, 12, 88]);
const chunks = [
  storage.subarray(1, 3),
  new Uint8Array(0),
  new Uint8Array([0, 255, 42]),
];
const expected = [11, 12, 0, 255, 42];
let nextChunk = 0;
const body = new ReadableStream({
  pull(controller) {
    if (nextChunk === chunks.length) {
      controller.close();
      return;
    }
    controller.enqueue(chunks[nextChunk++]);
  },
});

// With no URL, use a local echo endpoint. A URL argument lets an external
// fixture inspect the upload and return a response body for the optional file.
let server;
let url = args[0];
if (url === undefined) {
  server = serve({ port: 0 }, async request => {
    if (new URL(request.url).pathname !== "/upload") {
      return new Response("Not Found", { status: 404 });
    }
    const bytes = new Uint8Array(await request.arrayBuffer());
    return new Response(bytes, {
      headers: { "content-type": "application/octet-stream" },
    });
  });
  url = new URL("/upload", server.url).href;
}

try {
  const response = await fetch(url, {
    method: "POST",
    body,
  });

  const received = await response.bytes();
  const output = args[1];
  if (output !== undefined) await writeFile(output, received);

  if (!response.ok) throw new Error("HTTP " + response.status);
  if (args[0] === undefined) {
    const actual = Array.from(received);
    if (actual.length !== expected.length || actual.some((byte, i) => byte !== expected[i])) {
      throw new Error("Unexpected upload bytes: " + actual.join(","));
    }
    console.log(actual.join(","));
  } else {
    console.log(JSON.stringify({ status: response.status, responseBytes: received.byteLength }));
  }
} finally {
  if (server !== undefined) server.close();
}
