// Portable workflow v1 protocol. No provider calls or repository dependencies.
export class WorkflowError extends Error {
  constructor(code, message) { super(message); this.name = "WorkflowError"; this.code = code; }
}
function need(ok, message) { if (!ok) throw new WorkflowError("invalid-protocol", message); }
function object(value, keys, label) {
  need(value && typeof value === "object" && !Array.isArray(value), label + " must be an object");
  need(Object.keys(value).every(key => keys.includes(key)), label + " has unknown fields");
}
function exact(value, keys, label) {
  object(value, keys, label);
  need(keys.every(key => Object.hasOwn(value, key)), label + " is incomplete");
}
function text(value, label) { need(typeof value === "string" && value.trim().length > 0 && !value.includes("\0"), label + " must be nonempty text"); }
function list(value, label, nonempty = true) { need(Array.isArray(value) && (!nonempty || value.length > 0), label + " must be an array" + (nonempty ? " with entries" : "")); }
function strings(value, label, nonempty = true) {
  list(value, label, nonempty); value.forEach(v => text(v, label));
  need(new Set(value).size === value.length, label + " contains duplicates");
}
function subset(values, allowed, label, nonempty = true) {
  strings(values, label, nonempty);
  need(values.every(v => allowed.includes(v)), label + " contains unknown IDs");
}
export function repositoryPath(value, label = "path") {
  text(value, label);
  need(!value.startsWith("/") && !/^[A-Za-z]:/.test(value) && !value.includes("\\") &&
    value.split("/").every(p => p !== ".." && p !== "." && p !== "" && p.toLowerCase() !== ".git"),
    label + " must be a literal repository-relative path outside Git metadata");
  return value;
}
const sol = {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high"};
export const DEFAULT_ROLES = Object.freeze({
  general: Object.freeze({...sol}), planning: Object.freeze({...sol}),
  review: Object.freeze({...sol, effort: "xhigh"}),
  routine: Object.freeze({provider: "openai-chatgpt", model: "gpt-6-luna", effort: "max"}),
  difficult: Object.freeze({...sol}),
});
const efforts = ["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"];
export function validateConfig(input) {
  object(input, ["version", "requirementsFile", "requirementIds", "rulesFiles", "git", "roles", "artifactsDir"], "config");
  need(input.version === 1, "config.version must be 1");
  repositoryPath(input.requirementsFile, "requirementsFile");
  if (input.requirementIds !== undefined) strings(input.requirementIds, "requirementIds");
  const rulesFiles = input.rulesFiles ?? ["AGENTS.md"];
  strings(rulesFiles, "rulesFiles", false); rulesFiles.forEach(p => repositoryPath(p));
  exact(input.git, ["branch", "remote", "pushBranch", "branchPushSafe", "releaseSafetyReason"], "git");
  ["branch", "remote", "pushBranch", "releaseSafetyReason"].forEach(k => text(input.git[k], "git." + k));
  need(!input.git.remote.startsWith("-"), "git.remote cannot be an option");
  need(input.git.branchPushSafe === true, "branch push release safety must be explicitly confirmed");
  const roles = {};
  object(input.roles ?? {}, Object.keys(DEFAULT_ROLES), "roles");
  for (const [role, defaults] of Object.entries(DEFAULT_ROLES)) {
    const override = input.roles?.[role] ?? {};
    object(override, ["provider", "model", "effort"], "roles." + role);
    const model = {...defaults, ...override};
    text(model.provider, "provider"); text(model.model, "model");
    need(efforts.includes(model.effort), "unsupported effort");
    roles[role] = model;
  }
  if (input.artifactsDir !== undefined) text(input.artifactsDir, "artifactsDir");
  return {...input, rulesFiles: input.rulesFiles === undefined ? undefined : [...rulesFiles], roles, git: {...input.git}};
}
// Default inventory: declared IDs where available; otherwise the entire unchanged
// source is one coverage group. Reviewers audit all source behaviors in either mode.
export const DOCUMENT_REQUIREMENT_ID = "DOCUMENT";
export function requirementInventory(source, supplied) {
  text(source, "requirements source");
  const declared = [...source.matchAll(/^\s*(?:[-*]\s+|#{1,6}\s+)?(?:\*\*)?([A-Z][A-Z0-9-]*[0-9])(?:\*\*)?\s*[:—]/gm)].map(m => m[1]);
  need(new Set(declared).size === declared.length, "duplicate requirement declarations");
  if (supplied === undefined && declared.length === 0) return [DOCUMENT_REQUIREMENT_ID];
  const ids = supplied ?? declared;
  strings(ids, "authoritative requirement IDs");
  need(declared.every(id => ids.includes(id)), "requirementIds omits declared requirements");
  // Body references/examples are not declarations; full-source review covers prose.
  need(ids.every(id => source.includes(id)), "requirement ID absent from source");
  return [...ids];
}
export function validateCommand(command) {
  exact(command, ["argv", "cwd", "purpose"], "command");
  list(command.argv, "command.argv");
  command.argv.forEach((v, i) => {
    need(typeof v === "string" && !v.includes("\0") && (i !== 0 || v.length > 0), "invalid command argument");
  });
  need(command.cwd === "." || repositoryPath(command.cwd), "invalid command cwd");
  text(command.purpose, "command.purpose");
  return command;
}
function commands(value, label, nonempty = true) { list(value, label, nonempty); value.forEach(validateCommand); }
export function parseResult(answer) {
  need(typeof answer === "string", "agent answer must be JSON text");
  let result;
  try { result = JSON.parse(answer); } catch { throw new WorkflowError("invalid-protocol", "agent answer is not exact JSON"); }
  if (result?.type === "blocked") {
    exact(result, ["type", "version", "reason", "evidence"], "blocked result");
    need(result.version === 1, "unsupported blocked version");
    text(result.reason, "blocked reason"); text(result.evidence, "blocked evidence");
    throw new WorkflowError("blocked", result.reason + ": " + result.evidence);
  }
  return result;
}
export function validatePlan(plan, ids, revision) {
  exact(plan, ["type", "version", "revision", "requirements", "slices", "integration"], "plan");
  need(plan.type === "plan" && plan.version === 1 && plan.revision === revision && Number.isSafeInteger(revision) && revision > 0, "wrong plan type/version/revision");
  list(plan.requirements, "requirements"); list(plan.slices, "slices");
  const sliceIds = plan.slices.map(s => s.id);
  strings(sliceIds, "slice IDs");
  const requirementIds = plan.requirements.map(r => r.id);
  subset(requirementIds, ids, "plan requirement IDs");
  need(ids.length === requirementIds.length, "plan omits requirements");
  const prior = [];
  for (const slice of plan.slices) {
    exact(slice, ["id", "title", "dependsOn", "requirementIds", "ownedPaths", "difficulty", "difficultyReason", "verification"], "slice");
    text(slice.title, "slice title"); subset(slice.dependsOn, prior, "slice dependencies", false);
    subset(slice.requirementIds, ids, "slice requirements");
    strings(slice.ownedPaths, "slice owned paths"); slice.ownedPaths.forEach(p => repositoryPath(p));
    need(["routine", "difficult"].includes(slice.difficulty), "slice difficulty must be routine or difficult");
    text(slice.difficultyReason, "difficulty reason"); commands(slice.verification, "slice verification");
    prior.push(slice.id);
  }
  for (const requirement of plan.requirements) {
    exact(requirement, ["id", "acceptance", "slices"], "requirement");
    list(requirement.acceptance, "acceptance paths");
    for (const acceptance of requirement.acceptance) {
      exact(acceptance, ["behavior", "usagePath"], "acceptance");
      text(acceptance.behavior, "acceptance behavior"); text(acceptance.usagePath, "acceptance usage path");
    }
    subset(requirement.slices, sliceIds, "requirement slices");
    const mapped = plan.slices.filter(s => s.requirementIds.includes(requirement.id)).map(s => s.id);
    need(mapped.length === requirement.slices.length && mapped.every(id => requirement.slices.includes(id)), "inconsistent requirement/slice mapping");
  }
  exact(plan.integration, ["commands", "regression", "liveChecks"], "integration");
  commands(plan.integration.commands, "integration commands");
  exact(plan.integration.regression, ["rationale", "commands"], "regression");
  text(plan.integration.regression.rationale, "regression rationale");
  commands(plan.integration.regression.commands, "regression commands", false);
  list(plan.integration.liveChecks, "live checks", false);
  for (const live of plan.integration.liveChecks) {
    exact(live, ["reason", "command"], "live check"); text(live.reason, "live check reason"); validateCommand(live.command);
  }
  return plan;
}
export function validateReview(review, ids, revision, mode) {
  exact(review, ["type", "version", "revision", "mode", "findings"], "review");
  need(review.type === "review" && review.version === 1 && review.revision === revision && review.mode === mode, "wrong review type/version/revision/mode");
  list(review.findings, "findings", false);
  const findingIds = [];
  for (const finding of review.findings) {
    exact(finding, ["id", "requirementIds", "evidence", "action"], "finding");
    text(finding.id, "finding ID"); subset(finding.requirementIds, ids, "finding requirements");
    text(finding.evidence, "finding evidence"); text(finding.action, "finding action");
    findingIds.push(finding.id);
  }
  strings(findingIds, "finding IDs", false);
  return review;
}
// Slice 2 worker contract: command results remain authoritative, never worker claims.
export function validateImplementation(result, sliceId, ids) {
  exact(result, ["type", "version", "sliceId", "summary", "changedPaths", "requirementIds", "escalation"], "implementation");
  need(result.type === "implementation" && result.version === 1 && result.sliceId === sliceId, "wrong implementation identity");
  text(result.summary, "implementation summary"); subset(result.requirementIds, ids, "implementation requirements");
  strings(result.changedPaths, "changed paths", false); result.changedPaths.forEach(p => repositoryPath(p));
  if (result.escalation !== null) {
    exact(result.escalation, ["reason", "evidence"], "escalation");
    text(result.escalation.reason, "escalation reason"); text(result.escalation.evidence, "escalation evidence");
  }
  return result;
}
/** @param {null | {reason: string, evidence: string}} escalation */
export function implementationRole(config, slice, escalation = null) {
  if (escalation !== null) {
    exact(escalation, ["reason", "evidence"], "escalation");
    text(escalation.reason, "escalation reason"); text(escalation.evidence, "escalation evidence");
  }
  need(["routine", "difficult"].includes(slice.difficulty), "invalid difficulty");
  return config.roles[escalation || slice.difficulty === "difficult" ? "difficult" : "routine"];
}
