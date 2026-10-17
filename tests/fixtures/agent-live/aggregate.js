import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "aggregate",
  maxTokens: 4096,
  cacheKey: "noe-increment-13-aggregate",
  maxSteps: 8,
},
  "Read numbers.json from the working directory with run_js. Calculate the array's count, total and average. Return a JSON object with numeric count, total and average, using the actual file contents."
);
console.log(result.lastResult.answer);
