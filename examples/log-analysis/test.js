// Run with NOE; no provider, Node.js, npm, or external services.
import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import process from "node:process";
import { analyzeJSONL, toMarkdown } from "./analyze.js";

const dir = dirname(resolve(process.argv[1]));
let checks = 0;
function equal(actual, expected, label) {
  if (JSON.stringify(actual) !== JSON.stringify(expected)) {
    throw new Error(`${label}: got ${JSON.stringify(actual)}, expected ${JSON.stringify(expected)}`);
  }
  checks++;
}
const complete = analyzeJSONL(readFileSync(resolve(dir, "fixtures/complete.jsonl"), "utf8"), "complete");
equal(complete.modelCalls, 2, "only model.finished counts");
equal(complete.toolCalls, 2, "tool.finished counts");
equal(complete.promptTokens, { value: 400, knownRequests: 2, totalRequests: 2 }, "snapshot ignored; input alias");
equal(complete.cachedTokens.value, 240, "cached sum");
equal(complete.uncachedPromptTokens.value, 160, "non-cache sum");
equal(complete.cacheHitRate.value, 0.6, "token-weighted rate, not request mean");
equal(complete.outputOverflows.value, 1, "model-facing overflow only, not raw/capture omission");
equal(complete.requests.map((r) => [r.runId, r.modelStepId, r.inputTokens, r.cachedTokens, r.uncachedTokens, r.cacheHitRate]),
  [["run-a", "step-1", 100, 0, 100, 0]], "low-hit identification, explicit cached zero");
equal(analyzeJSONL(readFileSync(resolve(dir, "fixtures/complete.jsonl"), "utf8"), "all", { below: 1 }).requests[1].modelStepId, "step-2", "origin identity fallback");
const partial = analyzeJSONL("\n" + readFileSync(resolve(dir, "fixtures/partial.jsonl"), "utf8") + "\n", "partial");
equal(partial.modelCalls, 4, "blank lines ignored");
equal(partial.promptTokens, { value: 50, knownRequests: 2, totalRequests: 4 }, "partial input coverage");
equal(partial.cachedTokens, { value: 5, knownRequests: 2, totalRequests: 4 }, "independent cached coverage");
equal(partial.uncachedPromptTokens, { value: 0, knownRequests: 1, totalRequests: 4 }, "zero is known, missing is not");
equal(partial.cacheHitRate, { value: null, knownRequests: 1, totalRequests: 4, inputTokens: 0, cachedTokens: 0 }, "zero denominator; do not divide mismatched subsets");
equal(partial.outputOverflows, { value: 0, knownRequests: 1, totalRequests: 3 }, "unavailable tool result unknown");
equal(partial.requests.map((r) => [r.line, r.cacheHitRate, r.rateStatus]),
  [[2, null, "zero-input"], [3, null, "unknown"], [4, null, "unknown"], [5, null, "unknown"]], "unknown requests visible with source lines");
const toolCases = [
  [{ type: "tool.finished", modelResultAvailable: true, result: null },
    { value: 0, knownRequests: 1, totalRequests: 1 }, "normal null result is known"],
  [{ type: "tool.finished", modelResultAvailable: true },
    { value: null, knownRequests: 0, totalRequests: 1 }, "missing result field is unknown"],
  [{ type: "tool.finished", modelResultAvailable: false, result: null, hookError: "afterTool failed" },
    { value: null, knownRequests: 0, totalRequests: 1 }, "hook failure result is unavailable"],
];
for (const [event, expected, label] of toolCases) {
  equal(analyzeJSONL(JSON.stringify(event), label).outputOverflows, expected, label);
}
const empty = analyzeJSONL("\n  \n", "empty");
equal([empty.modelCalls, empty.toolCalls, empty.promptTokens.value, empty.cacheHitRate.value], [0, 0, null, null], "empty file");
equal(analyzeJSONL('{"type":"model.finished"}', "missing").cachedTokens.value, null, "missing never reported as zero");
let error = "";
try { analyzeJSONL('\n{}\n{broken', "bad.jsonl"); } catch (failure) { error = failure.message; }
equal(error, "bad.jsonl:3: invalid JSON", "malformed JSON context");
const markdown = toMarkdown([complete, partial]);
equal(markdown.includes("60.00% (2/2 requests)") && markdown.includes("unknown / n/a (1/4 requests)") && markdown.includes("step-1"), true, "Markdown values, coverage and identities");
equal(toMarkdown([{ ...empty, file: "a|<b>\nfile" }]).includes("a&#124;&lt;b&gt; file"), true, "Markdown filename escaping");
console.log(`PASS: ${checks} checks`);
