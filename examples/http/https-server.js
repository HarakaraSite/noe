import { readFile } from "noe:fs";
import { serve, upgradeWebSocket } from "noe:http";

// ./noe run examples/http/https-server.js <certificate.pem> <private-key.pem> [port]
const args = process.argv.slice(2);
if (
  args.length < 2 ||
  args.length > 3 ||
  args[0].length === 0 ||
  args[1].length === 0 ||
  (args.length === 3 && !/^\d+$/.test(args[2]))
) {
  console.error("Usage: noe run examples/http/https-server.js <certificate.pem> <private-key.pem> [port: 0..65535]");
  process.exit(2);
}

const port = args.length === 3 ? Number(args[2]) : 8443;
if (!Number.isSafeInteger(port) || port > 65535) {
  console.error("Usage: noe run examples/http/https-server.js <certificate.pem> <private-key.pem> [port: 0..65535]");
  process.exit(2);
}

const [certificatePath, privateKeyPath] = args;
const [cert, key] = await Promise.all([
  readFile(certificatePath, "utf8"),
  readFile(privateKeyPath, "utf8"),
]);

const server = serve({ hostname: "127.0.0.1", port, cert, key }, async request => {
  const pathname = new URL(request.url).pathname;
  if (request.method === "GET" && pathname === "/ws") {
    const { socket, response } = upgradeWebSocket(request);
    socket.addEventListener("message", event => socket.send(event.data));
    return response;
  }
  if (request.method === "GET" && pathname === "/health") {
    return new Response("ok\n", { headers: { "Content-Type": "text/plain; charset=utf-8" } });
  }
  if (request.method === "POST" && pathname === "/echo") {
    let value;
    try {
      value = await request.json();
    } catch {
      return new Response("Invalid JSON\n", { status: 400 });
    }
    return Response.json(value);
  }
  return new Response("Not Found\n", { status: 404 });
});

console.log(server.url);
