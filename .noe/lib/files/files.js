import {readdir, lstat, readFile, writeFile, mkdir} from "node:fs/promises";
import {join, resolve, dirname} from "node:path";

function integer(value, minimum, name) {
  if (!Number.isSafeInteger(value) || value < minimum) {
    throw new RangeError(`${name} must be a safe integer >= ${minimum}`);
  }
}

// Paths in entries are relative to the returned absolute root path.
export async function ls(path = ".", {tree = false, depth = 2, offset = 0, limit = 100} = {}) {
  integer(depth, 1, "depth");
  integer(offset, 0, "offset");
  integer(limit, 1, "limit");
  const root = resolve(path);
  const entries = [];
  async function walk(relative, level) {
    const names = (await readdir(join(root, relative))).sort();
    for (const name of names) {
      const entryPath = join(relative, name);
      const stat = await lstat(join(root, entryPath));
      const type = stat.isSymbolicLink() ? "symlink" : stat.isDirectory() ? "directory" : stat.isFile() ? "file" : "other";
      entries.push({path: entryPath, type, bytes: stat.size, depth: level});
      if (tree && type === "directory" && level < depth) await walk(entryPath, level + 1);
    }
  }
  await walk("", 1);
  const page = entries.slice(offset, offset + limit);
  const nextOffset = offset + page.length;
  return {
    path: root,
    entries: page,
    total: entries.length,
    ...(nextOffset < entries.length ? {nextOffset} : {}),
  };
}

// Preserve line endings and read to EOF unless a line limit is requested.
export async function read_file(path, {offset = 1, limit} = {}) {
  integer(offset, 1, "offset");
  if (limit !== undefined) integer(limit, 1, "limit");
  const absolute = resolve(path);
  const text = await readFile(absolute, "utf8");
  const lines = text.match(/[^\n]*\n|[^\n]+$/g) ?? [];
  const start = Math.min(offset - 1, lines.length);
  const end = limit === undefined ? lines.length : Math.min(start + limit, lines.length);
  return {
    path: absolute,
    text: lines.slice(start, end).join(""),
    offset,
    lines: end - start,
    totalLines: lines.length,
    ...(end < lines.length ? {nextOffset: end + 1} : {}),
  };
}

// Create parent directories and write UTF-8 text, replacing existing content.
export async function write_file(path, text) {
  if (typeof text !== "string") throw new TypeError("text must be a string");
  const absolute = resolve(path);
  await mkdir(dirname(absolute), {recursive: true});
  await writeFile(absolute, text);
  return {path: absolute, bytes: new TextEncoder().encode(text).length};
}

// Exact, literal replacement: one match by default, all matches on request.
export async function edit_file(path, oldText, newText, {all = false} = {}) {
  if (typeof oldText !== "string" || oldText.length === 0) throw new TypeError("oldText must be a nonempty string");
  if (typeof newText !== "string") throw new TypeError("newText must be a string");
  if (typeof all !== "boolean") throw new TypeError("all must be a boolean");
  const absolute = resolve(path);
  const text = await readFile(absolute, "utf8");
  const parts = text.split(oldText);
  const matches = parts.length - 1;
  if (matches === 0) throw new Error("oldText was not found; read the current file and use exact text, including line endings");
  if (matches > 1 && !all) throw new Error(`oldText matched ${matches} places; include more surrounding text or use {all: true}`);
  const updated = parts.join(newText);
  await writeFile(absolute, updated);
  return {path: absolute, replacements: matches, bytes: new TextEncoder().encode(updated).length};
}
