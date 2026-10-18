import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "openrouter",
  model: "deepseek/deepseek-v4.1-flash",
  effort: "auto",
  name: "control-demo",
  system: "Complete tasks without waiting for approval. Continue follow-up tasks using the retained conversation.",
  maxSteps: 6,
}, "For this initial task, use run_js to wait 45 seconds, then report that the initial task finished. Do not ask for human input or approval. The delay lets a person in a separate terminal steer this active task; incorporate any received instruction after the current JavaScript finishes.", { keepAlive: true });

console.log(JSON.stringify(snapshot, null, 2));
