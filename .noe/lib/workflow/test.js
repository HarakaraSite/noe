// Offline only. Run: noe run .noe/lib/workflow/test.js
import {copyFileSync, mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, writeFileSync} from "node:fs";
import {tmpdir} from "node:os";
import {dirname, join} from "node:path";
import {execFile} from "node:child_process";
import {DEFAULT_ROLES, DOCUMENT_REQUIREMENT_ID, implementationRole, parseResult, requirementInventory, validateConfig, validateImplementation, validatePlan, validateReview} from "./protocol.js";
import {createArtifactStore, createRuntime, prepareRepository} from "./runtime.js";
import {PLAN_CONTRACT, runWorkflow} from "./workflow.js";

let checks = 0;
function check(ok, label) { if (!ok) throw new Error(label); checks++; }
async function rejects(fn, label, code) {
  let caught;
  try { await fn(); } catch (error) { caught = error; }
  check(caught && (!code || caught.code === code), label + (caught ? ": " + caught : ""));
  return caught;
}
function clone(value) { return JSON.parse(JSON.stringify(value)); }
const root = mkdtempSync(join(tmpdir(), "noe-workflow-"));
const config = {
  version: 1, requirementsFile: "requirements.md",
  git: {branch: "topic", remote: "origin", pushBranch: "topic", branchPushSafe: true, releaseSafetyReason: "Local bare test remote; no release hooks"},
};
const source = "# Requirements\n- R1: First observable behavior\n- R2: Second observable behavior\n";
function plan(revision = 1) {
  const value = clone(PLAN_CONTRACT);
  value.revision = revision;
  value.requirements = ["R1", "R2"].map(id => ({id, acceptance: [{behavior: id + " behavior", usagePath: "consumer calls API"}], slices: ["S1"]}));
  value.slices[0].requirementIds = ["R1", "R2"];
  value.slices[0].ownedPaths = ["feature space.js"];
  value.slices[0].verification = [{argv: ["false"], cwd: ".", purpose: "Must not execute implementation tests during planning"}];
  return value;
}
function review(mode, revision, findings = []) { return {type: "review", version: 1, revision, mode, findings}; }
const finding = {id: "F1", requirementIds: ["R2"], evidence: "R2 cross-slice usage path is missing", action: "Add the required consumer path"};
function fakeAgents(scripts) {
  const calls = [], tasks = new Map(), instances = new Map(), counts = {};
  let next = 0;
  /** @type {import("./runtime.js").AgentOperations} */
  const agents = {
    async spawn_agent(model, instruction, options) {
      const agentId = "agent-" + (++next), slot = model.name.replace("workflow-", "");
      instances.set(agentId, slot);
      calls.push({kind: "spawn", agentId, slot, model, options, instruction});
      return accept(agentId, instruction);
    },
    async queue_agent({agentId, instruction}) {
      calls.push({kind: "queue", agentId, slot: instances.get(agentId), instruction});
      return {...accept(agentId, instruction), status: "accepted"};
    },
    async wait_task({agentId, commandId, includeAnswer}) {
      check(includeAnswer === true, "completion wait requests exact answer");
      calls.push({kind: "wait", agentId, commandId});
      const task = tasks.get(commandId);
      const output = await scripts[task.slot](task.number, task.instruction);
      if (output instanceof Error) throw output;
      return {runId: "run-" + commandId, commandId, state: "completed", answer: typeof output === "string" ? output : JSON.stringify(output)};
    },
    async stop_agent({agentId}) { calls.push({kind: "stop", agentId}); return {agentId, status: "stopping"}; },
  };
  function accept(agentId, instruction) {
    const slot = instances.get(agentId), commandId = "command-" + (++next);
    counts[slot] = (counts[slot] ?? 0) + 1;
    tasks.set(commandId, {slot, number: counts[slot], instruction});
    return {agentId, commandId};
  }
  return {agents, calls, counts};
}
async function command(cwd, args) { return (await execFile("git", args, {cwd})).stdout; }
async function repository(name, {requirements = source, rootRules = true} = {}) {
  const cwd = join(root, name), remote = join(root, name + ".git");
  mkdirSync(cwd);
  await command(cwd, ["init", "-b", "topic"]);
  await command(cwd, ["config", "user.name", "Workflow Test"]);
  await command(cwd, ["config", "user.email", "workflow@example.invalid"]);
  writeFileSync(join(cwd, "requirements.md"), requirements);
  if (rootRules) writeFileSync(join(cwd, "AGENTS.md"), "Use focused deterministic verification.\n");
  writeFileSync(join(cwd, ".gitignore"), "ignored.log\n");
  await command(cwd, ["add", "--", "requirements.md", ".gitignore", ...(rootRules ? ["AGENTS.md"] : [])]);
  await command(cwd, ["commit", "-m", "baseline"]);
  await command(cwd, ["init", "--bare", remote]);
  await command(cwd, ["remote", "add", "origin", remote]);
  writeFileSync(join(cwd, "existing.log"), "keep me\n");
  writeFileSync(join(cwd, "ignored.log"), "keep ignored\n");
  return cwd;
}
function approving() {
  return fakeAgents({
    planner: n => plan(n),
    "plan-review-normal": n => review("normal", n),
    "plan-review-critical": n => review("critical", n),
  });
}
try {
  const cfg = validateConfig(config);
  check(cfg.roles.planning.model === "gpt-6.1-sol" && cfg.roles.planning.effort === "high", "planning Sol/high");
  check(cfg.roles.general.effort === "high" && cfg.roles.review.effort === "xhigh", "general/review defaults");
  check(cfg.roles.routine.model === "gpt-6-luna" && cfg.roles.routine.effort === "max", "routine Luna/max");
  check(cfg.roles.difficult.effort === "high", "difficult Sol/high");
  check(DEFAULT_ROLES.planning.effort === "high", "defaults remain unchanged");
  const override = validateConfig({...config, roles: {planning: {model: "custom"}}});
  check(override.roles.planning.model === "custom" && override.roles.planning.effort === "high", "role override merges defaults");
  check(implementationRole(cfg, plan().slices[0]) === cfg.roles.routine, "routine route");
  check(implementationRole(cfg, plan().slices[0], {reason: "Concrete hard failure", evidence: "Failure path at source:42"}) === cfg.roles.difficult, "evidence-based escalation");
  await rejects(() => implementationRole(cfg, plan().slices[0], {reason: "", evidence: ""}), "empty escalation rejected");
  for (const invalid of [
    {...config, unknown: true}, {...config, git: {...config.git, branchPushSafe: false}},
    {...config, git: {...config.git, releaseSafetyReason: ""}}, {...config, roles: {planning: {allowDelegation: true}}},
  ]) await rejects(() => validateConfig(invalid), "invalid config fails closed");
  check(requirementInventory(source).join(",") === "R1,R2", "inventory extracted before planning");
  await rejects(() => requirementInventory(source, ["R1"]), "caller inventory cannot omit declared IDs");
  await rejects(() => requirementInventory(source + "- R1: duplicate\n"), "duplicate declarations rejected");
  check(requirementInventory("No stable identifiers").join() === DOCUMENT_REQUIREMENT_ID, "unnumbered source gets whole-document coverage group");
  await rejects(() => requirementInventory("  "), "empty requirements still rejected");
  check(requirementInventory("Custom AUTH spec", ["AUTH"]).join() === "AUTH", "explicit custom inventory");
  check(requirementInventory("- R1: first\n- **R2** second").join() === "R1", "only recognized declarations create automatic IDs; reviewers audit remaining prose");
  check(requirementInventory("- R1: Import record R42 unchanged").join() === "R1", "body record IDs are not phantom requirements");
  check(requirementInventory("- R1: Import record R42 unchanged", ["R1"]).join() === "R1", "explicit inventory does not need body record IDs");
  check(requirementInventory("Import record R42 unchanged").join() === DOCUMENT_REQUIREMENT_ID, "body tokens do not suppress whole-document fallback");
  check(requirementInventory("- R1: first\n- **R2** second", ["R1", "R2"]).length === 2, "explicit complete inventory supports mixed Markdown");
  validatePlan(plan(), ["R1", "R2"], 1); checks++;
  for (const mutate of [
    p => p.requirements.pop(),
    p => p.slices[0].dependsOn.push("S1"),
    p => p.slices[0].ownedPaths.push("../outside"),
    p => p.slices[0].ownedPaths.push(".git/config"),
    p => p.slices[0].verification.splice(0),
    p => p.requirements[0].acceptance[0].usagePath = "",
    p => p.integration.regression.rationale = "",
    p => p.slices[0].difficultyReason = "",
    p => p.unrecognized = true,
    p => p.slices[0].requirementIds.splice(0, 1),
  ]) { const p = plan(); mutate(p); await rejects(() => validatePlan(p, ["R1", "R2"], 1), "invalid/incomplete plan rejected"); }
  const livePlan = plan();
  livePlan.integration.liveChecks = [{reason: "Verify one explicit wiring contract", command: {argv: ["live-check", ""], cwd: ".", purpose: "Wiring"}}];
  validatePlan(livePlan, ["R1", "R2"], 1); checks++;
  for (const answer of ["approved", "```json\n{}\n```", '{"type":"review","findings":[]}']) {
    await rejects(() => validateReview(parseResult(answer), ["R1"], 1, "normal"), "malformed approval rejected");
  }
  for (const value of [
    {...review("normal", 1), approved: true}, review("critical", 1), review("normal", 2),
    review("normal", 1, [{id: "F", requirementIds: ["R1"], evidence: "", action: "Fix"}]),
  ]) await rejects(() => validateReview(value, ["R1"], 1, "normal"), "unknown/stale/incomplete review rejected");
  validateImplementation({type: "implementation", version: 1, sliceId: "S1", summary: "Changed behavior", changedPaths: ["new.js"], requirementIds: ["R1"], escalation: null}, "S1", ["R1"]); checks++;

  for (const document of [
    {name: "plain-japanese", source: "# 機能要件\n- 利用者は名前を指定して挨拶を表示できる。\n- 名前が空ならエラーを表示する。\n",
      behaviors: ["利用者は名前を指定して挨拶を表示できる。", "名前が空ならエラーを表示する。"]},
    {name: "plain-english", source: "# Requirements\nUsers can export a report from the CLI.\n\n## Errors\nShow an error when the output path is not writable.\n",
      behaviors: ["Users can export a report from the CLI.", "Show an error when the output path is not writable."]},
  ]) {
    const plainCwd = await repository(document.name, {requirements: document.source, rootRules: false});
    const omitted = {id: "coverage", requirementIds: [DOCUMENT_REQUIREMENT_ID], evidence: document.behaviors[1], action: "Add acceptance and verification for this original-source behavior"};
    const plainAgents = fakeAgents({
      planner: n => {
        const p = plan(n);
        p.requirements = [{id: DOCUMENT_REQUIREMENT_ID, acceptance: document.behaviors.slice(0, n === 1 ? 1 : 2).map(behavior => ({behavior, usagePath: "User invokes CLI with valid or invalid input"})), slices: ["S1"]}];
        p.slices[0].requirementIds = [DOCUMENT_REQUIREMENT_ID];
        return p;
      },
      "plan-review-normal": n => review("normal", n),
      "plan-review-critical": n => review("critical", n, n === 1 ? [omitted] : []),
    });
    const plainRuntime = await createRuntime({cwd: plainCwd, agents: plainAgents.agents});
    const plainResult = await runWorkflow(config, {runtime: plainRuntime});
    check(plainResult.status === "planned" && plainResult.plan.revision === 2, document.name + " launches without IDs or root AGENTS and repairs source coverage");
    check(plainResult.plan.requirements[0].acceptance.length === 2, document.name + " final acceptance covers both behaviors");
    check(Object.values(plainAgents.counts).every(n => n === 2), document.name + " both independent reviewers repeat after coverage repair");
    const inputs = JSON.parse(readFileSync(join(plainResult.artifactDirectory, "inputs.json"), "utf8"));
    check(inputs.requirements === document.source && inputs.ids.join() === DOCUMENT_REQUIREMENT_ID && inputs.rules.length === 0, document.name + " original source and empty default rules persisted");
    check(readFileSync(join(plainCwd, "requirements.md"), "utf8") === document.source, document.name + " source never edited to add IDs");
    const requests = plainAgents.calls.filter(c => c.kind === "spawn" || c.kind === "queue");
    check(requests.every(c => c.instruction.includes(JSON.stringify(document.source)) && c.instruction.includes('"mode": "whole-document"')), document.name + " every task receives full original source and whole-document coverage rule");
    check(plainAgents.calls.filter(c => c.kind === "spawn").every(c => c.model.system.includes("including nested files")), document.name + " all worker prompts instruct applicable nested rule discovery");
    check(plainAgents.calls.filter(c => c.kind === "stop").length === 3, document.name + " workers cleaned after coverage loop");
    for (const rulesFiles of [["AGENTS.md"], ["missing-rules.md"]]) {
      const notStarted = approving();
      const error = await rejects(() => runWorkflow({...config, rulesFiles}, {runtime: {...plainRuntime, agents: notStarted.agents}}), document.name + " explicitly missing rule file fails");
      check(error.message.includes(rulesFiles[0]), "explicit missing rule failure identifies its path");
      check(notStarted.calls.length === 0, "missing explicit rules fail before agent work");
      check(JSON.parse(readFileSync(join(error.artifactDirectory, "outcome.json"), "utf8")).status === "failed", "explicit missing rules failure persisted");
    }
  }

  const referenceSource = "# Requirements\n- R1: Import record R42 unchanged.\n";
  const referenceCwd = await repository("body-reference", {requirements: referenceSource});
  for (const requirementIds of [undefined, ["R1"]]) {
    const referenceAgents = fakeAgents({
      planner: n => {
        const p = plan(n);
        p.requirements = [{id: "R1", acceptance: [{behavior: "Import record R42 unchanged.", usagePath: "Consumer imports legacy record R42"}], slices: ["S1"]}];
        p.slices[0].requirementIds = ["R1"];
        return p;
      },
      "plan-review-normal": n => review("normal", n),
      "plan-review-critical": n => review("critical", n),
    });
    const referenceRuntime = await createRuntime({cwd: referenceCwd, agents: referenceAgents.agents});
    const referenceResult = await runWorkflow({...config, requirementIds}, {runtime: referenceRuntime});
    check(referenceResult.status === "planned" && referenceResult.plan.requirements.map(r => r.id).join() === "R1", "R42 record example reaches planning without phantom requirement");
    const inputs = JSON.parse(readFileSync(join(referenceResult.artifactDirectory, "inputs.json"), "utf8"));
    check(inputs.ids.join() === "R1" && inputs.requirements === referenceSource, "automatic and explicit inventories preserve original reference source");
    const reviewerRequests = referenceAgents.calls.filter(c => c.kind === "spawn" && c.slot.includes("review"));
    check(reviewerRequests.length === 2 && reviewerRequests.every(c => c.instruction.includes(JSON.stringify(referenceSource)) && c.instruction.includes("Audit full original-source coverage")), "both reviewers audit complete source including R42 example");
  }

  const cwd = await repository("planning");
  const head = (await command(cwd, ["rev-parse", "HEAD"])).trim();
  const fake = fakeAgents({
    planner: n => plan(n),
    "plan-review-normal": n => review("normal", n, n === 1 ? [finding] : []),
    "plan-review-critical": n => review("critical", n, n === 2 ? [finding] : []),
  });
  // Copy just the portable folder's production modules and import from outside the checkout.
  const portable = join(root, "portable");
  mkdirSync(portable);
  const moduleDirectory = dirname(new URL(/** @type {{url: string}} */ (import.meta).url).pathname);
  for (const name of ["protocol.js", "runtime.js", "workflow.js"]) copyFileSync(join(moduleDirectory, name), join(portable, name));
  const copied = await import(join(portable, "workflow.js"));
  const runtime = await createRuntime({cwd, agents: fake.agents});
  const result = await copied.runWorkflow(config, {runtime});
  check(result.status === "planned" && result.plan.revision === 3, "both-review repair loop settles revision 3");
  check(Object.values(fake.counts).every(n => n === 3), "both reviewers repeat on every repaired revision");
  const spawns = fake.calls.filter(c => c.kind === "spawn");
  check(spawns.length === 3 && new Set(spawns.map(c => c.agentId)).size === 3, "planner and independent reviewer conversations retained");
  check(spawns.every(c => c.options.keepAlive === true && c.options.allowDelegation === false && c.model.allowDelegation === undefined), "authoritative spawn third options");
  check(spawns.filter(c => c.slot.includes("review")).every(c => c.model.effort === "xhigh"), "all reviews use Sol/xhigh");
  check(fake.calls.filter(c => c.kind === "stop").length === 3, "workers cleaned on success");
  for (let rev = 1; rev <= 3; rev++) {
    const normal = fake.calls.filter(c => c.slot === "plan-review-normal" && c.instruction)[rev - 1].instruction;
    const critical = fake.calls.filter(c => c.slot === "plan-review-critical" && c.instruction)[rev - 1].instruction;
    const extract = value => value.split("Plan revision:\n")[1].split("\nYour prior review:")[0];
    check(extract(normal) === extract(critical), "reviewers see same revision " + rev);
  }
  const files = readdirSync(result.artifactDirectory);
  check(files.includes("plan-3.json") && files.includes("review-2-critical.json") && files.includes("outcome.json"), "revision/review/final artifacts persisted");
  check(JSON.parse(readFileSync(join(result.artifactDirectory, "baseline.json"), "utf8")).untracked.includes("ignored.log"), "ignored baseline log protected too");
  check(result.baseline.untracked.includes("existing.log"), "initial untracked snapshot");
  check((await command(cwd, ["rev-parse", "HEAD"])).trim() === head, "planning made no commits");
  check((await command(cwd, ["status", "--porcelain"])).trim() === "?? existing.log", "unrelated logs preserved and index clean");
  check(readFileSync(join(cwd, "existing.log"), "utf8") === "keep me\n", "untracked content preserved");
  check((await command(cwd, ["ls-remote", "--heads", "origin"])).trim() === "", "planning never pushes (local bare remote)");
  const commandStore = createArtifactStore(result.baseline.artifactBase);
  const evidence = await commandStore.command(runtime, {argv: ["git", "not-a-command"], cwd: ".", purpose: "Capture real failure"}, "expected failure");
  check(evidence.exitCode !== 0 && evidence.stderr.length > 0, "real failed command captured as data");
  check(readdirSync(commandStore.directory).some(n => n.endsWith(".stderr")), "test output artifacts");

  for (const badAnswer of ['{"type":"review","version":1,"revision":1,"mode":"normal"}', "yes"]) {
    const bad = approving();
    const original = bad.agents.wait_task;
    bad.agents.wait_task = async args => {
      const out = await original(args);
      return bad.calls.find(c => c.kind === "spawn" && c.agentId === args.agentId)?.slot === "plan-review-normal" ? {...out, answer: badAnswer} : out;
    };
    const error = await rejects(() => runWorkflow(config, {runtime: {...runtime, agents: bad.agents}}), "malformed reviewer cannot approve", "invalid-protocol");
    check(JSON.parse(readFileSync(join(error.artifactDirectory, "outcome.json"), "utf8")).status === "failed", "malformed failure persisted");
    check(bad.calls.filter(c => c.kind === "stop").length === 3, "cleanup waits for all independent launches on malformed result");
  }
  for (const state of /** @type {const} */ (["failed", "cancelled"])) {
    const bad = approving();
    bad.agents.wait_task = async ({commandId}) => ({runId: "failed-run", commandId, state, answer: JSON.stringify(plan()), error: "task failure"});
    const error = await rejects(() => runWorkflow(config, {runtime: {...runtime, agents: bad.agents}}), "failed/cancelled task never approved", state === "failed" ? "agent-task-failed" : "cancelled");
    check(JSON.parse(readFileSync(join(error.artifactDirectory, "outcome.json"), "utf8")).status === (state === "failed" ? "failed" : "cancelled"), "failed/cancelled status persisted");
    check(bad.calls.filter(c => c.kind === "stop").length === 1, "failed planner cleaned");
  }
  const blocked = approving();
  blocked.agents.wait_task = async ({commandId}) => ({runId: "blocked", commandId, state: "completed", answer: JSON.stringify({type: "blocked", version: 1, reason: "Contradiction", evidence: "R1 conflicts with R2"})});
  await rejects(() => runWorkflow(config, {runtime: {...runtime, agents: blocked.agents}}), "genuine blocked result fails closed", "blocked");
  const cancellation = new Error("Host cancellation");
  const throwing = approving();
  throwing.agents.wait_task = async () => { throw cancellation; };
  check(await rejects(() => runWorkflow(config, {runtime: {...runtime, agents: throwing.agents}}), "wait rejection propagates") === cancellation, "cancellation identity preserved");

  writeFileSync(join(cwd, "AGENTS.md"), "tracked edit");
  await rejects(() => prepareRepository(runtime, cfg), "tracked edit refused", "git-preflight");
  await command(cwd, ["add", "--", "AGENTS.md"]);
  await rejects(() => prepareRepository(runtime, cfg), "index edit refused", "git-preflight");
  await command(cwd, ["reset", "--hard", "HEAD"]);
  await command(cwd, ["checkout", "--detach"]);
  await rejects(() => prepareRepository(runtime, cfg), "detached HEAD refused", "git-preflight");
  await command(cwd, ["checkout", "topic"]);
  await rejects(() => prepareRepository(runtime, validateConfig({...config, git: {...config.git, remote: "missing"}})), "unknown destination refused", "git");
  await rejects(() => prepareRepository(runtime, validateConfig({...config, git: {...config.git, branch: "wrong"}})), "wrong source branch refused", "git-preflight");
  await rejects(() => prepareRepository(runtime, validateConfig({...config, artifactsDir: "tracked-logs"})), "tracked artifact location refused", "artifacts");
  await rejects(() => runWorkflow(config, {runtime, stopAfter: "push"}), "future phases not falsely reported complete", "not-implemented");
  const linked = join(root, "linked");
  await command(cwd, ["worktree", "add", "-b", "linked", linked]);
  const linkedRuntime = await createRuntime({cwd: linked, agents: approving().agents});
  const linkedRepo = await prepareRepository(linkedRuntime, validateConfig({...config, git: {...config.git, branch: "linked"}}));
  check(linkedRepo.artifactBase.includes("/worktrees/") && !linkedRepo.artifactBase.startsWith(linked + "/"), "worktree-safe Git metadata artifacts");
  console.log("PASS workflow slice 1: " + checks + " focused checks, offline agents and real temporary Git.");
} finally { rmSync(root, {recursive: true, force: true}); }
