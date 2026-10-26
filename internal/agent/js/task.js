import { defaults, instructions } from "noe:agent/internal";
import { callHooks, loadHookModules } from "noe:agent/hooks";
import { TaskHistory } from "noe:agent/history";
import { rpc, isRecord } from "noe:agent/task-protocol";
import { runJSTool, executeToolCall } from "noe:agent/run-js";
import { executeToolWithHooks } from "noe:agent/tool-pipeline";

// Preserve the existing private task-module entry point for direct consumers.
export { executeToolWithHooks } from "noe:agent/tool-pipeline";

const inherited = JSON.parse(defaults());
const systemInstruction = instructions();

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
  const cacheKey = normalized.cacheKey === undefined
    ? requireString(start.sessionId, "agent session ID") : normalized.cacheKey;
  let status = "failed";
  let acceptedAnswer = "";
  let failureMessage = "";
  try {
    if (start.instructionFailed === true) {
      failureMessage = typeof start.instructionError === "string" ? start.instructionError : "instruction source failed";
      status = "failed";
      return undefined;
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
      return rpc("context_prepare", {runId, modelStepId,
        ...(managedConversation && !candidate ? {historyRevision: history.revision} : {
          messages: candidate ? candidate.messages : history.messages(),
          history: candidate ? candidate.layout : history.layout(),
        }), tools: [runJSTool],
        snapshot: candidate ? candidate.snapshot : undefined, final, appliedPlanId});
    }
    let modelStepId = null;
    while (true) {
      if (modelStepId === null) {
        const prepared = await rpc("begin_model_step", { runId,
          ...(managedConversation ? {historyRevision: history.revision} : {messages: history.messages()}),
        });
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
      // The Host freezes this exact text with its hook-failure ID batch during
      // final preparation; reads do not consume it. Admission validates the
      // sent text before marking only that batch as notified.
      // Preserve each sent status with its response as ordinary, trimmable history.
      const contextMessage = {role: "user", content: context.notification};
      const response = await rpc("model", {
        runId, modelStepId, model,
        ...(managedConversation
          ? {historyRevision: history.revision, notification: context.notification}
          : {messages: [...history.messages(), contextMessage]}),
        tools: [runJSTool], cacheKey, maxTokens, effort,
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
        const imagesByCall = new Array(calls.length);
        let results;
        try {
          results = await Promise.all(calls.map((call, index) => executeToolWithHooks(
            loadedHooks, {runId, modelStepId, requestId: response.requestId}, call,
            effective => executeToolCall(runId, modelStepId, effective),
            images => { imagesByCall[index] = images; },
          )));
        } catch (error) {
          try { await rpc("discard_images", {runId, modelStepId}); } catch (_) {}
          throw error;
        }
        const images = imagesByCall.flatMap(images => images || []);
        try {
          history.completeBlock(response.message, results, images, contextMessage);
        } catch (error) {
          try { await rpc("discard_images", {runId, modelStepId}); } catch (_) {}
          throw error;
        }
        if (managedConversation || images.length > 0) {
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
      history.completeBlock(response.message, [], undefined, contextMessage);
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
