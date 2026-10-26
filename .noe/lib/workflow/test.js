// Focused offline check for launcher configuration, parent instructions, and task termination.
import {existsSync, mkdtempSync, readFileSync, rmSync, writeFileSync} from "node:fs";
import {join, resolve} from "node:path";
import process from "node:process";
import {fileURLToPath} from "node:url";
import {DEFAULT_DECISION, DEFAULT_ROLES, main, runCli} from "./workflow.js";

function assert(condition, message) {
  if (!condition) throw new Error("Assertion failed: " + message);
}

function assertPromptSettings(prompt, settings) {
  assert(prompt.includes(JSON.stringify(settings, null, 2)), "effective decision settings reach the parent prompt");
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
  // A role-only file keeps working and inherits the separate Jev defaults.
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
  assert(lastCapture().prompt.includes("Decision settings (explicit noe:decision provider/model only; separate from agent role settings and effort):"), "decision settings are clearly separated from role/effort settings");
  assertPromptSettings(lastCapture().prompt, {provider: "openrouter", model: "typesafe/jev-1.14"});
  assert(lastCapture().prompt.includes("Read applicable root and nested AGENTS.md"), "workflow-parent instructions are loaded");
  assert(lastCapture().options.allowDelegation === true, "the native root parent explicitly receives delegation permission");

  // Omitting --config honors a valid default-path config when present, or built-ins otherwise.
  calls = 0;
  const defaultConfigPath = resolve(process.cwd(), ".noe/workflow.json");
  const defaultPathSettings = existsSync(defaultConfigPath)
    ? JSON.parse(readFileSync(defaultConfigPath, "utf8"))
    : {};
  const ambientDecision = {...DEFAULT_DECISION, ...(defaultPathSettings.decision ?? {})};
  const ambientRoleOverrides = defaultPathSettings.roles ?? {};
  const ambientRoles = Object.fromEntries(
    Object.entries(DEFAULT_ROLES).map(([name, role]) => [
      name,
      {...role, ...(ambientRoleOverrides[name] ?? {})},
    ]),
  );
  const withoutConfig = await runCli(["--requirements", requirementsFile], {runAgent: stub});
  assert(withoutConfig === answer, "configuration remains optional");
  assertPromptSettings(lastCapture().prompt, ambientDecision);
  assert(
    lastCapture().prompt.includes(JSON.stringify(ambientRoles, null, 2)),
    "the no-config launch passes default-path role settings or built-in role defaults",
  );
  assert(
    JSON.stringify(lastCapture().config) === JSON.stringify(ambientRoles.parent),
    "the parent role reflects the ambient default-path config or built-in defaults",
  );

  // Partial overrides inherit the other decision default without affecting agent roles.
  await runWithConfig({decision: {provider: "custom-decision-provider"}}, "provider-only.json");
  assertPromptSettings(lastCapture().prompt, {provider: "custom-decision-provider", model: "typesafe/jev-1.14"});
  assert(lastCapture().config.model === "gpt-6.1-sol" && lastCapture().config.effort === "high", "decision provider override does not alter agent settings");

  await runWithConfig({decision: {model: "custom/decision-model"}}, "model-only.json");
  assertPromptSettings(lastCapture().prompt, {provider: "openrouter", model: "custom/decision-model"});
  assert(lastCapture().config.provider === "openai-chatgpt", "decision model override does not alter the agent provider");

  // Both decision values can be overridden independently of role overrides.
  await runWithConfig({
    decision: {provider: "custom-provider", model: "custom/model"},
    roles: {parent: {model: "custom-agent-model"}, routine: {effort: "low"}},
  }, "full-override.json");
  assertPromptSettings(lastCapture().prompt, {provider: "custom-provider", model: "custom/model"});
  assert(lastCapture().config.provider === "openai-chatgpt" && lastCapture().config.model === "custom-agent-model" && lastCapture().config.effort === "high", "agent-role override remains independent of decision settings");
  assert(lastCapture().prompt.includes('"effort": "low"'), "independent role effort is retained for agent assignment");

  // The example file documents the same supported decision fields and preserves the roles.
  const testModule = /** @type {{url: string}} */ (/** @type {any} */ (import.meta));
  const examplePath = fileURLToPath(new URL("./config.example.json", testModule.url));
  const example = JSON.parse(readFileSync(examplePath, "utf8"));
  assert(example.decision.provider === "openrouter" && example.decision.model === "typesafe/jev-1.14", "sample decision defaults are valid JSON");
  assert(example.roles.parent.effort === "high", "sample agent role settings remain present");

  // Invalid decision objects/fields (especially effort) fail before launching an agent.
  const invalidSettings = [
    {decision: null},
    {decision: []},
    {decision: {effort: "high"}},
    {decision: {threshold: 0.8}},
    {decision: {provider: ""}},
    {decision: {provider: "   "}},
    {decision: {model: ""}},
    {decision: {model: 7}},
  ];
  for (let index = 0; index < invalidSettings.length; index++) {
    const priorCalls = calls;
    let error;
    try {
      await runWithConfig(invalidSettings[index], "invalid-" + index + ".json");
    } catch (caught) {
      error = String(caught?.message ?? caught);
    }
    assert(typeof error === "string" && error.toLowerCase().includes("decision"), "invalid decision config is rejected (case " + index + "): " + String(error));
    assert(calls === priorCalls, "invalid decision config is rejected before run_agent");
  }
  let effortError;
  try {
    await runWithConfig({decision: {effort: "high"}}, "invalid-effort.json");
  } catch (caught) {
    effortError = String(caught?.message ?? caught);
  }
  assert(effortError?.includes("Unknown decision setting: effort"), "decision effort is explicitly rejected");

  // Ensure the parent prompt has all six next-stage boundaries and a native, evidence-based call.
  const gateHeadings = [
    "1. **Plan drafted -> independent plan reviews.**",
    "2. **Plan reviews resolved -> implementation.**",
    "3. **Slice implementation -> focused verification.**",
    "4. **Focused verification -> code review.**",
    "5. **Slice code review resolved -> local commit.**",
    "6. **Overall validation -> final delivery/push.**",
  ];
  let previous = -1;
  for (const heading of gateHeadings) {
    const index = lastCapture().prompt.indexOf(heading);
    assert(index > previous, "all six exact Jev boundaries appear in order: " + heading);
    previous = index;
  }
  assert(lastCapture().prompt.includes('import { decide } from "noe:decision";'), "instructions demonstrate the existing native decision API");
  assert(lastCapture().prompt.includes("provider: decisionSettings.provider") && lastCapture().prompt.includes("model: decisionSettings.model"), "the example uses effective decision settings explicitly");
  assert(lastCapture().prompt.includes("Pause before authorizing/running the planned focused verification"), "implementation pauses before the planned focused checks");
  for (const choice of ["proceed", "revise", "blocked"]) {
    assert(lastCapture().prompt.includes(choice), "the decision instructions explain " + choice);
  }
  assert(lastCapture().prompt.includes("actual current source/diff") && lastCapture().prompt.includes("commands and outcomes") && lastCapture().prompt.includes("not merely paths"), "gates require actual evidence, not file paths or completion claims");

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
  assert(help.includes("openrouter") && help.includes("typesafe/jev-1.14"), "help describes the default decision settings");
  assert(help.includes("offline") && help.includes("configured OpenRouter access") && help.includes("ChatGPT"), "help explains offline behavior and OpenRouter plus default ChatGPT access");
  assert(calls === beforeHelp, "help does not start a parent or read the supplied missing paths");

  console.log("workflow launcher offline checks passed");
} finally {
  process.exitCode = undefined;
  rmSync(workdir, {recursive: true, force: true});
}
