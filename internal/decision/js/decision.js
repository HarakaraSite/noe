import { request } from "noe:decision/internal";

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

// Validate JSON data before stringify can silently drop or replace values.
function validateJSON(value, ancestors = new Set()) {
  if (value === null || typeof value === "string" || typeof value === "boolean") return;
  if (typeof value === "number" && Number.isFinite(value)) return;
  if (typeof value !== "object") throw new TypeError("decision input must contain JSON values");
  if (ancestors.has(value)) throw new TypeError("decision input must not contain cycles");
  const prototype = Object.getPrototypeOf(value);
  if (!Array.isArray(value) && prototype !== Object.prototype && prototype !== null) {
    throw new TypeError("decision input must contain JSON objects or arrays");
  }
  ancestors.add(value);
  if (Array.isArray(value)) {
    for (const item of value) validateJSON(item, ancestors);
  } else {
    for (const key of Object.keys(value)) validateJSON(value[key], ancestors);
  }
  ancestors.delete(value);
}

export async function decide(options) {
  if (!isRecord(options)) throw new TypeError("decision options must be an object");
  for (const key of Object.keys(options)) {
    if (!["provider", "model", "state", "questions"].includes(key)) throw new TypeError("unknown decision option: " + key);
  }
  if (options.provider !== "openrouter") throw new TypeError("decision provider must be openrouter");
  if (typeof options.model !== "string" || !options.model.trim()) throw new TypeError("decision model must be a nonempty string");
  const state = options.state;
  if (!(typeof state === "string" && state.trim()) && !(state !== null && typeof state === "object")) {
    throw new TypeError("decision state must be a nonempty string, object, or array");
  }
  if (!isRecord(options.questions) || !Object.keys(options.questions).length ||
      Object.values(options.questions).some(question => !isRecord(question))) {
    throw new TypeError("decision questions must be a nonempty object of question objects");
  }
  validateJSON(options);
  return JSON.parse(await request("decide", JSON.stringify(options)));
}
