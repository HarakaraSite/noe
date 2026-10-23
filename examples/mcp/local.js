// Exercise the same official HTTP client against a local echo fixture.
import { serve } from "noe:http";
import { withMCP } from "./client.js";

const requests = [];
let initialized = false;
const server = serve({ hostname: "127.0.0.1", port: 0 }, async request => {
  // This small fixture has no standalone server-to-client notification stream.
  if (request.method === "GET") return new Response(null, { status: 405 });
  if (request.method !== "POST") return new Response(null, { status: 405 });
  const message = await request.json();
  requests.push(message.method);
  if (message.id === undefined) {
    if (message.method === "notifications/initialized") initialized = true;
    return new Response(null, { status: 202 });
  }
  const reply = payload => new Response(JSON.stringify({ jsonrpc: "2.0", id: message.id, ...payload }), {
    headers: { "content-type": "application/json" },
  });
  if (message.method === "initialize") {
    return reply({ result: {
      protocolVersion: "2025-06-18",
      capabilities: { tools: {} },
      serverInfo: { name: "noe-local-echo", version: "1" },
    } });
  }
  if (!initialized) return reply({ error: { code: -32000, message: "Client is not initialized" } });
  if (message.method === "tools/list") {
    return reply({ result: { tools: [{
      name: "echo", description: "Return the supplied text",
      inputSchema: { type: "object", properties: { text: { type: "string" } }, required: ["text"] },
    }] } });
  }
  if (message.method === "tools/call" && message.params?.name === "echo") {
    if (typeof message.params.arguments?.text !== "string") {
      return reply({ error: { code: -32602, message: "text must be a string" } });
    }
    // Send an SSE response in separate byte chunks, as remote servers can do.
    const bytes = new TextEncoder().encode(`event: message\ndata: ${JSON.stringify({
      jsonrpc: "2.0", id: message.id,
      result: { content: [{ type: "text", text: message.params.arguments.text }] },
    })}\n\n`);
    return new Response(new ReadableStream({
      start(controller) {
        controller.enqueue(bytes.slice(0, 31));
        controller.enqueue(bytes.slice(31));
        controller.close();
      },
    }), { headers: { "content-type": "text/event-stream" } });
  }
  return reply({ error: { code: -32601, message: "Unknown method or tool" } });
});

try {
  const result = await withMCP(`${server.url}/mcp`, async client => {
    const tools = (await client.listTools()).tools.map(tool => tool.name);
    if (JSON.stringify(tools) !== '["echo"]') throw new Error("Unexpected tool list");
    const texts = ["hello", "東京の雪", "third"];
    const responses = await Promise.all(texts.map(text => client.callTool({ name: "echo", arguments: { text } })));
    const replies = responses.map(response => response.content[0].text);
    if (JSON.stringify(replies) !== JSON.stringify(texts)) throw new Error("Echo results differ");
    return { tools, replies, initialized, requests };
  });
  console.log(JSON.stringify(result, null, 2));
} finally {
  server.close();
}
