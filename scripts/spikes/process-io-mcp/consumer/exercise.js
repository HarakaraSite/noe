import { Client } from "mcp/client";
import { NoeTransport } from "mcp/transport";

export async function exercise(config) {
  const client = new Client({ name: "noe-process-check", version: "1" });
  const transport = new NoeTransport(config);
  try {
    await client.connect(transport);
    const listed = await client.listTools();
    if (listed.tools.length !== 1 || listed.tools[0]?.name !== "echo") {
      throw new Error("tool discovery failed");
    }
    const values = ["hello", "雪".repeat(90000), "third"];
    const replies = await Promise.all(values.map(text => client.callTool({ name: "echo", arguments: { text } })));
    for (let i = 0; i < values.length; i++) {
      if (replies[i].content[0]?.text !== values[i]) {
        throw new Error(`response mismatch ${i}`);
      }
    }
    return {
      sdk: "1.32.1",
      initialized: true,
      tools: listed.tools.length,
      concurrentCalls: replies.length,
      largeInputBytes: new TextEncoder().encode(values[1]).length,
    };
  } finally {
    await client.close();
    if (!transport.closed || transport.child !== undefined || transport.exitStatus?.code !== 0 || transport.exitStatus?.signal) {
      throw new Error(`child cleanup failed: closed=${transport.closed} child=${!!transport.child} exit=${JSON.stringify(transport.exitStatus)}`);
    }
  }
}
