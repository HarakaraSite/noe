import { request, defaults, instructions, observe } from "noe:agent/internal";

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

// Observe an existing run through its JSONL file. Snapshot assembly and waiting
// belong to Go so repeated calls do not need JS state or an additional model.
export async function observe_agent(options = {}) {
  if (!isRecord(options)) throw new TypeError("observation options must be an object");
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

export function createAgent(options = {}) {
  if (!isRecord(options)) throw new TypeError("agent options must be an object");
  const provider = requireString(options.provider === undefined ? inherited.provider : options.provider, "provider");
  if (provider !== "openrouter") throw new TypeError("provider must be openrouter");
  const model = requireString(options.model === undefined ? inherited.model : options.model, "model");
  const name = options.name === undefined ? "agent" : requireString(options.name, "name");
  const extraSystem = options.system === undefined ? "" : requireString(options.system, "system", true);
  const effort = requireEffort(options.effort === undefined ? inherited.effort : options.effort);
  const maxTokens = options.maxTokens === undefined ? (inherited.maxTokens || 0) : requireLimit(options.maxTokens, "maxTokens");
  requireLimit(maxTokens, "maxTokens");

  return Object.freeze({
    async run(instruction, runOptions = {}) {
      requireString(instruction, "instruction");
      if (!isRecord(runOptions)) throw new TypeError("run options must be an object");
      const configuredCacheKey = runOptions.cacheKey === undefined ? undefined : requireString(runOptions.cacheKey, "cacheKey", true);
      const maxSteps = runOptions.maxSteps === undefined ? 0 : requireLimit(runOptions.maxSteps, "maxSteps");

      const start = await rpc("start", { provider, model, name, maxTokens, effort });
      const runId = start.runId;
      if (typeof runId !== "string" || !runId) throw new Error("agent broker returned an invalid run ID");
      const cacheKey = configuredCacheKey === undefined ? runId : configuredCacheKey;
      let status = "failed";
      let acceptedAnswer = "";
      let failureMessage = "";
      try {
        const messages = [
          { role: "system", content: systemInstruction + (extraSystem ? "\n" + extraSystem : "") },
          { role: "user", content: instruction },
        ];
        let modelCalls = 0;

        while (true) {
          if (maxSteps > 0 && modelCalls >= maxSteps) {
            throw new Error("agent reached maxSteps=" + maxSteps + " without a final answer");
          }
          modelCalls++;
          const response = await rpc("model", {
            runId, model, messages, tools: [runJSTool], cacheKey, maxTokens, effort,
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
          acceptedAnswer = answer;
          status = "completed";
          return answer;
        }
      } catch (error) {
        failureMessage = String(error);
        throw error;
      } finally {
        await rpc("finish", { runId, status, answer: acceptedAnswer, error: failureMessage }).catch(() => {});
      }
    },
  });
}
