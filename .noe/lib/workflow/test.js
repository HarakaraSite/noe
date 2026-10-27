// Focused offline check for launcher configuration, parent instructions, and task termination.
import {existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import process from "node:process";
import {fileURLToPath} from "node:url";
import * as workflow from "./workflow.js";

const {DEFAULT_ROLES, main, runCli} = workflow;

function assert(condition, message) {
  if (!condition) throw new Error("Assertion failed: " + message);
}

const workdir = mkdtempSync(resolve(process.cwd(), ".workflow-launcher-test-"));
const requirementText = "# Settled task\nImplement the requested behavior and verify it offline.\n";
const answer = "Implemented the settled behavior; focused checks passed.";
const requirementsFile = join(workdir, "requirements.md");
writeFileSync(requirementsFile, requirementText);

let calls = 0;
/** @type {{config: {provider: string, model: string, effort: string}, prompt: string, options: {allowDelegation?: boolean}} | undefined} */
let capture;
function lastCapture() {
  if (capture === undefined) throw new Error("run_agent was not called");
  return capture;
}
/** @param {{provider: string, model: string, effort: string}} config
 * @param {string} prompt
 * @param {{allowDelegation?: boolean}} options
 */
const stub = async (config, prompt, options) => {
  calls++;
  capture = {config, prompt, options};
  return {lastResult: {state: "completed", answer}};
};

async function runWithConfig(settings, name = "settings.json") {
  const configFile = join(workdir, name);
  writeFileSync(configFile, JSON.stringify(settings));
  return runCli(["--requirements", requirementsFile, "--config", configFile], {runAgent: stub});
}

try {
  for (const name of ["routine", "difficult"]) {
    const role = DEFAULT_ROLES[name];
    assert(role.provider === "openai-chatgpt" && role.model === "gpt-6.1-terra" && role.effort === "high", name + " implementation/exploration defaults to Terra/high");
  }

  // Role overrides reach the parent without separate decision settings.
  const roleOnlyConfig = {
    roles: {routine: {model: "offline-routine", effort: "low"}},
  };
  const roleOnlyFile = join(workdir, "role-only.json");
  writeFileSync(roleOnlyFile, JSON.stringify(roleOnlyConfig));

  const originalLog = console.log;
  let printed = "";
  console.log = text => { printed = String(text); };
  try {
    const returned = await main(
      ["--requirements", requirementsFile, "--config", roleOnlyFile],
      {runAgent: stub},
    );
    assert(returned === answer && printed === answer, "the natural-language parent answer is returned and printed");
  } finally {
    console.log = originalLog;
  }

  assert(calls === 1, "one parent is launched");
  assert(lastCapture().config.provider === "openai-chatgpt", "parent provider default is passed");
  assert(lastCapture().config.model === "gpt-6.1-sol" && lastCapture().config.effort === "high", "parent Sol/high defaults are passed");
  assert(lastCapture().prompt.includes(requirementText), "requirements are read from the caller's working directory");
  assert(lastCapture().prompt.includes(process.cwd()), "the actual launch working directory is supplied");
  assert(lastCapture().prompt.includes("offline-routine") && lastCapture().prompt.includes("gpt-6.1-sol"), "loaded role settings and defaults reach the parent");
  assert(lastCapture().prompt.includes(JSON.stringify({...DEFAULT_ROLES, routine: {...DEFAULT_ROLES.routine, ...roleOnlyConfig.roles.routine}}, null, 2)), "effective role overrides reach the parent");
  assert(!("DEFAULT_DECISION" in workflow), "experimental decision defaults are no longer exported");
  assert(lastCapture().prompt.includes("Read applicable root and nested AGENTS.md"), "workflow-parent instructions are loaded");
  assert(lastCapture().options.allowDelegation === true, "the native root parent explicitly receives delegation permission");

  // Omitting --config honors a valid default-path config when present, or built-ins otherwise.
  calls = 0;
  const defaultConfigPath = resolve(process.cwd(), ".noe/workflow.json");
  const defaultPathSettings = existsSync(defaultConfigPath)
    ? JSON.parse(readFileSync(defaultConfigPath, "utf8"))
    : {};
  const ambientRoleOverrides = defaultPathSettings.roles ?? {};
  const ambientRoles = Object.fromEntries(
    Object.entries(DEFAULT_ROLES).map(([name, role]) => [
      name,
      {...role, ...(ambientRoleOverrides[name] ?? {})},
    ]),
  );
  const withoutConfig = await runCli(["--requirements", requirementsFile], {runAgent: stub});
  assert(withoutConfig === answer, "configuration remains optional");
  assert(
    lastCapture().prompt.includes(JSON.stringify(ambientRoles, null, 2)),
    "the no-config launch passes default-path role settings or built-in role defaults",
  );
  assert(
    JSON.stringify(lastCapture().config) === JSON.stringify(ambientRoles.parent),
    "the parent role reflects the ambient default-path config or built-in defaults",
  );

  // Empty and partial role configs inherit agent defaults.
  await runWithConfig({}, "empty.json");
  assert(lastCapture().prompt.includes(JSON.stringify(DEFAULT_ROLES, null, 2)), "empty configuration passes built-in role defaults");
  await runWithConfig({
    roles: {parent: {model: "custom-agent-model"}, routine: {effort: "low"}},
  }, "role-overrides.json");
  assert(lastCapture().config.provider === "openai-chatgpt" && lastCapture().config.model === "custom-agent-model" && lastCapture().config.effort === "high", "partial parent override inherits provider and effort");
  assert(lastCapture().prompt.includes('"effort": "low"'), "independent role effort override is retained");

  // The copyable example contains only supported role settings.
  const testModule = /** @type {{url: string}} */ (/** @type {any} */ (import.meta));
  const examplePath = fileURLToPath(new URL("./config.example.json", testModule.url));
  const example = JSON.parse(readFileSync(examplePath, "utf8"));
  assert(Object.keys(example).length === 1 && example.roles, "example configuration contains only roles");
  assert(example.roles.parent.effort === "high", "sample parent role remains present");
  for (const name of ["routine", "difficult"]) {
    assert(JSON.stringify(example.roles[name]) === JSON.stringify(DEFAULT_ROLES[name]), name + " sample matches the Terra/high default");
  }
  await runWithConfig(example, "example.json");
  assert(lastCapture().prompt.includes(JSON.stringify(example.roles, null, 2)), "sample role-only config launches correctly");

  // Removed experimental settings and invalid role settings fail before agent launch.
  const invalidSettings = [
    {settings: {decision: {provider: "openrouter", model: "typesafe/jev-1.14"}}, expected: "Unknown workflow setting: decision"},
    {settings: {decision: null}, expected: "Unknown workflow setting: decision"},
    {settings: {extra: true}, expected: "Unknown workflow setting: extra"},
    {settings: {roles: {unknown: {effort: "high"}}}, expected: "Unknown workflow role: unknown"},
    {settings: {roles: {routine: {unknown: "value"}}}, expected: "Unknown setting for role routine: unknown"},
    {settings: {roles: {routine: {effort: ""}}}, expected: "must be a nonempty string"},
  ];
  for (let index = 0; index < invalidSettings.length; index++) {
    const priorCalls = calls;
    let error;
    try {
      await runWithConfig(invalidSettings[index].settings, "invalid-" + index + ".json");
    } catch (caught) {
      error = String(caught?.message ?? caught);
    }
    assert(typeof error === "string" && error.includes(invalidSettings[index].expected), "unsupported configuration is rejected (case " + index + "): " + String(error));
    assert(calls === priorCalls, "unsupported configuration fails before run_agent");
  }

  // The supplied procedure has no decision presets and keeps implementation/testing continuous.
  const parentPrompt = lastCapture().prompt;
  assert(parentPrompt.includes("There are no mandatory model-based approval gates."), "normal workflow progress has no model approval dependency");
  assert(parentPrompt.includes("concrete unresolved judgment") && parentPrompt.includes("whether an extra opinion can be skipped"), "another opinion is not a replacement approval gate");
  assert(parentPrompt.includes("does not create a new mandatory gate"), "unavailable advice is not an approval dependency");
  for (const removed of ["typesafe/jev-1.14", "Optional consultation settings", "decision.provider", "decision.model"]) {
    assert(!parentPrompt.includes(removed), "removed decision preset is absent from the parent prompt: " + removed);
  }
  assert(parentPrompt.includes("implement and run the planned focused checks in one continuous loop"), "implementation owners can execute focused checks without a separate authorization");
  assert(parentPrompt.includes("including after repairs") && parentPrompt.includes("Repairs do not need a separate test-authorization handoff."), "repair verification stays continuous");
  assert(parentPrompt.includes("Avoid rerunning the same valid checks solely because the work passed from child to parent."), "handoff alone does not cause duplicate checks");
  assert(parentPrompt.includes("Reuse existing evidence rather than repackaging requirements, full diffs and test output"), "record keeping reuses evidence instead of repeated approval packaging");
  for (const obsolete of [
    "The six next-stage boundaries",
    "Pause before authorizing/running the planned focused verification",
    "explicitly withhold the planned focused verification",
    "every required parent-side noe:decision.decide call",
    "pass through readiness boundary",
  ]) {
    assert(!parentPrompt.includes(obsolete), "obsolete gate/test-pause instruction is absent: " + obsolete);
  }

  // Removing decision gates must not remove independent review, slice commits or delivery checks.
  for (const retained of [
    "independent, read-only normal and critical reviewers",
    "Use an independent code reviewer",
    "one modifying implementation child at a time",
    "Do not commit while a valid finding remains unresolved.",
    "Stage only intended paths/changes",
    "Do not push per slice.",
    "After all slices, verify the full requirements and cross-slice user paths.",
    "Push once using normal Git commands only after requirements, validation and review are satisfied",
    "Never release, create tags, force push, rewrite history",
    "report that delivery is incomplete",
  ]) {
    assert(parentPrompt.includes(retained), "parent procedure retains safeguard: " + retained);
  }
  const outerPath = fileURLToPath(new URL("./agent-instruction.md", testModule.url));
  const outerInstructions = readFileSync(outerPath, "utf8");
  assert(outerInstructions.includes("no mandatory model-based approval gates or separate decision-provider credential requirement"), "outer requester guidance matches ordinary agent-only access");
  assert(outerInstructions.includes("without a parent approval pause before testing"), "outer worker guidance permits continuous testing");
  assert(outerInstructions.includes("Keep the owning command/tool alive until it completes."), "outer guidance still awaits one standalone subprocess");

  // Native non-completed outcomes remain errors and are not mistaken for successful delivery.
  const originalError = console.error;
  const priorExitCode = process.exitCode;
  for (const state of ["failed", "cancelled", "running", undefined]) {
    let reported = "";
    console.error = text => { reported = String(text); };
    process.exitCode = undefined;
    const failedAgent = async () => ({
      lastResult: state === undefined ? undefined : {state, error: "offline native outcome"},
    });
    try {
      await main(["--requirements", requirementsFile, "--config", roleOnlyFile], {runAgent: failedAgent});
    } finally {
      console.error = originalError;
    }
    assert(process.exitCode === 1, "native state " + String(state) + " becomes a nonzero CLI status");
    assert(reported.includes("did not complete"), "native state " + String(state) + " is surfaced as an error");
  }
  process.exitCode = priorExitCode;

  // Help is offline: it neither reads the missing paths nor launches an agent.
  const beforeHelp = calls;
  const help = await runCli(["--help", "--requirements", "missing.md", "--config", "missing.json"], {
    runAgent: async () => { calls++; throw new Error("help called run_agent"); },
  });
  assert(help.includes("Usage:"), "help prints usage");
  assert(help.includes("role overrides only") && help.includes("no workflow-specific decision settings"), "help documents role-only configuration");
  assert(!help.includes("openrouter") && !help.includes("typesafe/jev-1.14"), "help has no decision provider/model defaults");
  assert(help.includes("offline") && help.includes("configured agent-provider access") && help.includes("ChatGPT"), "help explains offline help and ordinary agent access");
  assert(calls === beforeHelp, "help does not start a parent or read the supplied missing paths");

  console.log("workflow launcher offline checks passed");
} finally {
  process.exitCode = undefined;
  rmSync(workdir, {recursive: true, force: true});
}
