import { request } from "noe:image/internal";
import { resolve } from "node:path";

const reservedParameters = new Set([
  "model", "prompt", "n", "stream", "provider", "baseurl", "path", "output",
  "api_key", "authorization", "headers", "session_id", "prompt_cache_key",
  "input_references", "image", "images",
]);

function isRecord(value) {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function requireString(value, label) {
  if (typeof value !== "string" || !value.trim()) {
    throw inputError(label + " must be a nonempty string");
  }
  return value;
}

function validateJSON(value, ancestors = new Set()) {
  if (value === null || typeof value === "string" || typeof value === "boolean") return;
  if (typeof value === "number" && Number.isFinite(value)) return;
  if (typeof value !== "object") throw inputError("image parameters must contain JSON values");
  if (ancestors.has(value)) throw inputError("image parameters must not contain cycles");
  const prototype = Object.getPrototypeOf(value);
  if (!Array.isArray(value) && prototype !== Object.prototype && prototype !== null) {
    throw inputError("image parameters must contain JSON objects or arrays");
  }
  ancestors.add(value);
  if (Array.isArray(value)) {
    for (const item of value) validateJSON(item, ancestors);
  } else {
    for (const key of Object.keys(value)) validateJSON(value[key], ancestors);
  }
  ancestors.delete(value);
}

function detailFor(options) {
  if (!isRecord(options)) throw inputError("read_image options must be an object");
  for (const key of Object.keys(options)) {
    if (key !== "detail") throw inputError("unknown read_image option: " + key);
  }
  const detail = options.detail === undefined ? "auto" : options.detail;
  if (detail !== "auto" && detail !== "low" && detail !== "high") {
    throw inputError("detail must be auto, low, or high");
  }
  return detail;
}

function inputError(message) { return imageError({code:"input",message}); }

function imageError(details) {
  if (!isRecord(details)) details = {};
  const error = new Error(typeof details.message === "string" ? details.message : "image request failed");
  error.name = "ImageError";
  for (const field of ["code", "providerCode", "providerMessage", "statusCode", "requestId", "usage"]) {
    if (details[field] !== undefined) error[field] = details[field];
  }
  return error;
}

async function rpc(method, payload) {
  const response = JSON.parse(await request(method, JSON.stringify(payload)));
  if (isRecord(response) && Object.prototype.hasOwnProperty.call(response, "imageError")) {
    throw imageError(response.imageError);
  }
  return response;
}

let readOrdinal = 0;
export async function read_image(path, options = {}) {
  const absolutePath = resolve(requireString(path, "path"));
  const detail = detailFor(options);
  return rpc("read_image", { path: absolutePath, detail, ordinal: ++readOrdinal });
}

export async function generate_image(options) {
  if (!isRecord(options)) throw inputError("generate_image options must be an object");
  const allowed = new Set(["provider", "model", "prompt", "output", "parameters"]);
  for (const key of Object.keys(options)) {
    if (!allowed.has(key)) throw inputError("unknown generate_image option: " + key);
  }
  const provider = requireString(options.provider, "provider");
  const model = requireString(options.model, "model");
  const prompt = requireString(options.prompt, "prompt");
  const output = resolve(requireString(options.output, "output"));
  const payload = { provider, model, prompt, output };

  if (options.parameters !== undefined) {
    if (!isRecord(options.parameters)) throw inputError("parameters must be an object");
    for (const key of Object.keys(options.parameters)) {
      if (!key.trim()) throw inputError("image parameter names must be nonempty strings");
      if (reservedParameters.has(key.toLowerCase())) {
        throw inputError("reserved image parameter: " + key);
      }
    }
    validateJSON(options.parameters);
    payload.parameters = options.parameters;
  }
  return rpc("generate_image", payload);
}
