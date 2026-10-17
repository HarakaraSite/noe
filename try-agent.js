import { run_agent } from "noe:agent";

// 引数でタスクを変更できます: ./noe run try-agent.js "README.mdを要約して"
const task = process.argv.slice(2).join(" ") ||
  "run_jsでJavaScriptを実行し、1から100までの整数の合計を計算してください。" +
  "実行結果を使い、日本語で簡潔に回答してください。";

console.log("タスク:", task);
console.log("実行中…");

const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "try-agent",
  system: "必要な処理を自律的に実行し、日本語で回答してください。",
  cacheKey: "noe-try-agent-v1",
}, task);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
