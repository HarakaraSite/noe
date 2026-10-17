// A local MCP fixture; protocols and framing belong to JS, not the Go host.
import { stdin, stdout } from "noe:io";
const decoder = new TextDecoder(), encoder = new TextEncoder();
const writer = stdout.getWriter();
let pending = "";
async function message(line) {
  if (!line.trim()) return;
  const request = JSON.parse(line);
  if (request.id === undefined) return;
  let result;
  if (request.method === "initialize") {
    result = { protocolVersion: request.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: "noe-js-fixture", version: "1" } };
  } else if (request.method === "tools/list") {
    result = { tools: [{ name: "echo", description: "Echo text", inputSchema: { type: "object", properties: { text: { type: "string" } }, required: ["text"] } }] };
  } else if (request.method === "tools/call" && request.params.name === "echo") {
    result = { content: [{ type: "text", text: request.params.arguments.text }] };
  } else {
    await writer.write(encoder.encode(JSON.stringify({ jsonrpc: "2.0", id: request.id, error: { code: -32601, message: "Unknown method" } }) + "\n"));
    return;
  }
  await writer.write(encoder.encode(JSON.stringify({ jsonrpc: "2.0", id: request.id, result }) + "\n"));
}
for await (const bytes of stdin) {
  pending += decoder.decode(bytes, { stream: true });
  let at;
  while ((at = pending.indexOf("\n")) >= 0) {
    const line = pending.slice(0, at); pending = pending.slice(at + 1);
    await message(line);
  }
}
pending += decoder.decode();
if (pending.trim()) throw new Error("Incomplete MCP message at EOF");
writer.releaseLock();
