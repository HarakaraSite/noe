import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "parent",
  maxTokens: 4096,
  cacheKey: "noe-increment-13-parallel",
  maxSteps: 8,
},
  "Delegate to exactly two child agents from ONE run_js call. Use Promise.all so they run concurrently: run_agent({name:'left'},'Read left.json with run_js and return the numeric sum of its array') and run_agent({name:'right'},'Read right.json with run_js and return the numeric sum of its array'). Do not import run_agent; it is already bound. Each child MUST run its own JavaScript to read its file. Print the child results to the parent tool output, then combine their answers and return a JSON object with numeric left, right and total. Do not compute the files yourself instead of delegating."
);
console.log(result.lastResult.answer);
