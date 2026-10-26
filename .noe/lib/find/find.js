// Filename discovery for NOE. No npm dependencies or shell invocation.
import {spawn} from "noe:process";
import {lstat} from "node:fs/promises";
import {relative, resolve} from "node:path";

const encoder = new TextEncoder();
const bytes = value => encoder.encode(JSON.stringify(value)).length;
const types = {all: null, file: "f", directory: "d", symlink: "l"};

function optionsFor(path, options) {
  if (typeof path !== "string" || !path || path.includes("\0")) {
    throw new TypeError("find: path must be a nonempty string without NUL");
  }
  if (!options || typeof options !== "object" || Array.isArray(options)) throw new TypeError("find: options must be an object");
  const {name = "*", type = "all", ignoreCase = false, maxDepth,
    maxResults = 300, maxBytes = 32000, backend = "auto"} = options;
  if (typeof name !== "string" || !name || /[\0\n/\\{}]/.test(name) || name.includes("**")) {
    throw new TypeError("find: name must be a basename glob (*, ?, []), without NUL, newline, slash, backslash, braces or **");
  }
  if (typeof type !== "string" || !Object.hasOwn(types, type)) throw new TypeError("find: type must be all, file, directory, or symlink");
  if (typeof ignoreCase !== "boolean") throw new TypeError("find: ignoreCase must be a boolean");
  if (!["auto", "fd", "find"].includes(backend)) throw new TypeError("find: backend must be auto, fd, or find");
  for (const [key, value] of Object.entries({maxResults, maxBytes, ...(maxDepth === undefined ? {} : {maxDepth})})) {
    if (!Number.isSafeInteger(value) || value < 1) throw new TypeError(`find: ${key} must be a positive safe integer`);
  }
  return {path: resolve(path), name, type, ignoreCase, maxDepth, maxResults, maxBytes, backend};
}

function argumentsFor(o, backend) {
  if (backend === "fd") {
    const args = ["--glob", "--print0", "--color=never", "--hidden", "--no-ignore",
      "--exclude", ".git", "--show-errors", o.ignoreCase ? "--ignore-case" : "--case-sensitive"];
    if (o.maxDepth !== undefined) args.push("--max-depth", String(o.maxDepth));
    if (types[o.type]) args.push("--type", types[o.type]);
    return [...args, "--", o.name, "."];
  }
  const args = [".", "-mindepth", "1"];
  if (o.maxDepth !== undefined) args.push("-maxdepth", String(o.maxDepth));
  args.push("(", "-name", ".git", "-prune", ")", "-o", "(");
  if (types[o.type]) args.push("-type", types[o.type]);
  args.push(o.ignoreCase ? "-iname" : "-name", o.name, "-print0", ")");
  return args;
}

function findError(backend, status, stderr) {
  const error = new Error(`find: ${backend} failed (${status.signal || status.code})${stderr ? `: ${stderr.trim()}` : ""}`);
  error.name = "FindError";
  error.backend = backend;
  error.exitCode = status.code;
  return error;
}

function collector(o, result) {
  const size = () => bytes({...result, complete: false, limitedBy: "maxResults"});
  if (size() > o.maxBytes) throw new RangeError("find: maxBytes is too small for result metadata");
  const limit = reason => { result.complete = false; result.limitedBy = reason; return false; };
  return {
    limit,
    add(path) {
      if (result.results.length >= o.maxResults) return limit("maxResults");
      result.results.push(path);
      if (size() <= o.maxBytes) return true;
      result.results.pop();
      return limit("maxBytes");
    },
  };
}

async function readResults(stream, o, collect) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (true) {
      const {value, done} = await reader.read();
      buffer += done ? decoder.decode() : decoder.decode(value, {stream: true});
      let end;
      while ((end = buffer.indexOf("\0")) >= 0) {
        // Both commands run in the root and print ./relative paths.
        const path = relative(".", buffer.slice(0, end));
        buffer = buffer.slice(end + 1);
        if (!collect.add(path)) return;
      }
      if (encoder.encode(buffer).length > o.maxBytes) {
        collect.limit("maxBytes");
        return;
      }
      if (done) {
        if (buffer) throw new Error("find: incomplete command output");
        return;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

async function readErrors(stream, result) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let text = "";
  try {
    while (true) {
      const {value, done} = await reader.read();
      if (done) break;
      text = (text + decoder.decode(value, {stream: true})).slice(0, 1800);
    }
    return (text + decoder.decode()).slice(0, 1800);
  } catch (error) {
    if (!result.limitedBy) throw error;
    return text;
  } finally {
    reader.releaseLock();
  }
}

/** Find files/directories by basename glob. See find.md for the contract. */
export async function find(path = ".", options = {}) {
  const o = optionsFor(path, options);
  // Directory roots only; neither backend should silently treat a file or
  // a symlink root differently. Symlinks inside the tree remain searchable.
  if (!(await lstat(o.path)).isDirectory()) throw new TypeError("find: path must be a directory, not a file or symlink");
  let backend = o.backend === "auto" ? "fd" : o.backend;
  let child;
  try {
    child = await spawn(backend, argumentsFor(o, backend), {cwd: o.path, stdin: "ignore"});
  } catch (error) {
    // Match only NOE's executable-lookup error, never permission/search errors.
    if (o.backend !== "auto" || !String(error.message).includes('exec: "fd": executable file not found in ')) throw error;
    backend = "find";
    child = await spawn(backend, argumentsFor(o, backend), {cwd: o.path, stdin: "ignore"});
  }
  const result = {backend, path: o.path, results: [], complete: true, limitedBy: null, warnings: []};
  try {
    const collect = collector(o, result);
    const [, stderr, status] = await Promise.all([
      readResults(child.stdout, o, collect).then(async () => {
        if (result.limitedBy) await child.close();
      }),
      readErrors(child.stderr, result),
      child.exited,
    ]);
    if (!result.limitedBy && (status.signal || status.code !== 0 || stderr.trim())) {
      throw findError(backend, status, stderr);
    }
    return result;
  } finally {
    await child.close();
  }
}
