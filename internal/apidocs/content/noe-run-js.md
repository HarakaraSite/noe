# `noe:run_js` agent tool

`noe:run_js` is a tool available to managed agents. It is a tool name and documentation topic, not an importable module or an additional tool for ordinary JavaScript. It runs supplied code in a fresh, owned NOE runtime; variables, module state, and open runtime resources do not persist to the next call. The runtime uses the current working directory and the configured loader factory.

Every call starts a new runtime. The supplied source is a virtual entry, so relative imports resolve from the execution's configured cwd; imports from ordinary files keep normal referrer-relative behavior. The selected loader factory handles imports. HTTP/HTTPS ESM imports depend on the selected loader; the NOE CLI enables them by default, while Go hosts opt in explicitly. npm discovery and unmapped bare package imports are unavailable. Resources created during the call belong to its runtime; owned work is recovered when the task ends or is cancelled.

Top-level await and ES module imports are supported. Return structured data with export default value or export default await operation(); console.log remains captured stdout. A final expression is not automatically returned to the model. Filesystem paths are relative to process.cwd(). Virtual-entry relative imports use the configured cwd; imports from loaded files resolve relative to their referrer. Use an absolute path for an unmapped local module.

Task recipes are available as details: results, files, commands, imports, http, agents, and failures. For example, docs("noe:run_js", "files") reads file-operation recipes directly. Each code block below is a separate run_js call; adapt input files, program names, and URLs to the task.

## run_js

The internal input is an object with one required property. JSON tool providers use this form:

```json
{"code":"// Read the embedded API reference.\nimport { docs } from 'noe:docs';\nconsole.log(docs('noe:process', 'spawn'));"}
```

The built-in openai-chatgpt provider instead accepts the JavaScript module source directly as a custom tool input, without JSON wrapping or code fences. Hooks still receive parsed `arguments: {code}`; `rawArguments` is the original source on that transport, and the original JSON string on JSON tool transports.

`code` must be a string. NOE's default Policy asks for a short English `//` comment describing the operation on the first line, before imports and executable statements, with no leading blank line. This is a NOE policy, not an API requirement; other hosts supply their own Policy. Agent helpers are not implicitly bound: import them explicitly from noe:agent, just like other ES module APIs.

The result reports `stdout`, `stderr`, `exitCode`, and `reason`, plus optional `data` and `diagnostics`. A nonzero exit code is still a result and can include useful output. `reason` distinguishes ordinary completion from process failure, timeout, or cancellation. stdout/stderr contain actual writes only; NOE exception diagnostics are separate.

Standard output and standard error are captured independently. Each stream retains at most 64 KiB: the first 32 KiB and last 32 KiB. If a stream is larger, its text contains a truncation marker and the result includes the corresponding `stdoutOmittedBytes` or `stderrOmittedBytes` count. These byte counts describe omitted captured output, not an exit status.

Use `noe:docs` from `run_js` to look up an API before using it. Use `noe:process` when a child command must remain active during the call, and close owned handles in `finally`. A child process, socket, signal subscription, or other owned resource cannot continue independently after its owning tool runtime ends.

`noe:image` has a task-specific path: `read_image(path)` returns metadata to JavaScript and attaches the image to the next model request after the tool call. Do not expect a top-level `noe run`, `eval`, or Worker to provide model vision context.

## results

~~~js
// Return a structured directory count.
import {readdirSync} from "node:fs";
export default {count: readdirSync(".").length};
~~~

This returns data: {count: ...}, separately from stdout/stderr. No default export omits data; export default null returns data: null. A default Promise or thenable is awaited once. A default function is never called automatically. Root undefined/function/Symbol values with no JSON representation, BigInt, cycles, and throwing conversions fail serialization. Normal nested JSON.stringify rules apply, including omitted object properties, null in arrays, and toJSON/getter calls. Nested Promises are not deeply awaited; resolve them yourself, for example with Promise.all.

Diagnostics contain code, stage, message, and available stack/position/exception. Stages are setup, execute, serialize, and cleanup; these are observed NOE stages, not exact engine parse/link phases. Position components are included only when actually available. A diagnostic's code is a NOE classification; exception.code is the original command/user code. Command stdout/stderr remain inside exception details; binary output is {encoding: "base64", data: "..."}.

Serialization occurs after the entry/default and its owned work complete. Jobs created by successful JSON conversion are checked before final success; the value is not serialized twice. A serialize failure or later job failure may follow completed file writes or external effects. NOE does not replay the source to repair it. If cleanup alone fails after data is finalized, data remains available together with a failure diagnostic.

The runner preserves its captured result for hooks and execution events. After afterTool, the model-facing JSON value has a shared default budget of 64 KiB (65536 UTF-8 bytes), including metadata, JSON escaping, stdout, stderr, data, diagnostics, command exception output, and arbitrary hook results. Values within budget retain their existing shape. Overflow returns an envelope with outputPreview, outputLimit, and original execution status in execution. The preview keeps the beginning and end of the serialized hook result, is incomplete JSON text, and must not be parsed as complete data. outputLimit reports limitBytes, sourceBytes, shownBytes, omittedBytes, returnedBytes, and complete:false. shown/omitted count source JSON bytes; returnedBytes is the encoded envelope size, including all metadata and JSON escaping, and stays within limitBytes. The same outputBytes setting also controls stream capture; capture omission counts describe source stream bytes, separately from model-preview omissions.

For oversized searches, narrow the query/scope or return filenames/counts, then read relevant parts. When the required content itself is larger, request a per-call budget in the initial line-comment block. Keep the operation comment first:

~~~js
// Read the required HTTP detail.
// @noe: {"outputBytes": 131072}
import {docs} from "noe:docs";
console.log(docs("noe:http", "serve"));
~~~

Only outputBytes is accepted, as an integer from 1024 to 262144 bytes (1–256 KiB). The minimum reserves room for omission metadata. It sets both each stream’s capture size and the shared final JSON budget. Console output and default-export data follow the same setting. Invalid or duplicate pragmas return a setup diagnostic before execution. Effective code after beforeTool controls this setting. Streams over the selected budget retain their beginning and end. Changing the setting in a later call does not recover previously omitted output. NOE does not replay the source or automatically save a full artifact. Check side effects before repeating work. Save large artifacts explicitly with the filesystem APIs and return a useful summary/path. Caught exceptions stay caught; printing an error does not make NOE infer an uncaught failure.

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

import.meta.url gives each file-backed module its encoded file URL and each HTTP/HTTPS module its final canonical URL. The virtual run_js entry uses a synthetic <virtual-entry>.mjs file URL under its configured cwd; the file need not exist. new URL("./asset.txt", import.meta.url) computes a module-relative asset URL without reading it. Ordinary filesystem-relative paths still use cwd.

process.cwd() is global; alternatively import process from "node:process". The module has no named cwd export. Local file paths work without an import map, and supported built-in modules have both bare and node: names. HTTP/HTTPS ESM URL imports are enabled in the CLI profile and optional in embedded profiles, independently of fetch. The standard URL loader accepts JavaScript MIME types and text/plain without requiring a .js/.mjs URL suffix. It performs no TypeScript transformation, npm lookup, or cross-runtime download caching; unmapped bare package imports are unavailable. Synchronous module downloads pause that runtime's JavaScript jobs and use the host execution context for cancellation. Imported module state is fresh in each run_js call. Files explicitly written to a known path can be read by later calls.

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

Import managed-agent helpers explicitly from noe:agent; the tool adds no implicit bindings. A child inherits the current agent's provider, model and effort when omitted.

For a parent waiting on delegated work, prefer spawn_agent and one wait_tasks call for the entire set. This lets guidance to the parent end the wait without ending its children:

~~~js
// Delegate work and return on completion or added guidance to the parent.
import {spawn_agent, wait_tasks} from "noe:agent";
const child = await spawn_agent({name: "reader", maxSteps: 4},
  "Read README.md and summarize its purpose in three lines.");
if (!child.commandId) throw Error("spawn did not return a task command");
export default await wait_tasks({tasks: [{agentId: child.agentId,
  commandId: child.commandId}], includeAnswer: false});
~~~

On reason:steer, finish run_js and let the parent model read the instruction and decide what to do. Do not loop, immediately re-wait, stop children in a finally block, or use Promise.all of completion-only waits to simulate this. For several children, pass all agentId/commandId pairs to the same wait_tasks call. completed contains retained outcomes and pending contains unfinished IDs; use wait_task/read_task_result later to read answers. Task failure/cancellation remains outcome data, not a fake successful task.

For a keep-alive worker, queue the task and wait for its accepted commandId. Stop it when work is complete, not merely because guidance ended the wait:

~~~js
// Queue work and keep its instance alive if guidance returns control early.
import {spawn_agent, queue_agent, wait_tasks, stop_agent} from "noe:agent";
const worker = await spawn_agent({name: "worker", maxSteps: 4},
  undefined, {keepAlive: true});
const receipt = await queue_agent({agentId: worker.agentId,
  instruction: "Read README.md and summarize its purpose in three lines."});
const waited = await wait_tasks({tasks: [{
  agentId: worker.agentId, commandId: receipt.commandId,
}], includeAnswer: false});
if (waited.reason === "completed") await stop_agent({agentId: worker.agentId});
export default {workerAgentId: worker.agentId, ...waited};
~~~

Child agents remain owned by the parent task across run_js calls; local variables do not. Return IDs in the tool result or save them to a known file for later calls. This differs from ordinary subprocess handles, which end with the tool runtime. With no owner run, wait_tasks waits for all selected tasks; it does not listen to arbitrary run IDs. Use run_agent/wait_task when completion-only waiting is intentional, observe_agent for snapshots, steer_agent with runId for guidance, and queue_agent with agentId for future work. Read docs("noe:agent", "wait_tasks") for the complete contract.

## failures

A failed run_js result can still contain useful stdout and stderr. Read diagnostics, any available error location, and operation details before retrying. If an assumed Node API shape failed, inspect its NOE docs and the actual value/export shape rather than repeating the same assumption. For example:

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
