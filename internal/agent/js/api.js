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

export function createAgentAPI({ request, defaults }) {
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
      : options.contextBudget;
    const policy = options.contextPolicy === undefined
      ? inherited.contextPolicy
      : options.contextPolicy;
    const limits = options.executionLimits === undefined
      ? inherited.executionLimits
      : options.executionLimits;
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
      if (key !== "keepAlive" && key !== "allowDelegation") throw new TypeError("unknown agent option: " + key);
    }
    const keepAlive = options.keepAlive === undefined ? false : options.keepAlive;
    if (typeof keepAlive !== "boolean") throw new TypeError("keepAlive must be a boolean");
    if (options.allowDelegation !== undefined && typeof options.allowDelegation !== "boolean") {
      throw new TypeError("allowDelegation must be a boolean");
    }
    if (instruction !== undefined || !keepAlive) requireString(instruction, "instruction");
    return rpc("spawn_agent", { config: resolved, instruction, keepAlive, allowDelegation: options.allowDelegation });
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

  // Wait for one accepted task, independently of later queue work or idle state.
  async function wait_task(options = {}) {
    if (!isRecord(options)) throw new TypeError("task wait options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "agentId" && key !== "commandId" && key !== "includeAnswer") {
        throw new TypeError("unknown task wait option: " + key);
      }
    }
    if (options.includeAnswer !== undefined && typeof options.includeAnswer !== "boolean") {
      throw new TypeError("includeAnswer must be a boolean");
    }
    return rpc("wait_task", {
      agentId: requireString(options.agentId, "agentId"),
      commandId: requireString(options.commandId, "commandId"),
      includeAnswer: options.includeAnswer,
    });
  }

  // Wait as one request so a pending owner steer returns the entire set.
  async function wait_tasks(options = {}) {
    if (!isRecord(options)) throw new TypeError("tasks wait options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "tasks" && key !== "includeAnswer") {
        throw new TypeError("unknown tasks wait option: " + key);
      }
    }
    if (!Array.isArray(options.tasks) || options.tasks.length === 0) {
      throw new TypeError("tasks must be a nonempty array");
    }
    if (options.includeAnswer !== undefined && typeof options.includeAnswer !== "boolean") {
      throw new TypeError("includeAnswer must be a boolean");
    }
    const seen = new Set();
    const tasks = options.tasks.map((task) => {
      if (!isRecord(task)) throw new TypeError("task reference must be an object");
      for (const key of Object.keys(task)) {
        if (key !== "agentId" && key !== "commandId") throw new TypeError("unknown task reference option: " + key);
      }
      const ref = {
        agentId: requireString(task.agentId, "agentId"),
        commandId: requireString(task.commandId, "commandId"),
      };
      const key = JSON.stringify([ref.agentId, ref.commandId]);
      if (seen.has(key)) throw new TypeError("tasks must be unique");
      seen.add(key);
      return ref;
    });
    return rpc("wait_tasks", { tasks, includeAnswer: options.includeAnswer });
  }

  // Read the retained answer without consuming it or requesting another model.
  async function read_task_result(options = {}) {
    if (!isRecord(options)) throw new TypeError("task result options must be an object");
    for (const key of Object.keys(options)) {
      if (!["agentId", "commandId", "offset", "maxBytes"].includes(key)) {
        throw new TypeError("unknown task result option: " + key);
      }
    }
    const query = {
      agentId: requireString(options.agentId, "agentId"),
      commandId: requireString(options.commandId, "commandId"),
    };
    for (const key of ["offset", "maxBytes"]) {
      if (options[key] === undefined) continue;
      if (!Number.isSafeInteger(options[key]) || options[key] < (key === "offset" ? 0 : 1)) {
        throw new TypeError(key + " must be a " + (key === "offset" ? "nonnegative" : "positive") + " safe integer");
      }
      query[key] = options[key];
    }
    return rpc("read_task_result", query);
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

  // Cancel one accepted command without stopping its instance or sibling queue.
  async function cancel_task(options = {}) {
    if (!isRecord(options)) throw new TypeError("cancel task options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "agentId" && key !== "commandId") {
        throw new TypeError("unknown cancel task option: " + key);
      }
    }
    return rpc("cancel_task", {
      agentId: requireString(options.agentId, "agentId"),
      commandId: requireString(options.commandId, "commandId"),
    });
  }

  // Stop a managed instance, including its active task and remaining queue.
  async function stop_agent(options = {}) {
    if (!isRecord(options)) throw new TypeError("stop agent options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "agentId") throw new TypeError("unknown stop agent option: " + key);
    }
    return rpc("stop_agent", { agentId: requireString(options.agentId, "agentId") });
  }

  // Live management is scoped to one owned agent instance.
  async function observe_agent(options = {}) {
    if (!isRecord(options)) throw new TypeError("observation options must be an object");
    for (const key of Object.keys(options)) {
      if (key !== "agentId" && key !== "includeAnswer") throw new TypeError("live observation only accepts agentId and includeAnswer");
    }
    if (options.includeAnswer !== undefined && typeof options.includeAnswer !== "boolean") {
      throw new TypeError("includeAnswer must be a boolean");
    }
    return rpc("observe_agent", { agentId: requireString(options.agentId, "agentId"), includeAnswer: options.includeAnswer });
  }

  return Object.freeze({ run_agent, spawn_agent, observe_agent, queue_agent, wait_task, wait_tasks, read_task_result, steer_agent, cancel_task, stop_agent, list_models });
}
