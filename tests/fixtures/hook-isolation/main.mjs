// Read completed answers and hook metadata without an event file.
import { run_agent, wait_task, read_task_result } from "noe:agent";
import { writeFileSync } from "node:fs";

const snapshot = await run_agent({
  provider: "openrouter",
  model: "fixture/model",
  effort: "auto",
  maxSteps: 1,
  hooks: ["./hooks.mjs"],
}, "Finish despite optional hook recording failures.");
const outcome = snapshot.lastResult;
if (outcome === null) throw new Error("missing retained outcome");
const reference = { agentId: snapshot.agentId, commandId: outcome.commandId };
const metadata = await wait_task({ ...reference, includeAnswer: false });
const answer = await read_task_result(reference);
const result = { outcome, metadata, answer: answer.text };
writeFileSync("./result.json", JSON.stringify(result));
console.log(JSON.stringify(result));
