import { fetch, serve } from "noe:http";

// Exchange JSON over loopback, consume the response, then release the listener.
const server = serve({ port: 0 }, async request => {
  const { value } = await request.json();
  return Response.json({ value: value + 1 });
});
try {
  const response = await fetch(server.url, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ value: 41 }),
  });
  if (!response.ok) throw new Error("HTTP " + response.status);
  console.log(JSON.stringify(await response.json()));
} finally {
  server.close();
}
