// Thin launcher: filesystem context and role settings go to one native workflow parent.
import {existsSync, readFileSync} from "node:fs";
import {dirname, resolve} from "node:path";
import process from "node:process";
import {fileURLToPath} from "node:url";
import {run_agent} from "noe:agent";

export const DEFAULT_DECISION = {provider: "openrouter", model: "typesafe/jev-1.14"};

export const DEFAULT_ROLES = {
  parent: {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high"},
  general: {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high"},
  planning: {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high"},
  review: {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "xhigh"},
  routine: {provider: "openai-chatgpt", model: "gpt-6-luna", effort: "max"},
  difficult: {provider: "openai-chatgpt", model: "gpt-6.1-sol", effort: "high"},
};

const moduleMeta = /** @type {{url: string}} */ (/** @type {any} */ (import.meta));
const PACKAGE_DIR = dirname(fileURLToPath(moduleMeta.url));
const INSTRUCTIONS_PATH = resolve(PACKAGE_DIR, "workflow.md");
const DEFAULT_CONFIG_PATH = ".noe/workflow.json";
const ROLE_FIELDS = new Set(["provider", "model", "effort"]);
const DECISION_FIELDS = new Set(["provider", "model"]);

export const HELP = [
  "Usage:",
  "  noe run /path/to/workflow/run.js --requirements PATH [--config PATH]",
  "  noe run /path/to/workflow/run.js --help",
  "",
  "Run from the target repository root. Requirements and config paths are relative to the current working directory.",
  "If --config is omitted, .noe/workflow.json is loaded when present; otherwise built-in model defaults are used.",
  "The optional decision settings default to openrouter / typesafe/jev-1.14; normal execution makes OpenRouter decision calls as well as ChatGPT agent calls by default.",
  "Help is offline and does not require provider credentials. Normal execution requires configured OpenRouter access and configured agent-provider access (ChatGPT by default).",
  "The workflow parent owns planning, delegated work, Git decisions, and the final natural-language report.",
  "There is no --repo option or automatic resume.",
].join("\n");

export function parseArgs(argv) {
  const seen = new Set();
  let requirementsPath;
  let configPath;
  let help = false;

  for (let index = 0; index < argv.length; index++) {
    const flag = argv[index];
    if (flag === "--help") {
      if (seen.has(flag)) throw new Error("Duplicate argument: " + flag);
      seen.add(flag);
      help = true;
      continue;
    }
    if (flag !== "--requirements" && flag !== "--config") {
      throw new Error("Unknown argument: " + flag);
    }
    if (seen.has(flag)) throw new Error("Duplicate argument: " + flag);
    seen.add(flag);
    const value = argv[++index];
    if (value === undefined || value.length === 0 || value.startsWith("--")) {
      throw new Error("Missing value for " + flag);
    }
    if (flag === "--requirements") requirementsPath = value;
    else configPath = value;
  }

  if (!help && requirementsPath === undefined) {
    throw new Error("Missing required --requirements PATH");
  }
  return {help, requirementsPath, configPath};
}

function defaultRoles() {
  return Object.fromEntries(Object.entries(DEFAULT_ROLES).map(([name, role]) => [name, {...role}]));
}

function readSettings(configPath, cwd) {
  const resolvedConfig = resolve(cwd, configPath ?? DEFAULT_CONFIG_PATH);
  if (configPath === undefined && !existsSync(resolvedConfig)) {
    return {roles: defaultRoles(), decision: {...DEFAULT_DECISION}, source: null};
  }

  let settings;
  try {
    settings = JSON.parse(readFileSync(resolvedConfig, "utf8"));
  } catch (error) {
    throw new Error("Cannot read workflow settings " + resolvedConfig + ": " + String(error?.message ?? error));
  }
  if (settings === null || typeof settings !== "object" || Array.isArray(settings)) {
    throw new Error("Workflow settings must be an object with optional role overrides and decision settings.");
  }
  const overrides = settings.roles ?? {};
  if (overrides === null || typeof overrides !== "object" || Array.isArray(overrides)) {
    throw new Error("Workflow settings roles must be an object.");
  }

  const roles = defaultRoles();
  for (const [name, override] of Object.entries(overrides)) {
    if (!Object.prototype.hasOwnProperty.call(roles, name)) {
      throw new Error("Unknown workflow role: " + name);
    }
    if (override === null || typeof override !== "object" || Array.isArray(override)) {
      throw new Error("Role override for " + name + " must be an object.");
    }
    for (const [field, value] of Object.entries(override)) {
      if (!ROLE_FIELDS.has(field)) throw new Error("Unknown setting for role " + name + ": " + field);
      if (typeof value !== "string" || value.trim().length === 0) {
        throw new Error("Role setting " + name + "." + field + " must be a nonempty string.");
      }
    }
    roles[name] = {...roles[name], ...override};
  }

  const decision = {...DEFAULT_DECISION};
  if (Object.prototype.hasOwnProperty.call(settings, "decision")) {
    const override = settings.decision;
    if (override === null || typeof override !== "object" || Array.isArray(override)) {
      throw new Error("Workflow settings decision must be an object with optional provider and model overrides.");
    }
    for (const [field, value] of Object.entries(override)) {
      if (!DECISION_FIELDS.has(field)) throw new Error("Unknown decision setting: " + field);
      if (typeof value !== "string" || value.trim().length === 0) {
        throw new Error("Decision setting " + field + " must be a nonempty string.");
      }
    }
    Object.assign(decision, override);
  }
  return {roles, decision, source: resolvedConfig};
}

function buildPrompt({cwd, requirementsPath, requirementsFile, requirements, settings, instructions}) {
  return [
    "You are the dedicated workflow parent for a settled requirements task. The launcher/requester is not a second coordinator; you own planning, judgment, workers, validation, Git and the completion report.",
    "Actual target repository working directory: " + cwd,
    "Requirements path supplied relative to that working directory: " + requirementsPath,
    "Resolved requirements file: " + requirementsFile,
    "The launcher has read the source below. Re-read the file and confirm it remains unchanged before planning; treat the original file and applicable repository work rules as authoritative.",
    "----- BEGIN SETTLED REQUIREMENTS -----",
    requirements,
    "----- END SETTLED REQUIREMENTS -----",
    "Role settings source: " + (settings.source ?? "built-in defaults (no config file present)"),
    "Role settings (ordinary agent provider/model/effort values):",
    JSON.stringify(settings.roles, null, 2),
    "Decision settings (explicit noe:decision provider/model only; separate from agent role settings and effort):",
    JSON.stringify(settings.decision, null, 2),
    "Use these effective decision settings explicitly in every required parent-side noe:decision.decide call.",
    "Follow the complete fixed procedure below. It defines responsibilities and judgment criteria, not a machine-enforced decision protocol.",
    "----- BEGIN WORKFLOW PARENT INSTRUCTIONS -----",
    instructions,
    "----- END WORKFLOW PARENT INSTRUCTIONS -----",
  ].join("\n\n");
}

/** Launch one parent and return its natural-language answer; tests may replace only run_agent. */
export async function launch({requirementsPath, configPath}, {runAgent = run_agent} = {}) {
  if (typeof requirementsPath !== "string" || requirementsPath.length === 0) {
    throw new Error("A requirements path is required.");
  }
  const cwd = process.cwd();
  const requirementsFile = resolve(cwd, requirementsPath);
  const requirements = readFileSync(requirementsFile, "utf8");
  const settings = readSettings(configPath, cwd);
  const instructions = readFileSync(INSTRUCTIONS_PATH, "utf8");
  const prompt = buildPrompt({cwd, requirementsPath, requirementsFile, requirements, settings, instructions});
  const parentConfig = /** @type {Parameters<typeof run_agent>[0]} */ (settings.roles.parent);
  const snapshot = await runAgent(parentConfig, prompt, {allowDelegation: true});
  const outcome = snapshot?.lastResult;

  if (!outcome || outcome.state !== "completed") {
    const state = outcome?.state ?? "no task outcome";
    const detail = outcome?.error ? ": " + outcome.error : "";
    throw new Error("Workflow parent did not complete (state: " + state + ")" + detail);
  }
  if (typeof outcome.answer !== "string") {
    throw new Error("Workflow parent completed without an answer.");
  }
  return outcome.answer;
}

export async function runCli(argv, dependencies = {}) {
  const args = parseArgs(argv);
  if (args.help) return HELP;
  return launch(args, dependencies);
}

export async function main(argv = process.argv.slice(2), dependencies = {}) {
  try {
    const answer = await runCli(argv, dependencies);
    console.log(answer);
    return answer;
  } catch (error) {
    console.error("workflow: " + String(error?.message ?? error));
    process.exitCode = 1;
    return undefined;
  }
}
