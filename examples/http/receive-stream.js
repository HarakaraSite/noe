import { fetch, serve } from "noe:http";

const chunks = [
  new Uint8Array([0, 255, 17]),
  new Uint8Array([34, 128, 0, 255]),
];
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

const server = serve({ port: 0 }, async request => {
  if (new URL(request.url).pathname !== "/receive" || request.method !== "POST") {
    return new Response("Not Found", { status: 404 });
  }

  if (request.body === null) throw new Error("Expected a request body stream");

  const reader = request.body.getReader();
  const bytes = [];
  let sum = 0;
  try {
    for (;;) {
      const { value, done } = await reader.read();
      if (done) break;
      for (const byte of value) {
        bytes.push(byte);
        sum += byte;
      }
    }
  } finally {
    reader.releaseLock();
  }

  return Response.json({ bytes, byteCount: bytes.length, sum });
});

try {
  const response = await fetch(new URL("/receive", server.url).href, {
    method: "POST",
    body,
  });
  if (!response.ok) throw new Error("HTTP " + response.status);

  const result = await response.json();
  const expected = { bytes: [0, 255, 17, 34, 128, 0, 255], byteCount: 7, sum: 689 };
  if (JSON.stringify(result) !== JSON.stringify(expected)) {
    throw new Error("Unexpected received body: " + JSON.stringify(result));
  }
  console.log(JSON.stringify(result));
} finally {
  server.close();
}
