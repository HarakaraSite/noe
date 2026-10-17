// Bundle this entry with the pinned official SDK; transport stays ordinary JS.
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { spawn } from "noe:process";
import { resolve } from "node:path";

class NoeTransport {
  constructor(server) { this.server = server; this.closing = false; }
  async start() {
    this.child = await spawn(resolve(process.argv[0]), ["run", this.server], { stderr: "inherit" });
    this.writer = this.child.stdin.getWriter();
    this.reading = this.readMessages();
    this.reading.catch(error => { if (!this.closing) this.onerror?.(error); });
  }
  async readMessages() {
    const decoder = new TextDecoder();
    let pending = "";
    for await (const bytes of this.child.stdout) {
      pending += decoder.decode(bytes, { stream: true });
      let at;
      while ((at = pending.indexOf("\n")) >= 0) {
        const line = pending.slice(0, at); pending = pending.slice(at + 1);
        if (line.trim()) this.onmessage?.(JSON.parse(line));
      }
    }
    pending += decoder.decode();
    if (pending.trim()) throw new Error("Incomplete MCP message at EOF");
    if (!this.closing) this.onclose?.();
  }
  async send(message) {
    await this.writer.write(new TextEncoder().encode(JSON.stringify(message) + "\n"));
  }
  async close() {
    if (this.closing) return;
    this.closing = true;
    try {
      // The fixture exits on EOF. This is the fixture's policy, rather than a
      // shutdown protocol embedded in NOE's generic process implementation.
      await this.writer.close();
      await this.child.exited;
      await this.reading;
    } finally {
      await this.child.close();
      this.onclose?.();
    }
  }
}

const server = process.argv[2];
if (!server) throw new Error("Pass the absolute fixture server.js path");
const client = new Client({ name: "noe-process-check", version: "1" });
const transport = new NoeTransport(server);
try {
  await client.connect(transport);
  const tools = await client.listTools();
  if (tools.tools[0]?.name !== "echo") throw new Error("Tool discovery failed");
  const values = ["hello", "雪".repeat(90000), "third"];
  const replies = await Promise.all(values.map(text => client.callTool({ name: "echo", arguments: { text } })));
  for (let i = 0; i < values.length; i++) {
    if (replies[i].content[0]?.text !== values[i]) throw new Error("Response mismatch " + i);
  }
  console.log(JSON.stringify({ sdk: "1.32.1", initialized: true, tools: tools.tools.length, concurrentCalls: replies.length, largeInputBytes: new TextEncoder().encode(values[1]).length }));
} finally {
  await client.close();
}
