// List Exa's official MCP tools, or explicitly request one web search.
import { readFile } from "node:fs/promises";
import { withMCP } from "./client.js";

const usage = "Usage: noe run --import-map examples/mcp/import-map.json examples/mcp/exa.js [list | search QUERY...]";
const [action = "list", ...words] = process.argv.slice(2);
if (action === "--help") {
  console.log(usage);
  process.exit(0);
}
if (!['list', 'search'].includes(action) || (action === 'list' && words.length)) {
  throw new Error(usage);
}
const query = words.join(" ").trim();
if (action === "search" && !query) throw new Error(`Search query is required. ${usage}`);

const headers = {};
const keyFile = process.env.EXA_API_KEY_FILE;
if (keyFile) {
  const key = (await readFile(keyFile, "utf8")).trim();
  if (!key) throw new Error("EXA_API_KEY_FILE is empty");
  headers["x-api-key"] = key;
}

const result = await withMCP("https://mcp.exa.ai/mcp", async client => {
  if (action === "list") return await client.listTools();
  return await client.callTool({
    name: "web_search_exa",
    arguments: { query, objective: query, numResults: 3 },
  });
}, headers);
console.log(JSON.stringify(result, null, 2));
if (result.isError) process.exitCode = 1;
export default result;
