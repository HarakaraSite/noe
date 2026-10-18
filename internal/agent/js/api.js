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

function hookModules(value, label) {
  if (!Array.isArray(value)) throw new TypeError(label + " must be an array of module specifiers");
  return value.map((specifier, index) => requireString(specifier, label + "[" + index + "]"));
}

function contextBudget(value, label = "contextBudget") {
  if (value === null) return null;
  if (!isRecord(value)) throw new TypeError(label + " must be an object or null");
  for (const key of Object.keys(value)) {
    if (key !== "inputTokens") throw new TypeError("unknown " + label + " option: " + key);
  }
  if (!Number.isSafeInteger(value.inputTokens) || value.inputTokens <= 0) {
    throw new TypeError(label + ".inputTokens must be a positive safe integer");
  }
  return {inputTokens: value.inputTokens};
}

function contextPolicy(value, label = "contextPolicy") {
  if (value === null) return null;
  if (!isRecord(value)) throw new TypeError(label + " must be an object or null");
  for (const key of Object.keys(value)) {
    if (key !== "retainRatio" && key !== "responseReserveTokens") {
      throw new TypeError("unknown " + label + " option: " + key);
    }
  }
  const policy = {};
  if (value.retainRatio !== undefined) {
    if (typeof value.retainRatio !== "number" || !Number.isFinite(value.retainRatio) ||
        value.retainRatio <= 0 || value.retainRatio >= 1) {
      throw new TypeError(label + ".retainRatio must be finite and between 0 and 1");
    }
    policy.retainRatio = value.retainRatio;
  }
  if (value.responseReserveTokens !== undefined) {
    if (!Number.isSafeInteger(value.responseReserveTokens) || value.responseReserveTokens <= 0) {
      throw new TypeError(label + ".responseReserveTokens must be a positive safe integer");
    }
    policy.responseReserveTokens = value.responseReserveTokens;
  }
  return policy;
}

function executionLimits(value) {
  if (!isRecord(value)) throw new TypeError("executionLimits must be an object");
  const limits = {};
  for (const key of Object.keys(value)) {
    if (!["providerCalls", "agentStarts", "queuedTasks", "hookCalls"].includes(key)) {
      throw new TypeError("unknown executionLimits option: " + key);
    }
    if (!Number.isSafeInteger(value[key]) || value[key] < 0) {
      throw new TypeError("executionLimits." + key + " must be a nonnegative safe integer");
    }
    limits[key] = value[key];
  }
  return limits;
}

export function createAgentAPI({ request, defaults, observe }) {
  const inherited = JSON.parse(defaults());

  async function rpc(method, payload) {
    return JSON.parse(await request(method, JSON.stringify(payload)));
  }

  function configFor(options) {
    if (!isRecord(options)) throw new TypeError("agent config must be an object");
    const provider = requireString(options.provider === undefined ? inherited.provider : options.provider, "provider");
    const model = requireString(options.model === undefined ? inherited.model : options.model, "model");
    const effort = options.effort === undefined ? inherited.effort : options.effort;
    if (!["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"].includes(effort)) {
      throw new TypeError("effort must be auto, none, minimal, low, medium, high, xhigh, or max");
    }
    const hooks = options.hooks === undefined
      ? (inherited.hooks === undefined ? [] : hookModules(inherited.hooks, "inherited hooks"))
      : hookModules(options.hooks, "hooks");
    const budget = options.contextBudget === undefined
      ? inherited.contextBudget
      : contextBudget(options.contextBudget);
    const policy = options.contextPolicy === undefined
      ? inherited.contextPolicy
      : contextPolicy(options.contextPolicy);
    const limits = options.executionLimits === undefined
      ? inherited.executionLimits
      : executionLimits(options.executionLimits);
    const config = {
      provider, model, effort, hooks,
      name: options.name === undefined ? "agent" : requireString(options.name, "name"),
      system: options.system === undefined ? "" : requireString(options.system, "system", true),
      maxTokens: requireLimit(options.maxTokens === undefined ? (inherited.maxTokens || 0) : options.maxTokens, "maxTokens"),
      maxSteps: requireLimit(options.maxSteps === undefined ? 0 : options.maxSteps, "maxSteps"),
    };
    if (budget !== undefined) config.contextBudget = contextBudget(budget);
    if (policy !== undefined) config.contextPolicy = contextPolicy(policy);
    if (limits !== undefined) config.executionLimits = executionLimits(limits);
    if (options.cacheKey !== undefined) config.cacheKey = requireString(options.cacheKey, "cacheKey", true);
    return config;
  }

  // Read the host's merged model catalog, without contacting a model provider.
  async function list_models(options = {}) {
    if (!isRecord(options)) throw new TypeError("model query must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "provider" && key !== "kind") throw new TypeError("unknown model query option: " + key);
    }
    const query = {};
    if (options.provider !== undefined) query.provider = requireString(options.provider, "provider");
    if (options.kind !== undefined) {
      if (options.kind !== "agent" && options.kind !== "decision" && options.kind !== "image") throw new TypeError("kind must be agent, decision, or image");
      query.kind = options.kind;
    }
    return rpc("list_models", query);
  }

  // Launch acceptance is independent of the first model request. A local host
  // tracks the instance as native work; a run_js client hands ownership to its run.
  async function spawn_agent(config = {}, instruction, options = {}) {
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
  async function run_agent(config = {}, instruction, options = {}) {
    const { agentId } = await spawn_agent(config, instruction, options);
    return rpc("wait_agent", { agentId });
  }

  async function queue_agent(options = {}) {
    if (!isRecord(options)) throw new TypeError("queue options must be an object");
    const query = {
      agentId: requireString(options.agentId, "agentId"),
      instruction: requireString(options.instruction, "instruction"),
    };
    if (options.commandId !== undefined) query.commandId = requireString(options.commandId, "commandId");
    return rpc("queue_agent", query);
  }

  // Add an instruction to one active task without interrupting its current work.
  async function steer_agent(options = {}) {
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
  async function interrupt_agent(options = {}) {
    if (!isRecord(options)) throw new TypeError("interrupt options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "runId" && key !== "agentId") {
        throw new TypeError("unknown interrupt option: " + key);
      }
    }
    const hasRun = options.runId !== undefined;
    const hasAgent = options.agentId !== undefined;
    if (hasRun === hasAgent) throw new TypeError("specify exactly one of runId or agentId");
    if (hasRun) return rpc("interrupt_agent", { runId: requireString(options.runId, "runId") });
    return rpc("interrupt_agent", { agentId: requireString(options.agentId, "agentId") });
  }

  // Live management is scoped; saved-file observation needs no broker or model.
  async function observe_agent(options = {}) {
    if (!isRecord(options)) throw new TypeError("observation options must be an object");
    if (options.agentId !== undefined) {
      for (const key of Object.keys(options)) {
        if (key !== "agentId") throw new TypeError("live observation only accepts agentId");
      }
      return rpc("observe_agent", { agentId: requireString(options.agentId, "agentId") });
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

  return Object.freeze({ run_agent, spawn_agent, observe_agent, queue_agent, steer_agent, interrupt_agent, list_models });
}
