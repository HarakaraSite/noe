import { createAgent } from "../lib/agent.js";
const agent = createAgent({ name: "single" });
console.log(await agent.run("Read numbers.json from the fixture directory using run_js. Calculate count, total and average. Return only JSON with those three numeric fields.", { cacheKey: "noe-spike-single-v1" }));
