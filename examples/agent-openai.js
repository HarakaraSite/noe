import { run_agent } from "noe:agent";

const model = process.argv[2] || "gpt-6-luna";
const task = process.argv.slice(3).join(" ") || "Use run_js to calculate 19 * 23, then return the result.";
const result = await run_agent({
  provider: "openai-chatgpt",
  model,
  effort: "auto",
  name: "openai-demo",
  maxTokens: 4096,
  maxSteps: 8,
}, task);
if (result.lastResult?.state !== "completed") throw new Error(result.lastResult?.error || "Agent task failed");
console.log(result.lastResult.answer);
