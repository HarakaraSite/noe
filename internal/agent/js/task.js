import { request, defaults, instructions } from "noe:agent/internal";

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

async function executeToolCall(runId, toolCall) {
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

  return requireRunnerResult(await rpc("run_js", { runId, toolCallId: toolCall.id, code: args.code }));
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
  return { provider, model, name, system, effort, maxTokens, maxSteps, cacheKey };
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

async function appendAndApplySteers(runId, messages, value) {
  const steers = validatedSteers(value);
  for (const steer of steers) messages.push({ role: "user", content: steer.instruction });
  await rpc("apply_steers", { runId, commandIds: steers.map((steer) => steer.commandId) });
}

// This internal entry point is used by the managed-task bootstrap. It keeps the
// model/tool loop in this module and reports every started run through finish.
export async function executeTask(config, instruction) {
  const normalized = normalizeConfig(config);
  requireString(instruction, "instruction");

  const { provider, model, name, system, effort, maxTokens, maxSteps } = normalized;
  const start = await rpc("start", { provider, model, name, maxTokens, effort, maxSteps });
  const runId = start.runId;
  if (typeof runId !== "string" || !runId) throw new Error("agent broker returned an invalid run ID");
  const cacheKey = normalized.cacheKey === undefined ? runId : normalized.cacheKey;
  let status = "failed";
  let acceptedAnswer = "";
  let failureMessage = "";
  try {
    const messages = [
      { role: "system", content: systemInstruction + (system ? "\n" + system : "") },
      { role: "user", content: instruction },
    ];
    while (true) {
      const response = await rpc("model", {
        runId, model, messages, tools: [runJSTool], cacheKey, maxTokens, effort,
      });
      if (!isRecord(response)) throw new Error("provider returned an invalid response");
      if (response.steers !== undefined) {
        await appendAndApplySteers(runId, messages, response.steers);
        continue;
      }
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
        messages.push(response.message);
        const results = await Promise.all(calls.map((call) => executeToolCall(runId, call)));
        for (let i = 0; i < calls.length; i++) {
          messages.push({
            role: "tool",
            tool_call_id: calls[i].id,
            content: JSON.stringify(results[i]),
          });
        }
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

      const committed = await rpc("commit_final", { runId, answer });
      if (!isRecord(committed) || typeof committed.committed !== "boolean") {
        throw new Error("agent broker returned an invalid final-answer result");
      }
      if (!committed.committed) {
        // Retain the complete candidate object before adding the new user
        // messages, including reasoning_details and provider extensions.
        messages.push(response.message);
        await appendAndApplySteers(runId, messages, committed.instructions);
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
