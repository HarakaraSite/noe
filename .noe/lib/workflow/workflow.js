// Standalone composition API. Slice 1 ends in "planned", never claims implementation completion.
import {DOCUMENT_REQUIREMENT_ID, WorkflowError, parseResult, requirementInventory, validateConfig, validatePlan, validateReview} from "./protocol.js";
import {createAgentPool, createArtifactStore, createRuntime, git, prepareRepository} from "./runtime.js";

export const PLAN_CONTRACT = {
  type: "plan", version: 1, revision: 1,
  requirements: [{id: "R1", acceptance: [{behavior: "Observable required behavior", usagePath: "Actual user/API execution path"}], slices: ["S1"]}],
  slices: [{
    id: "S1", title: "Vertical slice", dependsOn: [], requirementIds: ["R1"], ownedPaths: ["src/feature.js"],
    difficulty: "routine", difficultyReason: "Concrete reason for routine or difficult selection",
    verification: [{argv: ["test-program", "argument"], cwd: ".", purpose: "Required behavior this verifies"}],
  }],
  integration: {
    commands: [{argv: ["integration-program"], cwd: ".", purpose: "Requirement-level and cross-slice user paths"}],
    regression: {rationale: "Impact-based reason to run or omit product-wide checks", commands: []},
    liveChecks: [],
  },
};
export function reviewContract(mode, revision) {
  return {type: "review", version: 1, revision, mode, findings: []};
}
const JSON_ONLY = 'Return exactly one JSON object, no Markdown or surrounding prose. No unknown fields. If genuinely blocked, return {"type":"blocked","version":1,"reason":"...","evidence":"..."}. Failed commands and valid findings require repair, not waiver.';
function context(requirements, rules, ids) {
  const coverage = ids.length === 1 && ids[0] === DOCUMENT_REQUIREMENT_ID
    ? {mode: "whole-document", rule: "DOCUMENT anchors the entire original requirements source, not a summary. Enumerate every required behavior as a separate acceptance entry with its source section or quoted wording and actual usage path. Both reviewers must compare every source behavior with these entries; a generic implement-the-document entry cannot approve."}
    : {mode: "explicit-ids", rule: "IDs are an index, not a substitute for the full original source. Both reviewers must audit all required behaviors, including prose outside numbered declarations, against acceptance entries."};
  return JSON.stringify({authoritativeRequirementIds: ids, coverage, requirements, repositoryRules: rules}, null, 2);
}
export async function planWorkflow({config, requirements, rules, ids, pool, store}) {
  const basis = context(requirements, rules, ids);
  let revision = 1;
  let priorPlan = null;
  let priorReviews = [];
  for (;;) {
    const instruction = [
      "Create or repair the implementation plan. Planning is read-only; do not modify any repository files.",
      "Cover every authoritative requirement exactly once in requirements and map each to acceptance behavior, actual usage paths and implementing slices. Follow the authoritative coverage rule; preserve every behavior in the full original source.",
      "Use ordered dependency-respecting slices (one is allowed), explicit literal owned paths, and command argv arrays without shell interpolation.",
      "Select routine/difficult difficulty per slice with a reason. Routine uses Luna/max; difficult uses Sol/high unless configured otherwise. Escalation later needs concrete evidence.",
      "Provide final requirement-level/cross-slice verification, impact-based regression rationale, and optional explicit live checks only when needed.",
      "Do not omit, weaken, reinterpret away or change requirements. Resolve every valid finding; reviewers must verify the revision independently.",
      JSON_ONLY, "Exact plan shape (replace sample values; revision must be " + revision + "):",
      JSON.stringify({...PLAN_CONTRACT, revision}, null, 2), "Authoritative context:", basis,
      "Previous plan and review findings:", JSON.stringify({priorPlan, priorReviews}, null, 2),
    ].join("\n");
    const answer = await pool.task("planner", config.roles.planning, instruction, "You plan read-only. No implementation, staging, commits, pushing or requirement changes.");
    const plan = validatePlan(parseResult(answer), ids, revision);
    store.json("plan-" + revision + ".json", plan);
    store.event("plan-created", {revision});
    const modes = ["normal", "critical"];
    // Both reviewers see the same immutable revision; no fresh reviewer output is shared.
    // Await every completion even if its peer fails, avoiding orphan launch/cleanup races.
    const outcomes = await Promise.all(modes.map(async mode => {
      try {
        const reviewAnswer = await pool.task("plan-review-" + mode, config.roles.review, [
          "Independently review this plan. Read-only: do not modify any files.",
          mode === "normal" ? "Check explicit requirement coverage, viable execution paths, ownership, dependencies and verification." :
            "Challenge assumptions, omitted usage paths, integration gaps and unsafe contracts. No objection quota or hypothetical scope expansion.",
          "Audit full original-source coverage using the authoritative coverage rule, not just whether every ID is present. Any omitted behavior needs a finding anchored to its source section or quoted wording.",
          "Only unresolved concrete findings belong in findings. Each needs requirement IDs, path/requirement evidence and a corrective action.",
          "Approval is ONLY an empty validated findings list. Re-review your prior findings against this exact new revision.",
          JSON_ONLY, "Exact approval shape:", JSON.stringify(reviewContract(mode, revision)),
          'Finding shape: {"id":"F1","requirementIds":["R1"],"evidence":"Requirement/path evidence","action":"Concrete corrective action"}',
          "Authoritative context:", basis, "Plan revision:", JSON.stringify(plan, null, 2),
          "Your prior review:", JSON.stringify(priorReviews.find(r => r.mode === mode) ?? null),
        ].join("\n"), "Independent " + mode + " plan reviewer. Read-only; do not implement or change requirements.");
        const review = validateReview(parseResult(reviewAnswer), ids, revision, mode);
        store.json("review-" + revision + "-" + mode + ".json", review);
        return {review};
      } catch (error) { return {error}; }
    }));
    const failed = outcomes.find(o => o.error);
    if (failed) throw failed.error;
    const reviews = outcomes.map(o => o.review);
    store.event("plan-reviewed", {revision, findings: reviews.map(r => ({mode: r.mode, count: r.findings.length}))});
    if (reviews.every(r => r.findings.length === 0)) {
      store.event("plan-approved", {revision});
      return {plan, reviews};
    }
    priorPlan = plan;
    priorReviews = reviews;
    revision++;
  }
}
/**
 * Standalone only: await in a NOE subprocess launched from the Git root.
 * Not a managed run_js continuation/steer API. No automatic interrupted-run resume.
 * @param {*} input
 * @param {{runtime?: Awaited<ReturnType<typeof createRuntime>>, stopAfter?: string}} options
 * Future phases reuse config, runtime.command, pool.task, store, repository baseline.
 * Slice 2 adds implementation -> focused checks/review -> scoped commits ->
 * requirement integration/repair commits -> one explicit safe branch push.
 */
export async function runWorkflow(input, {runtime, stopAfter = "planning"} = {}) {
  if (stopAfter !== "planning") throw new WorkflowError("not-implemented", "Bootstrap slice 1 supports planning only");
  const config = validateConfig(input);
  const execution = runtime ?? await createRuntime();
  const repository = await prepareRepository(execution, config);
  const store = createArtifactStore(repository.artifactBase);
  let pool;
  let result;
  let failure;
  try {
    store.json("configuration.json", config);
    store.json("baseline.json", repository);
    store.event("started", {cwd: execution.cwd, stopAfter});
    const requirements = execution.read(config.requirementsFile);
    const ids = requirementInventory(requirements, config.requirementIds);
    const rules = [];
    for (const path of config.rulesFiles ?? ["AGENTS.md"]) {
      if (config.rulesFiles === undefined && !execution.exists(path)) {
        store.event("default-rules-absent", {path});
      } else {
        rules.push({path, text: execution.read(path)});
      }
    }
    store.json("inputs.json", {requirements, ids, rules});
    pool = createAgentPool(execution, store);
    const approved = await planWorkflow({config, requirements, rules, ids, pool, store});
    // Planning/review workers are read-only. Never promote a revision after Git mutation.
    if ((await git(execution, ["rev-parse", "HEAD"])).trim() !== repository.head ||
        (await git(execution, ["diff", "--name-only", "-z", "--"])) ||
        (await git(execution, ["diff", "--cached", "--name-only", "-z", "--"]))) {
      throw new WorkflowError("planning-mutation", "Planning workers changed tracked/index state or HEAD");
    }
    result = {status: "planned", ...approved, artifactDirectory: store.directory, baseline: repository};
  } catch (error) { failure = error; }
  try { if (pool) await pool.close(); }
  catch (error) {
    store.event("cleanup-failed", {error: String(error)});
    if (!failure) failure = error;
  }
  if (failure || !result) {
    failure = failure || new WorkflowError("execution", "No successful planning result was produced");
    const status = failure.code === "cancelled" ? "cancelled" : failure.code === "blocked" ? "blocked" : "failed";
    store.json("outcome.json", {status, code: failure.code ?? "execution", error: String(failure), artifactDirectory: store.directory});
    failure.artifactDirectory = store.directory;
    throw failure; // Preserve cancellation/error identity; never report failed work as completion.
  }
  store.json("outcome.json", result);
  return result;
}
