// A local echo MCP fixture. Protocol handling is JavaScript-only; the host
// process supplies streams, not an MCP implementation.
import { stdin, stdout } from "noe:io";

const mode = process.argv[2] || "normal";
const decoder = new TextDecoder();
const encoder = new TextEncoder();
const writer = stdout.getWriter();
let pending = "";
let initialized = false;
let stopOnEOF = false;
let waitingCalls = [];

async function write(message) {
  await writer.write(encoder.encode(JSON.stringify(message) + "\n"));
}

if (mode === "block") {
  await write({ jsonrpc: "2.0", method: "notifications/ready", params: {} });
  await new Promise(resolve => { setTimeout(resolve, 60000); });
}

async function handle(request) {
  if (request.id === undefined) {
    if (request.method === "notifications/initialized") initialized = true;
    return;
  }
  if (request.method === "initialize") {
    await write({ jsonrpc: "2.0", id: request.id, result: {
      protocolVersion: request.params.protocolVersion,
      capabilities: { tools: {} },
      serverInfo: { name: "noe-js-fixture", version: "1" },
    } });
    return;
  }
  if (request.method === "tools/list") {
    if (!initialized) {
      await write({ jsonrpc: "2.0", id: request.id, error: { code: -32000, message: "initialized notification required" } });
      return;
    }
    if (mode === "eof") {
      await writer.close();
      stopOnEOF = true;
      return;
    }
    if (mode === "malformed") {
      await writer.write(encoder.encode("not-json\n"));
      return;
    }
    await write({ jsonrpc: "2.0", id: request.id, result: {
      tools: [{ name: "echo", description: "Echo text", inputSchema: {
        type: "object", properties: { text: { type: "string" } }, required: ["text"],
      } }],
    } });
    return;
  }
  if (request.method === "tools/call" && request.params.name === "echo") {
    waitingCalls.push(request);
    if (waitingCalls.length === 3) {
      // Deliver responses in reverse order so the client must correlate IDs.
      for (const call of waitingCalls.reverse()) {
        await write({ jsonrpc: "2.0", id: call.id, result: { content: [{ type: "text", text: call.params.arguments.text }] } });
      }
      waitingCalls = [];
    }
    return;
  }
  await write({ jsonrpc: "2.0", id: request.id, error: { code: -32601, message: "Unknown method" } });
}

for await (const bytes of stdin) {
  pending += decoder.decode(bytes, { stream: true });
  let newline;
  while ((newline = pending.indexOf("\n")) >= 0) {
    const line = pending.slice(0, newline);
    pending = pending.slice(newline + 1);
    if (line.trim()) await handle(JSON.parse(line));
    if (stopOnEOF) break;
  }
  if (stopOnEOF) break;
}
pending += decoder.decode();
if (pending.trim()) throw new Error("incomplete MCP request at EOF");
writer.releaseLock();
