import { dirname, resolve } from "node:path";
import { startConsole } from "./server.js";

const usage = "Usage: noe run examples/agent-console/main.js [--provider ID] [--model ID] [--effort LEVEL] [--port 0..65535] [--data-dir PATH]";
const config = {
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "worker",
  system: "必要な処理を自律的に実行し、日本語で回答してください。",
};
const directory = dirname(resolve(process.argv[1]));
let dataDir = resolve(directory, "data/sessions");
let port = 8787;
const args = process.argv.slice(2);
for (let index = 0; index < args.length; index++) {
  const flag = args[index];
  if (flag === "--help") { console.log(usage); process.exit(0); }
  if (!["--provider", "--model", "--effort", "--port", "--data-dir"].includes(flag)) throw new Error(usage);
  const value = args[++index];
  if (!value || value.startsWith("--")) throw new Error(flag + " requires a value. " + usage);
  if (flag === "--port") {
    if (!/^\d+$/.test(value) || Number(value) > 65535) throw new Error(usage);
    port = Number(value);
  } else if (flag === "--data-dir") {
    dataDir = resolve(value);
  } else {
    config[flag.slice(2)] = value;
  }
}
const { server } = await startConsole({ config, dataDir, publicDir: resolve(directory, "public"), port });
console.log("NOE Agent Console: " + server.url);
console.log("History: " + dataDir);
