import { run_agent } from "noe:agent";

const file = process.argv[2] ?? "README.md";
const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
  system: "日本語で簡潔に回答してください。",
  cacheKey: "noe-summary-v1",
  maxSteps: 6,
}, `作業ディレクトリの ${JSON.stringify(file)} を実際に読み、要点を3点にまとめてください。`);

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error ?? `agent ended with ${result.reason}`);
}
console.log(result.lastResult.answer);
