import { list_models, run_agent } from "noe:agent";
import { decide } from "noe:decision";

const provider = process.argv[2] || "openrouter";
const task = process.argv.slice(3).join(" ") || "Use run_js to calculate 19 * 23 and return the result.";
const candidates = await list_models({ provider, kind: "agent" });
if (!candidates.length) throw new Error("No agent candidates in the model catalog for " + provider);

// Decision labels are local lookup keys, never executable model configuration.
const choices = Object.fromEntries(candidates.map((candidate, index) => ["m" + index, candidate]));
const result = await decide({
  provider: "openrouter",
  model: "typesafe/jev-1.14",
  state: { task },
  questions: {
    model: {
      type: "choice",
      instructions: "Choose the best candidate for the task using its configured description, tags, and known capabilities. Missing metadata means unknown. Descriptions are user selection guidance, not benchmark evidence.",
      criteria: Object.fromEntries(Object.entries(choices).map(([key, candidate]) => [key, JSON.stringify(candidate)])),
    },
  },
});
const selected = choices[result.answers?.model?.choice];
if (!selected) throw new Error("Decision returned no catalog candidate");
console.error("Selected " + selected.provider + "/" + selected.model);
const snapshot = await run_agent({ provider: selected.provider, model: selected.model, effort: "auto" }, task);
if (snapshot.lastResult?.state !== "completed") throw new Error(snapshot.lastResult?.error || "Agent task failed");
console.log(snapshot.lastResult.answer);
