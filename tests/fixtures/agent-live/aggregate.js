import { createAgent } from "noe:agent";

const agent = createAgent({
  model: "deepseek/deepseek-v4.1-flash",
  name: "aggregate",
  maxTokens: 4096,
});
console.log(await agent.run(
  "Read numbers.json from the working directory with run_js. Calculate the array's count, total and average. Return a JSON object with numeric count, total and average, using the actual file contents.",
  { cacheKey: "noe-increment-13-aggregate", maxSteps: 8 },
));
