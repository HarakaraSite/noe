// Local content search for NOE. No Node.js, npm, or shell invocation.
import {spawn} from "noe:process";

const encoder = new TextEncoder();
const bytes = value => encoder.encode(JSON.stringify(value)).length;
const fallbackWarning = "grep fallback: ignore files and hidden-file filtering are not applied; regex uses extended POSIX syntax.";

function strings(value, name) {
  if (!Array.isArray(value) || value.some(s => typeof s !== "string" || !s || s.includes("\0"))) {
    throw new TypeError(`search: ${name} must be an array of nonempty strings without NUL`);
  }
  return value;
}

function optionsFor(options) {
  if (!options || typeof options !== "object" || Array.isArray(options)) throw new TypeError("search: options must be an object");
  const {pattern, paths = ["."], mode = "lines", regex = true, ignoreCase = false,
    glob = [], maxResults = 50, maxBytes = 8000, backend = "auto"} = options;
  if (typeof pattern !== "string" || pattern.includes("\n") || pattern.includes("\0")) {
    throw new TypeError("search: pattern must be a single-line string without NUL");
  }
  if (!["lines", "files", "count"].includes(mode)) throw new TypeError("search: mode must be lines, files, or count");
  if (!["auto", "rg", "grep"].includes(backend)) throw new TypeError("search: backend must be auto, rg, or grep");
  if (typeof regex !== "boolean" || typeof ignoreCase !== "boolean") throw new TypeError("search: regex and ignoreCase must be booleans");
  for (const [name, value] of Object.entries({maxResults, maxBytes})) {
    if (!Number.isSafeInteger(value) || value < 1) throw new TypeError(`search: ${name} must be a positive safe integer`);
  }
  strings(paths, "paths");
  if (!paths.length) throw new TypeError("search: paths must not be empty");
  strings(glob, "glob");
  return {pattern, paths, mode, regex, ignoreCase, glob, maxResults, maxBytes, backend};
}

function argumentsFor(o, backend) {
  const args = backend === "rg"
    ? ["--no-config", "--no-heading", "--no-column", "--line-buffered"]
    : ["--recursive", "--devices=skip", "--exclude-dir=.git"];
  args.push("--color=never", "--null", "--with-filename");
  args.push(o.mode === "files" ? "--files-with-matches" : o.mode === "count" ? "--count" : "--line-number");
  if (backend === "grep") args.push("--binary-files=without-match");
  if (!o.regex) args.push("--fixed-strings");
  else if (backend === "grep") args.push("--extended-regexp");
  if (o.ignoreCase) args.push("--ignore-case");
  for (const glob of o.glob) {
    if (backend === "rg") args.push("--glob", glob);
    else {
      // GNU grep's filename globs cannot reproduce rg's path/brace globs.
      if (/[\/{}]/.test(glob) || glob.includes("**") || glob === "!") {
        throw new TypeError("search: grep fallback supports basename globs only (*, ?, []); install rg for path/brace globs");
      }
      args.push(glob.startsWith("!") ? `--exclude=${glob.slice(1)}` : `--include=${glob}`);
    }
  }
  // An explicit -e protects leading dashes in the pattern. A path '-' is a
  // file, not stdin; stdin is otherwise disabled for this finite search.
  args.push("-e", o.pattern, "--", ...o.paths.map(path => path.startsWith("-") ? `./${path}` : path));
  return args;
}

function searchError(backend, status, stderr) {
  const error = new Error(`search: ${backend} failed (${status.signal || status.code})${stderr ? `: ${stderr.trim()}` : ""}`);
  error.name = "SearchError";
  error.backend = backend;
  error.exitCode = status.code;
  return error;
}

// Bound diagnostics independently, while continuing to drain the pipe.
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

function collector(o, result) {
  // Reserve the longest completion metadata, including its JSON escaping.
  const size = () => bytes({...result, complete: false, limitedBy: "maxResults"});
  if (size() > o.maxBytes) throw new RangeError("search: maxBytes is too small for result metadata");
  const limit = reason => { result.complete = false; result.limitedBy = reason; return false; };
  return {
    limit,
    binary(path) {
      result.warnings.push(`Skipped binary match: ${path}`);
      if (size() <= o.maxBytes) return true;
      result.warnings.pop();
      return limit("maxBytes");
    },
    add(item, partial = false) {
      if (o.mode === "count" && item.count === 0) return true;
      if (result.results.length >= o.maxResults) return limit("maxResults");
      result.results.push(item);
      if (size() <= o.maxBytes && !partial) return true;
      result.results.pop();
      if (typeof item.text === "string") {
        // Keep a useful path/line and as much text as fits, instead of
        // silently dropping an entire very long matching line.
        const full = item.text;
        item = {...item, text: "", textTruncated: true};
        result.results.push(item);
        if (size() > o.maxBytes) result.results.pop();
        else {
          let low = 0, high = full.length;
          while (low < high) {
            const end = Math.ceil((low + high) / 2);
            item.text = full.slice(0, end);
            if (size() <= o.maxBytes) low = end;
            else high = end - 1;
          }
          // Do not split a UTF-16 surrogate pair at the retained boundary.
          if (low && /[\uD800-\uDBFF]/.test(full[low - 1])) low--;
          item.text = full.slice(0, low);
        }
      }
      return limit("maxBytes");
    },
  };
}

async function readResults(stream, o, collect) {
  const reader = stream.getReader();
  const decoder = new TextDecoder();
  let buffer = "", path = null;
  function row(body, partial = false) {
    if (o.mode === "count") {
      if (!/^\d+$/.test(body)) throw new Error("search: invalid count output");
      return collect.add({path, count: Number(body)});
    }
    const colon = body.indexOf(":");
    if (colon < 1 || !/^\d+$/.test(body.slice(0, colon))) throw new Error("search: invalid line output");
    return collect.add({path, line: Number(body.slice(0, colon)), text: body.slice(colon + 1)}, partial);
  }
  try {
    while (true) {
      const {value, done} = await reader.read();
      buffer += done ? decoder.decode() : decoder.decode(value, {stream: true});
      while (true) {
        if (path === null) {
          const end = buffer.indexOf("\0");
          // Explicit binary paths use rg's binary notice instead of the
          // normal NUL-delimited line format. Keep it out of line results.
          if (o.mode === "lines" && (end < 0 || buffer.slice(0, end).includes("binary file matches"))) {
            const notice = /^([\s\S]*?): binary file matches \(found "\\0" byte around offset \d+\)\n/.exec(buffer);
            if (notice) {
              if (!collect.binary(notice[1])) return;
              buffer = buffer.slice(notice[0].length);
              continue;
            }
          }
          if (end < 0) break;
          path = buffer.slice(0, end);
          buffer = buffer.slice(end + 1);
          if (o.mode === "files") {
            if (!collect.add(path)) return;
            path = null;
            continue;
          }
        }
        const end = buffer.indexOf("\n");
        if (end < 0) break;
        if (!row(buffer.slice(0, end))) return;
        path = null;
        buffer = buffer.slice(end + 1);
      }
      // At most the result budget plus one stream chunk is held for an
      // unfinished record; a huge line is never accumulated in full.
      if (encoder.encode(buffer).length > o.maxBytes) {
        if (path !== null && o.mode === "lines") row(buffer, true);
        else collect.limit("maxBytes");
        return;
      }
      if (done) {
        if (path !== null || buffer) throw new Error("search: incomplete command output");
        return;
      }
    }
  } finally {
    reader.releaseLock();
  }
}

/** Search file contents. See search.md for options and completeness semantics. */
export async function grep(options) {
  const o = optionsFor(options);
  let backend = o.backend === "auto" ? "rg" : o.backend;
  let child;
  try {
    child = await spawn(backend, argumentsFor(o, backend), {stdin: "ignore"});
  } catch (error) {
    // NOE spawn currently reports Go's executable lookup error as a message.
    // Do not hide permission, cwd, regex, or other execution errors.
    if (o.backend !== "auto" || !String(error.message).includes('exec: "rg": executable file not found in ')) throw error;
    backend = "grep";
    child = await spawn(backend, argumentsFor(o, backend), {stdin: "ignore"});
  }
  const result = {backend, mode: o.mode, results: [], complete: true, limitedBy: null,
    warnings: backend === "grep" ? [fallbackWarning] : []};
  try {
    const collect = collector(o, result);
    const [, stderr, status] = await Promise.all([
      readResults(child.stdout, o, collect).then(async () => {
        if (result.limitedBy) await child.close();
      }),
      readErrors(child.stderr, result),
      child.exited,
    ]);
    if (!result.limitedBy && (status.signal || (status.code !== 0 && status.code !== 1))) {
      throw searchError(backend, status, stderr);
    }
    // Some commands can report warnings with an otherwise successful status.
    if (stderr.trim() && !result.limitedBy) throw searchError(backend, status, stderr);
    return result;
  } finally {
    await child.close();
  }
}
