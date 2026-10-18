import {run_agent} from "noe:agent";

// Run from the repository root. These limits are example choices.
const instruction = process.argv.slice(2).join(" ") ||
  "Use run_js to calculate 12 + 30, then report the result briefly.";
const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "context-example",
  hooks: ["./examples/hooks/context.js"],
  contextBudget: {inputTokens: 20000},
  executionLimits: {providerCalls: 12, agentStarts: 4, queuedTasks: 4, hookCalls: 100},
  maxSteps: 6,
}, instruction);

if (result.lastResult?.state === "completed") console.log(result.lastResult.answer);
else console.log(JSON.stringify(result));
