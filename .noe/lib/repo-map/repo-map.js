// Language-independent repository overview for NOE. Reads names/metadata only.
import {existsSync, lstatSync, readdirSync, statSync} from "node:fs";
import {resolve, join} from "node:path";
import process from "node:process";
import {spawn} from "noe:process";

const encoder = new TextEncoder();
const byteSize = value => encoder.encode(JSON.stringify(value)).length;
const node = (name, parent = null, directory = true) => ({name, parent, directory, children: new Map(), files: 0});

function settings(options) {
  if (!options || typeof options !== "object" || Array.isArray(options)) throw new TypeError("repo-map: options must be an object");
  const {path = ".", depth = 2, maxEntries = 120, maxBytes = 8000, maxFiles = 50000, backend = "auto"} = options;
  if (typeof path !== "string" || !path || path.includes("\0")) throw new TypeError("repo-map: path must be a nonempty string without NUL");
  if (!["auto", "git", "fs"].includes(backend)) throw new TypeError("repo-map: backend must be auto, git, or fs");
  for (const [name, value] of Object.entries({depth, maxEntries, maxBytes, maxFiles})) {
    if (!Number.isSafeInteger(value) || value < (name === "depth" ? 0 : 1)) throw new TypeError(`repo-map: invalid ${name}`);
  }
  const root = resolve(path);
  if (!statSync(root).isDirectory()) throw new TypeError("repo-map: path must be a directory");
  return {root, depth, maxEntries, maxBytes, maxFiles, backend};
}

function inventory(o) {
  const root = node(".");
  let directories = 0, files = 0, stopped = false;
  function directory(parts) {
    let current = root;
    for (const name of parts) {
      if (!current.children.has(name)) {
        current.children.set(name, node(name, current));
        directories++;
      }
      current = current.children.get(name);
    }
    return current;
  }
  return {
    root,
    directory,
    get files() { return files; },
    get directories() { return directories; },
    get stopped() { return stopped; },
    add(parts, stat) {
      // Gitlinks (submodules) appear in ls-files as directory entries. Show
      // their boundary, without pretending to inventory another repository.
      if (stat.isDirectory()) {
        directory(parts).gitlink = true;
        return true;
      }
      const parent = directory(parts.slice(0, -1));
      const name = parts[parts.length - 1];
      if (parent.children.has(name)) return true;
      if (files >= o.maxFiles) { stopped = true; return false; }
      const item = node(name, parent, false);
      item.symlink = stat.isSymbolicLink();
      parent.children.set(name, item);
      files++;
      for (let current = parent; current; current = current.parent) current.files++;
      return true;
    },
  };
}

async function scanGit(o, entries) {
  const child = await spawn("git", ["-C", o.root, "ls-files", "--cached", "--others", "--exclude-standard", "--deduplicate", "-z", "--", "."], {
    stdin: "ignore", env: {...process.env, LC_ALL: "C"},
  });
  let stderr = "";
  async function errors() {
    const reader = child.stderr.getReader();
    const decoder = new TextDecoder();
    try {
      while (true) {
        const {value, done} = await reader.read();
        if (done) return;
        stderr = (stderr + decoder.decode(value, {stream: true})).slice(0, 1800);
      }
    } catch (error) {
      if (!entries.stopped) throw error;
    } finally { reader.releaseLock(); }
  }
  async function paths() {
    const reader = child.stdout.getReader();
    const decoder = new TextDecoder();
    let pending = "";
    try {
      while (true) {
        const {value, done} = await reader.read();
        pending += done ? decoder.decode() : decoder.decode(value, {stream: true});
        let end;
        while ((end = pending.indexOf("\0")) >= 0) {
          const path = pending.slice(0, end);
          pending = pending.slice(end + 1);
          const parts = path.split("/");
          const full = join(o.root, ...parts);
          // ls-files includes tracked deletions; the map describes the
          // current worktree rather than the index's deleted paths.
          let stat;
          try { stat = lstatSync(full); }
          catch (error) {
            const parent = join(o.root, ...parts.slice(0, -1));
            if (!existsSync(parent) || !readdirSync(parent).includes(parts[parts.length - 1])) continue;
            throw error;
          }
          if (!entries.add(parts, stat)) {
            await child.close();
            return;
          }
        }
        if (done) {
          if (pending) throw new Error("repo-map: incomplete git filename record");
          return;
        }
      }
    } finally { reader.releaseLock(); }
  }
  try {
    const [, , status] = await Promise.all([paths(), errors(), child.exited]);
    if (entries.stopped) return true;
    if (status.code === 128 && stderr.includes("not a git repository") && o.backend === "auto") return false;
    if (status.signal || status.code !== 0) throw new Error(`repo-map: git failed (${status.signal || status.code}): ${stderr.trim()}`);
    return true;
  } finally { await child.close(); }
}

function scanFilesystem(o, entries) {
  const pending = [[]];
  while (pending.length) {
    const parts = pending.pop();
    for (const name of readdirSync(join(o.root, ...parts))) {
      if (name === ".git") continue;
      const child = [...parts, name];
      const stat = lstatSync(join(o.root, ...child));
      if (stat.isDirectory()) {
        entries.directory(child);
        pending.push(child);
      } else if (!entries.add(child, stat)) return;
    }
  }
}

function childrenOf(item) {
  return [...item.children.values()].sort((a, b) => Number(b.directory) - Number(a.directory) || (a.name < b.name ? -1 : a.name > b.name ? 1 : 0));
}

function makeView(o, entries, backend) {
  const result = {root: o.root, backend, files: entries.files, directories: entries.directories,
    scanComplete: !entries.stopped, complete: !entries.stopped, limitedBy: entries.stopped ? ["maxFiles"] : [], tree: "",
    warnings: backend === "fs" ? ["Filesystem scan: ignore files are not applied; .git directories are skipped."] : []};
  const chosen = new Set([entries.root]);
  const ordered = new Map();
  function children(item) {
    if (!ordered.has(item)) ordered.set(item, childrenOf(item));
    return ordered.get(item);
  }
  const label = (item, depth) => {
    const name = JSON.stringify(item.name).slice(1, -1);
    if (!item.directory) return name + (item.symlink ? "@" : "");
    const count = `${item.files}${entries.stopped ? "+" : ""} files`;
    const hidden = [...item.children.values()].filter(child => !chosen.has(child)).length;
    const tail = item.gitlink ? "; gitlink" : hidden ? depth >= o.depth ? "; collapsed" : `; +${hidden} entries` : "";
    return `${name === "." ? "." : `${name}/`} (${count}${tail})`;
  };
  function render() {
    const lines = [];
    const stack = [{item: entries.root, prefix: "", connector: "", depth: 0}];
    while (stack.length) {
      const {item, prefix, connector, depth} = stack.pop();
      lines.push(prefix + connector + label(item, depth));
      const visible = children(item).filter(child => chosen.has(child));
      const nextPrefix = prefix + (connector ? connector === "└─ " ? "   " : "│  " : "");
      for (let i = visible.length - 1; i >= 0; i--) {
        stack.push({item: visible[i], prefix: nextPrefix, connector: i === visible.length - 1 ? "└─ " : "├─ ", depth: depth + 1});
      }
    }
    return lines.join("\n");
  }
  // Reserve all limit markers so final metadata cannot break the byte cap.
  function fits() {
    result.tree = render();
    return byteSize({...result, complete: false, limitedBy: ["maxFiles", "maxEntries", "maxBytes"]}) <= o.maxBytes;
  }
  if (!fits()) throw new RangeError("repo-map: maxBytes is too small for root metadata");
  const limits = new Set(result.limitedBy);
  const queue = [{item: entries.root, depth: 0}];
  let count = 0;
  // Select breadth first, so one large subtree cannot hide other top-level
  // directories. Render the selected nodes as an ordinary tree afterwards.
  for (let index = 0; index < queue.length; index++) {
    const {item, depth} = queue[index];
    if (depth >= o.depth) continue;
    for (const child of children(item)) {
      if (count >= o.maxEntries) { limits.add("maxEntries"); continue; }
      chosen.add(child);
      if (!fits()) { chosen.delete(child); limits.add("maxBytes"); continue; }
      count++;
      if (child.directory) queue.push({item: child, depth: depth + 1});
    }
  }
  result.tree = render();
  result.limitedBy = [...limits];
  result.complete = limits.size === 0;
  return result;
}

/** Return a bounded directory/file map. No file contents or language parsing. */
export async function repoMap(options = {}) {
  const o = settings(options);
  let entries = inventory(o), backend = "fs";
  if (o.backend !== "fs") {
    try {
      if (await scanGit(o, entries)) backend = "git";
    } catch (error) {
      if (o.backend !== "auto" || !String(error.message).includes('exec: "git": executable file not found in ')) throw error;
    }
  }
  if (backend === "fs") {
    entries = inventory(o);
    scanFilesystem(o, entries);
  }
  return makeView(o, entries, backend);
}
