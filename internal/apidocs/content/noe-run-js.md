# `noe:run_js` agent tool

`noe:run_js` is a tool available to managed agents. It is a tool name and documentation topic, not an importable module or an additional tool for ordinary JavaScript. It runs supplied code in a fresh NOE process; variables, module state, and open runtime resources do not persist to the next call. The process uses the current working directory and inherits the configured import map.

Every call starts a new process. Relative imports in the temporary module resolve from that module's source location; use a configured import-map alias or a local `.js`/`.mjs` path. HTTP imports, npm discovery, and unmapped bare package imports are unavailable. Resources created during the call belong to its runtime; owned work is recovered when the task ends or is cancelled.

Top-level await and ES module imports are supported. Print results with console.log; a final expression is not automatically returned to the model. Filesystem paths are relative to process.cwd(), whereas relative module imports use the temporary source location. Use an absolute path for an unmapped local module.

Task recipes are available as details: files, commands, imports, http, agents, and failures. For example, docs("noe:run_js", "files") reads file-operation recipes directly. Each code block below is a separate run_js call; adapt input files, program names, and URLs to the task.

## run_js

The input is an object with one required property:

```json
{"code":"// Read the embedded API reference.\nimport { docs } from 'noe:docs';\nconsole.log(docs('noe:process', 'spawn'));"}
```

`code` must be a string. Its first line must be a short English `//` comment describing the operation. Put the comment before imports and executable statements, with no leading blank line. Do not import or redeclare the managed-agent helpers already bound by the tool, such as `run_agent` and `observe_agent`.

The result reports `stdout`, `stderr`, `exitCode`, and `reason`. A nonzero exit code is still a result and can include useful output. `reason` distinguishes ordinary completion from process failure, timeout, or cancellation.

Standard output and standard error are captured independently. Each stream retains at most 64 KiB: the first 32 KiB and last 32 KiB. If a stream is larger, its text contains a truncation marker and the result includes the corresponding `stdoutOmittedBytes` or `stderrOmittedBytes` count. These byte counts describe omitted captured output, not an exit status.

Use `noe:docs` from `run_js` to look up an API before using it. Use `noe:process` when a child command must remain active during the call, and close owned handles in `finally`. A child process, socket, signal subscription, or other owned resource cannot continue independently after its owning tool runtime ends.

`noe:image` has a task-specific path: `read_image(path)` returns metadata to JavaScript and attaches the image to the next model request after the tool call. Do not expect a top-level `noe run`, `eval`, or Worker to provide model vision context.

## files

Read and write whole files with node:fs or node:fs/promises. Supply "utf8" when text is wanted; reads otherwise return bytes. This example assumes input.json contains an array and creates or replaces summary.json:

~~~js
// Read records and save a summary.
import {readFileSync, writeFileSync} from "node:fs";

const rows = JSON.parse(readFileSync("input.json", "utf8"));
const summary = {count: rows.length};
writeFileSync("summary.json", JSON.stringify(summary, null, 2));
console.log(JSON.stringify(summary));
~~~

readdirSync(path) returns string names, not Node Dirent objects. The withFileTypes option is not implemented. Use statSync for metadata; use lstatSync if the symbolic link itself matters:

~~~js
// List directory entries with their types.
import {readdirSync, statSync} from "node:fs";
import {join} from "node:path";

const dir = "docs";
console.log(JSON.stringify(readdirSync(dir).map(name => ({
  name,
  directory: statSync(join(dir, name)).isDirectory()
}))));
~~~

For async access, await readFile/readdir/stat from node:fs/promises. readdir also returns string names. Group independent async work with Promise.all when useful; keep dependent reads and writes ordered. For recursive walks, choose a policy for symbolic links rather than unintentionally following directory links in a cycle. See docs("node:fs", "readdirSync"), docs("node:fs", "statSync"), or docs("node:fs/promises", "readdir") for the exact contracts.

## commands

Use node:child_process execFile for a finite program with captured output. It already returns a Promise and accepts an argument array; no promisify, callback, or shell is required. The program must be installed on the host. For example, in a Git working tree:

~~~js
// Read the current Git working tree status.
import {execFile} from "node:child_process";

const {stdout, stderr} = await execFile("git", ["status", "--short"]);
console.log(stdout);
if (stderr) console.error(stderr);
~~~

When installed, rg can handle file discovery (rg --files) and text search without a custom directory walker. Interpret its no-match status separately from an actual command failure:

~~~js
// Search documentation and handle an empty result.
import {execFile} from "node:child_process";

try {
  const {stdout} = await execFile("rg", ["-n", "--", "TODO", "docs"]);
  console.log(stdout);
} catch (error) {
  if (error.code !== 1) throw error;
  console.log("No matches.");
}
~~~

execFile rejects on a nonzero exit with code, stdout, and stderr details. execFileSync and execSync throw an Error with the same details (their output is always text); success still returns stdout as a string. exec/execSync use a shell when shell syntax is needed. spawnSync returns a status object; inspect its error before status. For a live process with incremental Streams, read docs("noe:process", "spawn") instead. Owned processes cannot outlive the run_js runtime.

## imports

Use a configured import-map alias directly. An alias resolves a name to a path; it does not tell the model that the library exists or how to use it. Put that guidance in instructions and read the library's documentation when needed.

For a local file without an alias, resolve from the working directory to an absolute path. This example assumes lib/helper.js exports format:

~~~js
// Load a helper from the working directory.
import {resolve} from "node:path";

const {format} = await import(resolve(process.cwd(), "lib/helper.js"));
console.log(format("ready"));
~~~

process.cwd() is global; alternatively import process from "node:process". The module has no named cwd export. Local file paths work without an import map, and supported built-in modules have both bare and node: names. HTTP imports, npm lookup, and other unmapped bare package imports are unavailable. Imported module state is fresh in each run_js call. Files explicitly written to a known path can be read by later calls.

## http

Use fetch from noe:http for HTTP requests. This example assumes a local service exposes a JSON endpoint; change the URL for the task:

~~~js
// Fetch JSON and report HTTP failures.
import {fetch} from "noe:http";

const response = await fetch("http://127.0.0.1:8080/data.json");
if (!response.ok) {
  throw Error(`HTTP ${response.status}: ${(await response.text()).slice(0, 500)}`);
}
console.log(JSON.stringify(await response.json()));
~~~

HTTP 4xx/5xx responses do not reject fetch by themselves; inspect response.ok/status. A transport error rejects. Consume the body once with text(), json(), or its stream. Independent requests can use Promise.all. See docs("noe:http", "fetch") for aborts, streaming, redirects, and ownership.

## agents

Managed-agent helpers are already bound inside run_js. Do not import or redeclare them. A child inherits the current agent's provider, model, and effort when omitted. Use run_agent for a single task and inspect lastResult.state before accepting its answer:

~~~js
// Ask a child to summarize the README.
const child = await run_agent({name: "reader", maxSteps: 4},
  "Read README.md and summarize its purpose in three lines.");
const task = child.lastResult;
if (task?.state !== "completed") {
  throw Error(task?.error || JSON.stringify(child));
}
console.log(task.answer);
~~~

For a keep-alive worker, pass keepAlive in the third argument, queue the task, and await its receipt's commandId. wait_task waits for that task's outcome without polling; a failed or cancelled task resolves as data. Request instance shutdown when finished:

~~~js
// Queue a task and wait for its specific outcome.
const worker = await spawn_agent({name: "worker", maxSteps: 4},
  undefined, {keepAlive: true});
try {
  const receipt = await queue_agent({agentId: worker.agentId,
    instruction: "Read README.md and summarize its purpose in three lines."});
  const task = await wait_task({agentId: worker.agentId,
    commandId: receipt.commandId});
  if (task.state !== "completed") throw Error(task.error || JSON.stringify(task));
  console.log(task.answer);
} finally {
  await interrupt_agent({agentId: worker.agentId});
}
~~~

Child agents remain owned by the parent task across run_js calls; local variables do not. Save returned IDs to a known file if another call needs them. This managed-agent lifetime differs from ordinary subprocess handles created inside run_js, which end with that tool runtime. Use wait_task for a particular task, observe_agent for a live snapshot, steer_agent with runId for active-task guidance, and queue_agent with agentId for future work. Read docs("noe:agent") for the complete contracts.

## failures

A failed run_js result can still contain useful stdout and stderr. Read the error location and operation details before retrying. If an assumed Node API shape failed, inspect its NOE docs and the actual value/export shape rather than repeating the same assumption. For example:

~~~js
// Inspect the directory listing type and its documented contract.
import {readdirSync} from "node:fs";
import {docs} from "noe:docs";

const names = readdirSync("docs");
console.log(JSON.stringify({array: Array.isArray(names), firstType: typeof names[0]}));
console.log(docs("node:fs", "readdirSync"));
~~~

Common differences: readdirSync/readdir return strings, node:process exports only default, execFile is promise-based, and fs Streams are Web Streams. Refer directly to the relevant detail when the module is known; listing all docs topics again is unnecessary.

Do not report an operation as successful merely because a catch block printed its error and the outer process exited with zero. Catch expected outcomes deliberately; rethrow unexpected errors. Independent tool calls can run concurrently, so put operations that depend on one another in a single ordered call or wait for their results before continuing. Keep printed output relevant; when a complete large result is needed later, save it explicitly to a known file before printing a summary.
