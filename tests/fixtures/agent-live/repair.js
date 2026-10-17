import { createAgent } from "noe:agent";

const agent = createAgent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "repair",
  maxTokens: 4096,
});
console.log(await agent.run(
  "This is an autonomous error-recovery check. Your FIRST run_js call must contain ONLY: throw new Error('NOE_LIVE_REPAIR'); . Wait for that tool's failure result. Then repair the code in a NEW run_js call: actually read numbers.json from the working directory and calculate count, total and average. Return a JSON object with numeric count, total and average. Do not ask a human for help.",
  { cacheKey: "noe-increment-13-repair", maxSteps: 8 },
));
