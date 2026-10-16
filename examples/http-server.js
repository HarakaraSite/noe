// ./noe run examples/http-server.js [port]
const args = process.argv.slice(2);
if (args.length > 1 || (args.length === 1 && !/^\d+$/.test(args[0]))) {
  console.error("Usage: noe run examples/http-server.js [port: 0..65535]");
  process.exit(2);
}
const port = args.length === 0 ? 8080 : Number(args[0]);
if (!Number.isInteger(port) || port > 65535) {
  console.error("Usage: noe run examples/http-server.js [port: 0..65535]");
  process.exit(2);
}

const server = serve({ port }, async request => {
  const pathname = new URL(request.url).pathname;
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
