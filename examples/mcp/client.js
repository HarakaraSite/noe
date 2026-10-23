// Connect an official MCP client and close its transport after the operation.
import { Client, StreamableHTTPClientTransport } from "mcp/sdk";

export async function withMCP(url, operation, headers = {}) {
  const client = new Client({ name: "noe-mcp-example", version: "1" });
  const transport = new StreamableHTTPClientTransport(new URL(url), {
    requestInit: { headers },
  });
  try {
    await client.connect(transport);
    return await operation(client);
  } finally {
    await client.close();
  }
}
