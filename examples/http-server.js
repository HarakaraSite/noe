// ./noe run examples/http-server.js [port]
if (noe.args.length > 1 || (noe.args.length === 1 && !/^\d+$/.test(noe.args[0]))) {
  console.error("Usage: noe run examples/http-server.js [port: 0..65535]");
  noe.exit(2);
}
const port = noe.args.length === 0 ? 8080 : Number(noe.args[0]);
if (!Number.isInteger(port) || port > 65535) {
  console.error("Usage: noe run examples/http-server.js [port: 0..65535]");
  noe.exit(2);
}

const server = noe.serve({ port }, async request => {
  if (request.method === "GET" && request.path === "/health") {
    return { headers: { "Content-Type": "text/plain; charset=utf-8" }, body: "ok\n" };
  }
  if (request.method === "POST" && request.path === "/echo") {
    let value;
    try {
      value = await request.json();
    } catch {
      return { status: 400, body: "Invalid JSON\n" };
    }
    return {
      headers: { "Content-Type": "application/json; charset=utf-8" },
      body: JSON.stringify(value),
    };
  }
  return { status: 404, body: "Not Found\n" };
});
console.log(server.url);
