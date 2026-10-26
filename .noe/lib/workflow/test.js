// Focused offline check for launcher inputs, native handoff, and task termination handling.
import {existsSync, mkdtempSync, mkdirSync, rmSync, writeFileSync} from "node:fs";
import {join, relative, resolve} from "node:path";
import process from "node:process";
import {main, runCli} from "./workflow.js";

function assert(condition, message) {
  if (!condition) throw new Error("Assertion failed: " + message);
}

const workdir = mkdtempSync(resolve(process.cwd(), ".workflow-launcher-test-"));
const requirementText = "# Settled task\nImplement the requested behavior and verify it offline.\n";
const answer = "Implemented the settled behavior; focused checks passed.";
const requirementsFile = join(workdir, "requirements.md");
const configFile = join(workdir, "settings.json");
const requirementsArg = relative(process.cwd(), requirementsFile);
const configArg = relative(process.cwd(), configFile);
mkdirSync(workdir, {recursive: true});
writeFileSync(requirementsFile, requirementText);
writeFileSync(configFile, JSON.stringify({roles: {routine: {model: "offline-routine", effort: "low"}}}));

try {
  let calls = 0;
  const capture = {};
  const stub = async (config, prompt, options) => {
    calls++;
    capture.config = config;
    capture.prompt = prompt;
    capture.options = options;
    return {lastResult: {state: "completed", answer}};
  };
  const originalLog = console.log;
  let printed = "";
  console.log = text => { printed = String(text); };
  try {
    const returned = await main(["--requirements", requirementsArg, "--config", configArg], {runAgent: stub});
    assert(returned === answer && printed === answer, "the natural-language parent answer is returned and printed");
  } finally {
    console.log = originalLog;
  }

  assert(calls === 1, "one parent is launched");
  assert(capture.config.provider === "openai-chatgpt", "parent provider default is passed");
  assert(capture.config.model === "gpt-6.1-sol" && capture.config.effort === "high", "parent Sol/high defaults are passed");
  assert(capture.prompt.includes(requirementText), "requirements are read from the caller's working directory");
  assert(capture.prompt.includes(process.cwd()), "the actual launch working directory is supplied");
  assert(capture.prompt.includes("offline-routine") && capture.prompt.includes("gpt-6.1-sol"), "loaded role settings and defaults reach the parent");
  assert(capture.prompt.includes("Read applicable root and nested AGENTS.md"), "workflow-parent instructions are loaded");
  assert(capture.options.allowDelegation === true, "the native root parent explicitly receives delegation permission");

  const hasDefaultConfig = existsSync(resolve(process.cwd(), ".noe/workflow.json"));
  /** @type {{provider: string, model: string, effort: string} | undefined} */
  let defaultConfig;
  const withoutConfig = await runCli(["--requirements", requirementsArg], {
    runAgent: async config => {
      defaultConfig = config;
      return {lastResult: {state: "completed", answer}};
    },
  });
  assert(withoutConfig === answer, "configuration remains optional");
  if (defaultConfig === undefined) throw new Error("The optional-config path did not launch a parent.");
  if (!hasDefaultConfig) {
    assert(defaultConfig.model === "gpt-6.1-sol" && defaultConfig.effort === "high", "built-in defaults apply without a config file");
  }

  calls = 0;
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
      await main(["--requirements", requirementsArg, "--config", configArg], {runAgent: failedAgent});
    } finally {
      console.error = originalError;
    }
    assert(process.exitCode === 1, "native state " + String(state) + " becomes a nonzero CLI status");
    assert(reported.includes("did not complete"), "native state " + String(state) + " is surfaced as an error");
  }
  process.exitCode = priorExitCode;

  const beforeHelp = calls;
  const help = await runCli(["--help", "--requirements", "missing.md", "--config", "missing.json"], {
    runAgent: async () => { calls++; throw new Error("help called run_agent"); },
  });
  assert(help.includes("Usage:"), "help prints usage");
  assert(calls === beforeHelp, "help does not start a parent or read the supplied missing paths");

  console.log("workflow launcher offline checks passed");
} finally {
  process.exitCode = undefined;
  rmSync(workdir, {recursive: true, force: true});
}
