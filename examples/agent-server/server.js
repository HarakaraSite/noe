// ./noe run examples/agent-server/server.js [--port 8080] [--provider ...] [--model ...] [--effort ...]
import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { startAgentServer } from "./app.js";

const usage = "Usage: noe run [--events=PATH] examples/agent-server/server.js [--port 0..65535] [--provider ID] [--model ID] [--effort auto|none|minimal|low|medium|high|xhigh|max] [--events-path PATH]";
const options = { port: 8080, provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high", eventsPath: null };
const keys = { "--port": "port", "--provider": "provider", "--model": "model", "--effort": "effort", "--events-path": "eventsPath" };
const seen = new Set();
const args = process.argv.slice(2);
for (let i = 0; i < args.length; i++) {
  if (args[i] === "--help" && args.length === 1) { console.log(usage); process.exit(0); }
  const key = keys[args[i]];
  if (!key || seen.has(key) || i + 1 >= args.length || !args[i + 1].trim() || args[i + 1].startsWith("--")) {
    console.error(usage); process.exit(2);
  }
  seen.add(key);
  options[key] = args[++i];
}
if (!/^\d+$/.test(String(options.port)) || Number(options.port) > 65535 ||
    !["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"].includes(options.effort)) {
  console.error(usage); process.exit(2);
}
options.port = Number(options.port);
const html = await readFile(resolve(dirname(resolve(process.argv[1])), "index.html"), "utf8");
const server = await startAgentServer(options, html);
console.log(JSON.stringify({ url: server.url, agentId: server.agentId, provider: options.provider, model: options.model, effort: options.effort }));
