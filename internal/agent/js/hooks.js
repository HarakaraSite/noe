import { beginHookScope, endHookScope, requestHook, resolveHookModule } from "noe:agent/internal";
import { createAgentAPI } from "noe:agent/api";

function requireScope(value) {
  if (value === null || typeof value !== "object" || typeof value.scopeId !== "string" || !value.scopeId ||
      value.defaults === null || typeof value.defaults !== "object" || Array.isArray(value.defaults)) {
    throw new Error("agent broker returned an invalid hook scope");
  }
  return value;
}

export function hookErrorText(error) {
  try { if (error?.stack) return String(error.stack); } catch (_) {}
  try { return String(error); } catch (_) { return "unprintable hook error"; }
}

// Bind each callback to a separate Go operation. Its requests are cancelled
// when the callback returns, while child instances remain owned by the run.
export async function withHookAPI(callback, metadata = {}) {
  if (typeof callback !== "function") throw new TypeError("hook callback must be a function");
  const scope = requireScope(JSON.parse(await beginHookScope(JSON.stringify(metadata))));
  const scopeId = scope.scopeId;
  let active = true;
  const request = (method, payload) => {
    if (!active) return Promise.reject(new Error("hook API scope has ended"));
    return requestHook(scopeId, method, payload);
  };
  const agents = createAgentAPI({ request, defaults: () => JSON.stringify(scope.defaults) });
  const event = scope.event || {};
  const api = Object.freeze({
    agents,
    execution: Object.freeze({
      get: async () => JSON.parse(await request("execution_get", "{}")),
    }),
    context: Object.freeze({
      get: async () => {
        const result = JSON.parse(await request("context_get", JSON.stringify({
          runId: event.runId, modelStepId: event.modelStepId,
        })));
        if (!result || typeof result !== "object" || !result.context) {
          throw new Error("agent broker returned an invalid context snapshot");
        }
        return result.context;
      },
      setRuntimeInfo: async (value) => {
        if (!isRecord(value)) throw new TypeError("runtime information must be an object");
        for (const key of Object.keys(value)) {
          if (key !== "key" && key !== "text") throw new TypeError("unknown runtime information option: " + key);
        }
        if (typeof value.key !== "string" || !value.key.trim()) {
          throw new TypeError("runtime information key must be a nonempty string");
        }
        if (typeof value.text !== "string") throw new TypeError("runtime information text must be a string");
        if (event.phase !== "beforeModelRequest") {
          throw new Error("runtime information is only available during beforeModelRequest");
        }
        await request("context_set_runtime_info", JSON.stringify({
          runId: event.runId, modelStepId: event.modelStepId,
          key: value.key, text: value.text,
        }));
      },
    }),
  });
  let result;
  let callbackError;
  let callbackFailed = false;
  try {
    result = await callback(api, scope.event || {});
  } catch (error) {
    callbackFailed = true;
    callbackError = error;
  }
  active = false;
  let closeError;
  let closeFailed = false;
  try {
    await endHookScope(scopeId, callbackFailed ? "failed" : "completed",
      callbackFailed ? hookErrorText(callbackError) : "");
  } catch (error) {
    closeFailed = true;
    closeError = error;
  }
  if (callbackFailed) throw callbackError;
  if (closeFailed) throw closeError;
  return result;
}

const phases = ["beforeModelRequest", "afterModelResponse", "beforeTool", "afterTool"];

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function copyJSON(value) {
  const encoded = JSON.stringify(value);
  if (encoded === undefined) throw new TypeError("hook value must be JSON serializable");
  return JSON.parse(encoded);
}

export async function loadHookModules(specifiers) {
  const loaded = [];
  for (let index = 0; index < specifiers.length; index++) {
    let module = specifiers[index];
    let handlers;
    try {
      module = resolveHookModule(module, index);
      handlers = await import(module);
      for (const phase of phases) {
        if (handlers[phase] !== undefined && typeof handlers[phase] !== "function") {
          throw new TypeError(phase + " must be a function when exported");
        }
      }
    } catch (error) {
      throw new Error("load hook module " + module + ": " + hookErrorText(error));
    }
    loaded.push({module, index, handlers});
  }
  return loaded;
}

// Hooks receive separate JSON snapshots. In-place mutation cannot alter the
// provider message, raw tool facts, or another concurrent callback's input.
export async function callHooks(loaded, phase, event, validateReplacement) {
  let current = copyJSON(event);
  let changed = false;
  for (const registration of loaded) {
    const handler = registration.handlers[phase];
    if (handler === undefined) continue;
    const replacement = await withHookAPI(async (api, authoritative) => {
      const value = await handler(copyJSON({...current, ...authoritative}), api);
      if (phase !== "beforeTool" && phase !== "afterTool") return undefined;
      if (value === undefined) return undefined;
      const field = phase === "beforeTool" ? "arguments" : "result";
      const keys = isRecord(value) ? Object.keys(value) : [];
      if (!isRecord(value) || !Object.hasOwn(value, field) ||
          keys.some(key => key !== field && !(phase === "afterTool" && key === "imageIds"))) {
        throw new TypeError(phase + " must return undefined or {" + field + "}");
      }
      const replacement = copyJSON(value[field]);
      if (validateReplacement) validateReplacement(replacement);
      if (phase !== "afterTool" || !Object.hasOwn(value, "imageIds")) return {value: replacement};

      if (!Array.isArray(value.imageIds)) throw new TypeError("afterTool imageIds must be an array");
      const selected = copyJSON(value.imageIds);
      const availableImages = current.images === undefined ? [] : current.images;
      if (!Array.isArray(availableImages)) throw new TypeError("afterTool event.images must be an array");
      const availableIDs = new Set();
      for (const image of availableImages) {
        if (!isRecord(image) || typeof image.imageId !== "string" || !image.imageId || availableIDs.has(image.imageId)) {
          throw new TypeError("afterTool event.images contains invalid metadata");
        }
        availableIDs.add(image.imageId);
      }
      const selectedIDs = new Set();
      for (const imageId of selected) {
        if (typeof imageId !== "string" || !imageId) throw new TypeError("afterTool imageIds must contain nonempty strings");
        if (selectedIDs.has(imageId)) throw new TypeError("afterTool imageIds must not contain duplicates");
        if (!availableIDs.has(imageId)) throw new TypeError("afterTool imageIds must be a subset of current event.images");
        selectedIDs.add(imageId);
      }
      return {
        value: replacement,
        images: availableImages.filter(image => selectedIDs.has(image.imageId)),
      };
    }, {
      runId: event.runId, modelStepId: event.modelStepId,
      phase, hookIndex: registration.index, module: registration.module,
      requestId: event.requestId, toolCallId: event.toolCallId, toolName: event.toolName,
    });
    if (replacement !== undefined) {
      const field = phase === "beforeTool" ? "arguments" : "result";
      current[field] = replacement.value;
      if (replacement.images !== undefined) current.images = replacement.images;
      changed = true;
    }
  }
  return {event: current, changed};
}
