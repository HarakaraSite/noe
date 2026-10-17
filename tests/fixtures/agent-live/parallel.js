import { createAgent } from "noe:agent";

const parent = createAgent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "parent",
  maxTokens: 4096,
});
console.log(await parent.run(
  "Delegate to exactly two child agents from ONE run_js call. Use Promise.all so they run concurrently: createAgent({name:'left'}).run('Read left.json with run_js and return the numeric sum of its array') and createAgent({name:'right'}).run('Read right.json with run_js and return the numeric sum of its array'). Do not import createAgent; it is already bound. Each child MUST run its own JavaScript to read its file. Print the child results to the parent tool output, then combine their answers and return a JSON object with numeric left, right and total. Do not compute the files yourself instead of delegating.",
  { cacheKey: "noe-increment-13-parallel", maxSteps: 8 },
));
