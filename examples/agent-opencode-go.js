import { run_agent } from "noe:agent";

const model = process.argv[2] || "deepseek-v4.1-flash";
const instruction = process.argv.slice(3).join(" ") ||
  "Use run_js to calculate 19 * 23. Return only the numeric result.";

const snapshot = await run_agent({
  provider: "opencode-go",
  model,
  effort: "auto",
  name: "opencode-go",
  maxSteps: 8,
  maxTokens: 4096,
}, instruction);

if (snapshot.lastResult?.state !== "completed") {
  throw new Error(snapshot.lastResult?.error || "agent task did not complete");
}
console.log(snapshot.lastResult.answer);
