import { createAgent } from "../lib/agent.js";
const agent = createAgent({ name: "service" });
console.log(await agent.run("Call webSearch('NOE spike') using run_js. Return the complete resulting JSON. This is a local authenticated fixture, not an Internet search.", { cacheKey: "noe-spike-service-v1" }));
