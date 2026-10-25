import { rpc, isRecord } from "noe:agent/task-protocol";
import { callHooks, hookErrorText } from "noe:agent/hooks";
import { validateImageMetadata, validateToolArguments } from "noe:agent/run-js";

function parsedToolArguments(call) {
  if (typeof call.function.arguments !== "string") return null;
  try { return JSON.parse(call.function.arguments); } catch (_) { return null; }
}

// The wrapper is independent of the tool implementation. The model's full
// original call remains in history; effective arguments and results are
// recorded separately from actual execution facts.
export async function executeToolWithHooks(loaded, event, toolCall, execute, onImages = undefined) {
  if (onImages !== undefined && typeof onImages !== "function") {
    throw new TypeError("onImages must be a function");
  }
  const fn = toolCall.function;
  const toolName = typeof fn.name === "string" && fn.name.trim() ? fn.name : "<invalid>";
  const originalArguments = typeof toolCall._noe_raw_input === "string"
    ? toolCall._noe_raw_input : (fn.arguments === undefined ? null : fn.arguments);
  const before = await callHooks(loaded, "beforeTool", {
    ...event, toolCallId: toolCall.id, toolName,
    rawArguments: originalArguments, arguments: parsedToolArguments(toolCall),
  }, args => validateToolArguments(fn.name, args));
  const effectiveCall = before.changed ? {
    ...toolCall, function: {...fn, arguments: JSON.stringify(before.event.arguments)},
  } : toolCall;
  const executedResult = await execute(effectiveCall);
  let rawResult = executedResult;
  let rawImages = [];
  if (toolName === "run_js" && isRecord(executedResult) && Object.hasOwn(executedResult, "images")) {
    rawImages = executedResult.images === null ? [] : validateImageMetadata(executedResult.images);
    rawResult = {...executedResult};
    delete rawResult.images;
  }
  const report = {
    runId: event.runId, modelStepId: event.modelStepId,
    toolCallId: toolCall.id, toolName,
    rawArguments: originalArguments, arguments: before.event.arguments,
    rawResult, rawImages,
  };
  let after;
  try {
    after = await callHooks(loaded, "afterTool", {
      ...event, toolCallId: toolCall.id, toolName,
      rawArguments: originalArguments, arguments: before.event.arguments,
      result: rawResult, images: rawImages,
    });
  } catch (error) {
    // Ordinary handler failures have already recovered inside callHooks. Only
    // cancellation or infrastructure failure makes this output unavailable.
    try {
      await rpc("tool_result", {
        ...report, result: null, images: [], modelResultAvailable: false, hookError: hookErrorText(error),
      });
    } catch (_) {
      // Cancellation may already have closed admission. Keep the hook error.
    }
    throw error;
  }
  const result = after.changed ? after.event.result : rawResult;
  const images = after.event.images === undefined ? rawImages : validateImageMetadata(after.event.images);
  const recorded = await rpc("tool_result", {...report, result, images, modelResultAvailable: true});
  if (!isRecord(recorded) || !Object.hasOwn(recorded, "result")) {
    throw new Error("broker returned an invalid model tool output");
  }
  if (onImages !== undefined) onImages(images);
  return recorded.result;
}
