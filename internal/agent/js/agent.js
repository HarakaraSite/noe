import { request, defaults, observe } from "noe:agent/internal";

const inherited = JSON.parse(defaults());

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}
function requireString(value, label, allowEmpty = false) {
  if (typeof value !== "string" || (!allowEmpty && !value.trim())) {
    throw new TypeError(label + " must be " + (allowEmpty ? "a string" : "a nonempty string"));
  }
  return value;
}
function requireLimit(value, label) {
  if (!Number.isInteger(value) || value < 0) throw new TypeError(label + " must be a nonnegative integer");
  return value;
}
function configFor(options) {
  if (!isRecord(options)) throw new TypeError("agent config must be an object");
  const provider = requireString(options.provider === undefined ? inherited.provider : options.provider, "provider");
  const model = requireString(options.model === undefined ? inherited.model : options.model, "model");
  const effort = options.effort === undefined ? inherited.effort : options.effort;
  if (!["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"].includes(effort)) {
    throw new TypeError("effort must be auto, none, minimal, low, medium, high, xhigh, or max");
  }
  const config = {
    provider, model, effort,
    name: options.name === undefined ? "agent" : requireString(options.name, "name"),
    system: options.system === undefined ? "" : requireString(options.system, "system", true),
    maxTokens: requireLimit(options.maxTokens === undefined ? (inherited.maxTokens || 0) : options.maxTokens, "maxTokens"),
    maxSteps: requireLimit(options.maxSteps === undefined ? 0 : options.maxSteps, "maxSteps"),
  };
  if (options.cacheKey !== undefined) config.cacheKey = requireString(options.cacheKey, "cacheKey", true);
  return config;
}
async function rpc(method, payload) {
  return JSON.parse(await request(method, JSON.stringify(payload)));
}

// Read the host's merged model catalog, without contacting a model provider.
export async function list_models(options = {}) {
  if (!isRecord(options)) throw new TypeError("model query must be an object");
  for (const key of Object.keys(options)) {
    if (key !== "provider" && key !== "kind") throw new TypeError("unknown model query option: " + key);
  }
  const query = {};
  if (options.provider !== undefined) query.provider = requireString(options.provider, "provider");
  if (options.kind !== undefined) {
    if (options.kind !== "agent" && options.kind !== "decision") throw new TypeError("kind must be agent or decision");
    query.kind = options.kind;
  }
  return rpc("list_models", query);
}

// Launch acceptance is independent of the first model request. A local host
// tracks the instance as native work; a run_js client hands ownership to its run.
export async function spawn_agent(config = {}, instruction, options = {}) {
  const resolved = configFor(config);
  if (!isRecord(options)) throw new TypeError("agent options must be an object");
  for (const key of Object.keys(options)) {
    if (key !== "keepAlive") throw new TypeError("unknown agent option: " + key);
  }
  const keepAlive = options.keepAlive === undefined ? false : options.keepAlive;
  if (typeof keepAlive !== "boolean") throw new TypeError("keepAlive must be a boolean");
  if (instruction !== undefined || !keepAlive) requireString(instruction, "instruction");
  return rpc("spawn_agent", { config: resolved, instruction, keepAlive });
}

// Waiting changes neither the launch contract nor the managed lifetime.
export async function run_agent(config = {}, instruction, options = {}) {
  const {agentId} = await spawn_agent(config, instruction, options);
  return rpc("wait_agent", {agentId});
}

export async function queue_agent(options = {}) {
  if (!isRecord(options)) throw new TypeError("queue options must be an object");
  const query = {
    agentId: requireString(options.agentId, "agentId"),
    instruction: requireString(options.instruction, "instruction"),
  };
  if (options.commandId !== undefined) query.commandId = requireString(options.commandId, "commandId");
  return rpc("queue_agent", query);
}

// Add an instruction to one active task without interrupting its current work.
export async function steer_agent(options = {}) {
  if (!isRecord(options)) throw new TypeError("steer options must be an object");
  for (const key of Object.keys(options)) {
    if (key !== "runId" && key !== "instruction" && key !== "commandId") {
      throw new TypeError("unknown steer option: " + key);
    }
  }
  const query = {
    runId: requireString(options.runId, "runId"),
    instruction: requireString(options.instruction, "instruction"),
  };
  if (options.commandId !== undefined) query.commandId = requireString(options.commandId, "commandId");
  return rpc("steer_agent", query);
}

// Cancel one managed task, or stop an entire managed instance and its queue.
export async function interrupt_agent(options = {}) {
  if (!isRecord(options)) throw new TypeError("interrupt options must be an object");
  for (const key of Object.keys(options)) {
    if (key !== "runId" && key !== "agentId") {
      throw new TypeError("unknown interrupt option: " + key);
    }
  }
  const hasRun = options.runId !== undefined;
  const hasAgent = options.agentId !== undefined;
  if (hasRun === hasAgent) throw new TypeError("specify exactly one of runId or agentId");
  if (hasRun) return rpc("interrupt_agent", {runId: requireString(options.runId, "runId")});
  return rpc("interrupt_agent", {agentId: requireString(options.agentId, "agentId")});
}

// Live management is scoped; saved-file observation needs no broker or model.
export async function observe_agent(options = {}) {
  if (!isRecord(options)) throw new TypeError("observation options must be an object");
  if (options.agentId !== undefined) {
    for (const key of Object.keys(options)) {
      if (key !== "agentId") throw new TypeError("live observation only accepts agentId");
    }
    return rpc("observe_agent", {agentId: requireString(options.agentId, "agentId")});
  }
  const query = {
    logPath: requireString(options.logPath, "logPath"),
    runId: requireString(options.runId, "runId"),
  };
  for (const key of ["wait", "includeThinking", "includeOutput"]) {
    if (options[key] !== undefined) {
      if (typeof options[key] !== "boolean") throw new TypeError(key + " must be a boolean");
      query[key] = options[key];
    }
  }
  if (options.cursor !== undefined) {
    if (!isRecord(options.cursor)) throw new TypeError("cursor must be an object");
    query.cursor = {
      executionId: requireString(options.cursor.executionId, "cursor.executionId", true),
      seq: requireLimit(options.cursor.seq, "cursor.seq"),
    };
  }
  return JSON.parse(await observe(JSON.stringify(query)));
}
