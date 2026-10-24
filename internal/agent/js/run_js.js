import { rpc, isRecord } from "noe:agent/task-protocol";

// The model-facing schema and run_js-specific argument/result contract live together.
export const runJSTool = Object.freeze({
  type: "function",
  function: {
    name: "run_js",
    description: "Execute a JavaScript ES module with top-level await in a fresh owned runtime. Import agent operations explicitly, for example: import {spawn_agent} from 'noe:agent';. Export default to return JSON data. Results include stdout, stderr, exitCode, reason, and optional data/diagnostics. The complete model-facing result JSON has a shared 65536-byte budget, including metadata, JSON escaping, diagnostics, and hook results; larger results become explicitly incomplete outputPreview text with outputLimit metadata and original execution status. For oversized searches, narrow scope or return filenames/counts. When the required content itself is larger, add // @noe: {\"outputBytes\": 131072} in the initial line-comment block after the operation comment. outputBytes must be an integer from 1024 to 262144 bytes and also sets stdout/stderr capture size. Console output and default-export data use the same budget. Truncated streams retain their beginning and end. Do not blindly rerun side effects to retrieve output. On providers using raw JavaScript input, send the module source itself without JSON or code fences.",
    parameters: {
      type: "object",
      properties: {
        code: { type: "string", description: "An ES module containing the JavaScript to execute." },
      },
      required: ["code"],
      additionalProperties: false,
    },
  },
});

function failedToolResult(message) {
  return { stdout: "", stderr: "", exitCode: -1, reason: "failed",
    diagnostics: [{code: "execution_error", stage: "setup", message}] };
}

function requireRunnerResult(value) {
  if (!isRecord(value) || typeof value.stdout !== "string" || typeof value.stderr !== "string" ||
      !Number.isInteger(value.exitCode) || !["completed", "failed", "timeout", "cancelled"].includes(value.reason)) {
    throw new Error("runner returned an invalid tool result");
  }
  if (Object.hasOwn(value, "data")) {
    const data = value.data;
    const dataType = typeof data;
    if (data === undefined || dataType === "function" || dataType === "symbol" ||
        (dataType === "number" && !Number.isFinite(data))) {
      throw new Error("runner returned invalid execution data");
    }
  }
  if (value.diagnostics !== undefined) {
    if (!Array.isArray(value.diagnostics)) throw new Error("runner returned invalid diagnostics");
    for (const diagnostic of value.diagnostics) {
      if (!isRecord(diagnostic) || typeof diagnostic.code !== "string" ||
          typeof diagnostic.stage !== "string" || typeof diagnostic.message !== "string" ||
          (diagnostic.stack !== undefined && typeof diagnostic.stack !== "string")) {
        throw new Error("runner returned invalid diagnostics");
      }
      if (diagnostic.position !== undefined) {
        const position = diagnostic.position;
        if (!isRecord(position) ||
            (position.source !== undefined && typeof position.source !== "string") ||
            (position.line !== undefined && (!Number.isInteger(position.line) || position.line < 1)) ||
            (position.column !== undefined && (!Number.isInteger(position.column) || position.column < 1))) {
          throw new Error("runner returned invalid diagnostic position");
        }
      }
      if (diagnostic.exception !== undefined && !isRecord(diagnostic.exception)) {
        throw new Error("runner returned invalid exception details");
      }
    }
  }
  if (value.images !== undefined && value.images !== null) validateImageMetadata(value.images);
  return value;
}

export function validateImageMetadata(images) {
  if (!Array.isArray(images)) throw new Error("runner returned invalid image metadata");
  const seenIDs = new Set();
  for (const image of images) {
    if (!isRecord(image) || typeof image.imageId !== "string" || !image.imageId || seenIDs.has(image.imageId)) {
      throw new Error("runner returned invalid image metadata");
    }
    seenIDs.add(image.imageId);
  }
  return images;
}

export async function executeToolCall(runId, modelStepId, toolCall) {
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

export function validateToolArguments(toolName, args) {
  if (!isRecord(args)) throw new TypeError("tool arguments must be an object");
  if (toolName === "run_js" && (typeof args.code !== "string" || Object.keys(args).some(key => key !== "code"))) {
    throw new TypeError("run_js hook arguments must contain only code: string");
  }
}
