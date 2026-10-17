import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "repair",
  maxTokens: 4096,
  cacheKey: "noe-increment-13-repair",
  maxSteps: 8,
},
  "This is an autonomous error-recovery check. Your FIRST run_js call must contain ONLY: throw new Error('NOE_LIVE_REPAIR'); . Wait for that tool's failure result. Then repair the code in a NEW run_js call: actually read numbers.json from the working directory and calculate count, total and average. Return a JSON object with numeric count, total and average. Do not ask a human for help."
);
console.log(result.lastResult.answer);
