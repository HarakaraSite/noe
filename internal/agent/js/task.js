import { request, defaults, instructions } from "noe:agent/internal";
import { callHooks, hookErrorText, loadHookModules } from "noe:agent/hooks";
import { TaskHistory } from "noe:agent/history";

const inherited = JSON.parse(defaults());
const systemInstruction = instructions();

const runJSTool = Object.freeze({
  type: "function",
  function: {
    name: "run_js",
    description: "Execute JavaScript in a fresh NOE process and return stdout, stderr, exitCode, and reason.",
    parameters: {
      type: "object",
      properties: {
        code: { type: "string", description: "An ES module containing the JavaScript to execute. The first line must be a short single-line // comment in English describing the action, before any imports or code, with no leading blank line." },
      },
      required: ["code"],
      additionalProperties: false,
    },
  },
});

async function rpc(method, payload) {
  return JSON.parse(await request(method, JSON.stringify(payload)));
}

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
  if (!Number.isInteger(value) || value < 0) {
    throw new TypeError(label + " must be a nonnegative integer");
  }
  return value;
}

function requireEffort(value) {
  if (!["auto", "none", "minimal", "low", "medium", "high", "xhigh", "max"].includes(value)) {
    throw new TypeError("effort must be auto, none, minimal, low, medium, high, xhigh, or max");
  }
  return value;
}

function requireHooks(value) {
  if (!Array.isArray(value)) throw new TypeError("hooks must be an array of module specifiers");
  return value.map((specifier, index) => requireString(specifier, "hooks[" + index + "]"));
}

function requireContextBudget(value) {
  if (value === null) return null;
  if (!isRecord(value)) throw new TypeError("contextBudget must be an object or null");
  for (const key of Object.keys(value)) {
    if (key !== "inputTokens") throw new TypeError("unknown contextBudget option: " + key);
  }
  if (!Number.isSafeInteger(value.inputTokens) || value.inputTokens <= 0) {
    throw new TypeError("contextBudget.inputTokens must be a positive safe integer");
  }
  return {inputTokens: value.inputTokens};
}

function requireContextPolicy(value) {
  if (value === null) return null;
  if (!isRecord(value)) throw new TypeError("contextPolicy must be an object or null");
  for (const key of Object.keys(value)) {
    if (key !== "retainRatio" && key !== "responseReserveTokens") {
      throw new TypeError("unknown contextPolicy option: " + key);
    }
  }
  const policy = {};
  if (value.retainRatio !== undefined) {
    if (typeof value.retainRatio !== "number" || !Number.isFinite(value.retainRatio) ||
        value.retainRatio <= 0 || value.retainRatio >= 1) {
      throw new TypeError("contextPolicy.retainRatio must be finite and between 0 and 1");
    }
    policy.retainRatio = value.retainRatio;
  }
  if (value.responseReserveTokens !== undefined) {
    if (!Number.isSafeInteger(value.responseReserveTokens) || value.responseReserveTokens <= 0) {
      throw new TypeError("contextPolicy.responseReserveTokens must be a positive safe integer");
    }
    policy.responseReserveTokens = value.responseReserveTokens;
  }
  return policy;
}

function failedToolResult(message) {
  return { stdout: "", stderr: message, exitCode: -1, reason: "failed" };
}

function requireRunnerResult(value) {
  if (!isRecord(value) || typeof value.stdout !== "string" || typeof value.stderr !== "string" ||
      !Number.isInteger(value.exitCode) || !["completed", "failed", "timeout", "cancelled"].includes(value.reason)) {
    throw new Error("runner returned an invalid tool result");
  }
  return value;
}

async function executeToolCall(runId, modelStepId, toolCall) {
  if (!isRecord(toolCall.function)) {
    return failedToolResult("tool call has no function object");
  }
  const fn = toolCall.function;
  if (typeof fn.name !== "string" || !fn.name.trim()) {
    return failedToolResult("tool call function name must be a nonempty string");
  }
  if (fn.name !== "run_js") {
    return failedToolResult("unknown tool: " + fn.name);
  }
  if (typeof fn.arguments !== "string") {
    return failedToolResult("run_js arguments must be a JSON string");
  }

  let args;
  try {
    args = JSON.parse(fn.arguments);
  } catch (_) {
    return failedToolResult("run_js arguments must contain valid JSON");
  }
  if (!isRecord(args)) {
    return failedToolResult("run_js arguments must be an object");
  }
  if (typeof args.code !== "string") {
    return failedToolResult("run_js code must be a string");
  }

  return requireRunnerResult(await rpc("run_js", { runId, modelStepId, toolCallId: toolCall.id, code: args.code }));
}

function parsedToolArguments(call) {
  if (typeof call.function.arguments !== "string") return null;
  try { return JSON.parse(call.function.arguments); } catch (_) { return null; }
}

// The wrapper is independent of the tool implementation. The model's full
// original call remains in history; effective arguments and results are
// recorded separately from actual execution facts.
export async function executeToolWithHooks(loaded, event, toolCall, execute) {
  const fn = toolCall.function;
  const toolName = typeof fn.name === "string" && fn.name.trim() ? fn.name : "<invalid>";
  const originalArguments = fn.arguments === undefined ? null : fn.arguments;
  const before = await callHooks(loaded, "beforeTool", {
    ...event, toolCallId: toolCall.id, toolName,
    rawArguments: originalArguments, arguments: parsedToolArguments(toolCall),
  }, args => {
    if (!isRecord(args)) throw new TypeError("tool arguments must be an object");
    if (fn.name === "run_js" && (typeof args.code !== "string" || Object.keys(args).some(key => key !== "code"))) {
      throw new TypeError("run_js hook arguments must contain only code: string");
    }
  });
  const effectiveCall = before.changed ? {
    ...toolCall, function: {...fn, arguments: JSON.stringify(before.event.arguments)},
  } : toolCall;
  const rawResult = await execute(effectiveCall);
  const report = {
    runId: event.runId, modelStepId: event.modelStepId,
    toolCallId: toolCall.id, toolName,
    rawArguments: originalArguments, arguments: before.event.arguments,
    rawResult,
  };
  let after;
  try {
    after = await callHooks(loaded, "afterTool", {
      ...event, toolCallId: toolCall.id, toolName,
      rawArguments: originalArguments, arguments: before.event.arguments, result: rawResult,
    });
  } catch (error) {
    try {
      await rpc("tool_result", {...report, result: null, modelResultAvailable: false, hookError: hookErrorText(error)});
    } catch (_) {
      // Cancellation may already have closed admission. Keep the hook error.
    }
    throw error;
  }
  const result = after.changed ? after.event.result : rawResult;
  await rpc("tool_result", {...report, result, modelResultAvailable: true});
  return result;
}

function validateToolCalls(message) {
  const seenIDs = Object.create(null);
  const calls = message.tool_calls;
  if (calls === undefined || calls === null) return [];
  if (!Array.isArray(calls)) throw new Error("model returned malformed tool calls");

  for (const call of calls) {
    if (!isRecord(call) || typeof call.id !== "string" || !call.id.trim()) {
      throw new Error("model returned a tool call with an invalid ID");
    }
    if (seenIDs[call.id]) throw new Error("model returned a duplicate tool call ID");
    seenIDs[call.id] = true;
    if (call.type !== "function" || !isRecord(call.function)) {
      throw new Error("model returned a malformed function call");
    }
  }
  return calls;
}

function finalAnswer(content) {
  if (typeof content === "string") return content;
  if (Array.isArray(content)) {
    return content.filter((part) => isRecord(part) && part.type === "text" && typeof part.text === "string")
      .map((part) => part.text).join("\n");
  }
  return "";
}

function normalizeConfig(config) {
  if (!isRecord(config)) throw new TypeError("task config must be an object");
  const provider = requireString(config.provider === undefined ? inherited.provider : config.provider, "provider");
  const model = requireString(config.model === undefined ? inherited.model : config.model, "model");
  const name = config.name === undefined ? "agent" : requireString(config.name, "name");
  const system = config.system === undefined ? "" : requireString(config.system, "system", true);
  const effort = requireEffort(config.effort === undefined ? inherited.effort : config.effort);
  const maxTokens = config.maxTokens === undefined ? (inherited.maxTokens || 0) : requireLimit(config.maxTokens, "maxTokens");
  const maxSteps = config.maxSteps === undefined ? 0 : requireLimit(config.maxSteps, "maxSteps");
  const cacheKey = config.cacheKey === undefined ? undefined : requireString(config.cacheKey, "cacheKey", true);
  const hooks = config.hooks === undefined
    ? (inherited.hooks === undefined ? [] : requireHooks(inherited.hooks))
    : requireHooks(config.hooks);
  const contextBudget = config.contextBudget === undefined
    ? inherited.contextBudget
    : requireContextBudget(config.contextBudget);
  const contextPolicy = config.contextPolicy === undefined
    ? inherited.contextPolicy
    : requireContextPolicy(config.contextPolicy);
  return {
    provider, model, name, system, effort, maxTokens, maxSteps, cacheKey, hooks,
    contextBudget: contextBudget === undefined ? undefined : requireContextBudget(contextBudget),
    contextPolicy: contextPolicy === undefined ? {retainRatio: 0.1} : requireContextPolicy(contextPolicy),
  };
}

function validatedSteers(value) {
  if (!Array.isArray(value) || value.length === 0) throw new Error("agent broker returned an invalid steer batch");
  const seen = Object.create(null);
  return value.map((item) => {
    if (!isRecord(item) || typeof item.commandId !== "string" || !item.commandId.trim() ||
        typeof item.instruction !== "string" || !item.instruction.trim() || seen[item.commandId]) {
      throw new Error("agent broker returned an invalid steer command");
    }
    seen[item.commandId] = true;
    return item;
  });
}

async function appendAndApplySteers(runId, history, value) {
  const steers = validatedSteers(value);
  for (const steer of steers) history.appendSteer(steer);
  await rpc("apply_steers", { runId, commandIds: steers.map((steer) => steer.commandId), snapshot: history.snapshot() });
}

// This internal entry point is used by the managed-task bootstrap. It keeps the
// model/tool loop in this module and reports every started run through finish.
export async function executeTask(config, instruction) {
  const normalized = normalizeConfig(config);
  requireString(instruction, "instruction");

  const { provider, model, name, system, effort, maxTokens, maxSteps } = normalized;
  const start = await rpc("start", {
    provider, model, name, maxTokens, effort, maxSteps, hooks: normalized.hooks,
    contextBudget: normalized.contextBudget, contextPolicy: normalized.contextPolicy,
  });
  const runId = start.runId;
  if (typeof runId !== "string" || !runId) throw new Error("agent broker returned an invalid run ID");
  const managedConversation = start.snapshot !== undefined;
  const cacheKey = normalized.cacheKey === undefined ? runId : normalized.cacheKey;
  let status = "failed";
  let acceptedAnswer = "";
  let failureMessage = "";
  try {
    if (typeof start.instructionError === "string" && start.instructionError) {
      throw new Error(start.instructionError);
    }
    const userInstructions = typeof start.instructions === "string" ? start.instructions : "";
    const loadedHooks = await loadHookModules(normalized.hooks);
    const history = managedConversation
      ? TaskHistory.restore(start.snapshot)
      : new TaskHistory(
          { role: "system", content: userInstructions
            ? [systemInstruction, userInstructions, ...(system ? [system] : [])].join("\n\n")
            : systemInstruction + (system ? "\n" + system : "") },
          { role: "user", content: instruction },
        );
    async function prepareContext(modelStepId, final = false, appliedPlanId = undefined, candidate = undefined) {
      return rpc("context_prepare", {runId, modelStepId, messages: candidate ? candidate.messages : history.messages(),
        tools: [runJSTool], history: candidate ? candidate.layout : history.layout(),
        snapshot: candidate ? candidate.snapshot : undefined, final, appliedPlanId});
    }
    let modelStepId = null;
    while (true) {
      if (modelStepId === null) {
        const prepared = await rpc("begin_model_step", { runId, messages: history.messages(), tools: [runJSTool] });
        modelStepId = requireString(prepared.modelStepId, "modelStepId");
        if (prepared.steers !== undefined) await appendAndApplySteers(runId, history, prepared.steers);
        await prepareContext(modelStepId);
        await callHooks(loadedHooks, "beforeModelRequest", {runId, modelStepId});
      }
      const preparedContext = await prepareContext(modelStepId, true);
      if (preparedContext.plan !== undefined) {
        // Stage without deleting. The broker commits the candidate revision
        // and its factual event before local installation, so interruption
        // cannot leave an applied deletion with no receipt. No model request
        // can occur until the committed candidate is installed below.
        const candidate = history.stage(preparedContext.plan);
        await prepareContext(modelStepId, true, preparedContext.plan.id, candidate);
        history.commit(candidate);
      }
      const context = await rpc("context_get", {runId, modelStepId});
      if (!isRecord(context) || typeof context.notification !== "string") {
        throw new Error("agent broker returned an invalid context notification");
      }
      // The status is request-scoped: provider history retains only the source
      // transcript, never this diagnostic tail message.
      const modelMessages = [...history.messages(), {role: "system", content: context.notification}];
      const response = await rpc("model", {
        runId, modelStepId, model, messages: modelMessages, tools: [runJSTool], cacheKey, maxTokens, effort,
      });
      if (!isRecord(response)) throw new Error("provider returned an invalid response");
      if (response.steers !== undefined) {
        await appendAndApplySteers(runId, history, response.steers);
        continue;
      }
      await callHooks(loadedHooks, "afterModelResponse", {
        runId, modelStepId, requestId: response.requestId, response,
      });
      if (!isRecord(response) || !isRecord(response.message) || response.message.role !== "assistant") {
        throw new Error("provider returned an invalid assistant message");
      }
      if (response.message.refusal !== undefined && response.message.refusal !== null) {
        if (typeof response.message.refusal !== "string") throw new Error("provider returned a malformed refusal");
        if (response.message.refusal.trim()) throw new Error("model refused the request");
      }
      if (typeof response.finishReason !== "string" || !response.finishReason) {
        throw new Error("provider returned an invalid finish reason");
      }

      const calls = validateToolCalls(response.message);
      if (calls.length > 0) {
        if (response.finishReason !== "tool_calls") {
          throw new Error("provider returned tool calls without a tool_calls finish reason");
        }
        // Keep the provider's full assistant object, including reasoning,
        // call IDs, and extension fields, for the next request.
        const results = await Promise.all(calls.map(call => executeToolWithHooks(
          loadedHooks, {runId, modelStepId, requestId: response.requestId}, call,
          effective => executeToolCall(runId, modelStepId, effective),
        )));
        history.completeBlock(response.message, results);
        if (managedConversation) {
          await rpc("conversation_checkpoint", {runId, snapshot: history.snapshot()});
        }
        modelStepId = null;
        continue;
      }

      if (response.finishReason === "tool_calls") {
        throw new Error("provider reported tool_calls without any tool calls");
      }
      if (response.finishReason === "length") {
        throw new Error("model response reached its token limit");
      }
      if (response.finishReason !== "stop") {
        throw new Error("model response did not finish normally");
      }
      const answer = finalAnswer(response.message.content);
      if (!answer.trim()) throw new Error("model returned an empty final answer");

      // Append before the finalization gate. The broker commits this complete
      // assistant object on both accepted and steer-rejected candidates.
      history.completeBlock(response.message);
      const committed = await rpc("commit_final", { runId, answer, snapshot: history.snapshot() });
      if (!isRecord(committed) || typeof committed.committed !== "boolean") {
        throw new Error("agent broker returned an invalid final-answer result");
      }
      if (!committed.committed) {
        await appendAndApplySteers(runId, history, committed.instructions);
        modelStepId = null;
        continue;
      }
      acceptedAnswer = answer;
      status = "completed";
      return answer;
    }
  } catch (error) {
    failureMessage = String(error);
    throw error;
  } finally {
    await rpc("finish", { runId, status, answer: acceptedAnswer, error: failureMessage });
  }
}
