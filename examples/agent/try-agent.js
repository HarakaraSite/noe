import { run_agent } from "noe:agent";
import { readFile } from "node:fs/promises";

// 指示を直接渡すか、--task-fileでUTF-8ファイルから読み込めます。
const usage = "Usage: noe run examples/agent/try-agent.js [--provider ID] [--model ID] [--effort LEVEL] [--task-file PATH | instruction ...]";
const config = {
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "max",
  name: "try-agent",
  system: "必要な処理を自律的に実行し、日本語で回答してください。",
  cacheKey: "noe-try-agent-v1",
};
const args = process.argv.slice(2);
const words = [];
let taskFile;
for (let index = 0; index < args.length; index++) {
  const arg = args[index];
  if (arg === "--help") {
    console.log(usage);
    process.exit(0);
  }
  if (["--provider", "--model", "--effort", "--task-file"].includes(arg)) {
    const value = args[++index];
    if (!value || value.startsWith("--")) throw new Error(`${arg} requires a value. ${usage}`);
    if (arg === "--task-file") taskFile = value;
    else config[arg.slice(2)] = value;
  } else if (arg === "--") {
    words.push(...args.slice(index + 1));
    break;
  } else if (arg.startsWith("--")) {
    throw new Error(`Unknown option: ${arg}. ${usage}`);
  } else {
    words.push(arg);
  }
}
if (taskFile && words.length) throw new Error(`Choose --task-file or instruction text. ${usage}`);
const task = taskFile ? await readFile(taskFile, "utf8") : words.join(" ") ||
  "run_jsでJavaScriptを実行し、1から100までの整数の合計を計算してください。" +
  "実行結果を使い、日本語で簡潔に回答してください。";
if (!task.trim()) throw new Error("The task must not be empty.");

console.log("タスク:", task);
console.log("実行中…");

const result = await run_agent(config, task);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
