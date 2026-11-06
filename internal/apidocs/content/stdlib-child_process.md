# node:child_process

### API information

Reference: `node:child_process`; aliases: `child_process`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Commands.

Public-definition source: internal/stdlib/exec.go, internal/nativeio/host.go, internal/nativeio/js/streams.js, and their composition in internal/noestdlib/noestdlib.go. Reading this reference evaluates no target module and makes no provider request.

Runs captured commands and starts owned child processes. execFile invokes a program directly; exec and execSync pass a command string through the platform shell. `spawn` is an NOE-specific API that starts a live child and returns Web Streams; it is not the Node.js EventEmitter or Node streams API.

### Exports

`default`, `exec`, `execFile`, `execFileSync`, `execSync`, `spawn`, `spawnSync`

The default export object includes the same functions, including `spawn`.

### Type exports

`ExecOptions`, `ExecResult`, `SpawnSyncResult`, `ChildProcess`, `SpawnOptions`

`spawn(command, args = [], options = {}) -> Promise<ChildProcess>` starts a local program directly without invoking a shell. It is an NOE-specific extension to this module. The returned handle uses Web Streams of `Uint8Array`; it does not expose Node.js child-process events or Node streams. Use HTTP/WebSocket APIs for web traffic and noe:sockets for raw TCP/Unix byte streams.

## execFile

Syntax: execFile(file, args = [], options = {}) -> Promise<{stdout, stderr}>. file is an executable name or path; args is an argument array. Supported options are cwd, env, input, and encoding. Output is text unless encoding: "buffer" requests Uint8Array values. The promise rejects with an Error carrying code, stdout, and stderr on start failure, timeout, runtime shutdown, or a non-zero exit. NOE uses a 32 MiB capture limit per output stream and does not set a command timeout.

~~~js
import { execFile } from "node:child_process";

const { stdout } = await execFile("git", ["rev-parse", "--show-toplevel"]);
console.log(stdout.trim());
~~~

execFile already returns a promise; do not wrap it with promisify or pass a Node-style callback. Closing the NOE runtime stops a running child.

Omitting env uses the host-configured command environment. Supplying env replaces that environment; it does not merge individual variables. Executable lookup also uses the selected environment's PATH. To add or override a variable while preserving the runtime's visible environment, copy process.env first. For example, with Go installed:

~~~js
import {execFile} from "node:child_process";
import process from "node:process";

const {stdout} = await execFile("go", ["env", "CGO_ENABLED"], {
  env: {...process.env, CGO_ENABLED: "0"},
});
console.log(stdout.trim()); // 0
~~~

Using only env: {CGO_ENABLED: "0"} also removes PATH and the other inherited variables, so a command such as go may no longer be found.

## execFileSync

Syntax: execFileSync(file, args = [], options = {}) -> string. It waits for the program and returns stdout as text. Supported options are cwd, env, and input. A non-zero exit, start failure, or host-configured timeout throws an Error with code, stdout, and stderr, using the existing error message. Unlike spawnSync, this form does not return a status object.

The error outputs are strings, including empty strings when nothing was captured. Both the success result and error outputs remain text even if encoding: "buffer" is supplied; synchronous execution does not support buffer encoding. Capture is limited to 32 MiB per output stream in NOE. code is the process exit code on a normal exit and -1 on a timeout or some abnormal terminations. As with execFile, a failure before the process starts currently has code: 0; this does not mean success. Runtime shutdown still interrupts execution.

~~~js
import { execFileSync } from "node:child_process";

const branch = execFileSync("git", ["branch", "--show-current"]).trim();
console.log(branch);

try {
  execFileSync("git", ["show", "missing-ref"]);
} catch (error) {
  console.error("code:", error.code);
  console.error("stdout:", error.stdout);
  console.error("stderr:", error.stderr);
}
~~~

## exec

Syntax: exec(command, options = {}) -> Promise<{stdout, stderr}>. The command string is passed to /bin/sh -c on Unix-like systems or cmd.exe on Windows. It has the same captured-output, rejection, encoding, and runtime-lifetime behavior as execFile.

~~~js
import { exec } from "node:child_process";

const { stdout } = await exec("printf '%s\\n' ready");
console.log(stdout.trim());
~~~

Use execFile with an argument array when shell syntax is unnecessary.

## execSync

Syntax: execSync(command, options = {}) -> string. The command string is passed to the same platform shell as exec. Success returns stdout as text; failures throw an Error with code, stdout, and stderr, with the same options, text-only output, capture limits, and failure-code conventions as execFileSync.

~~~js
import { execSync } from "node:child_process";

try {
  console.log(execSync("git status --short").trim());
} catch (error) {
  console.error(error.message, error.code, error.stdout, error.stderr);
}
~~~

Use execFileSync with an argument array when shell syntax is unnecessary.

## spawnSync

Syntax: spawnSync(file, args = [], options = {}) -> {status, stdout, stderr, error?}. It waits for completion. For a started process, status reports its exit code (or -1 for some abnormal terminations). In this pinned version, a launch failure can return status: 0 with an error property. Always inspect error before interpreting status; status: 0 alone does not prove success. Output is text unless encoding: "buffer" requests Uint8Array values.

~~~js
import { spawnSync } from "node:child_process";

const result = spawnSync("git", ["status", "--short"]);
if (result.error) throw result.error;
console.log(result.stdout);
~~~

## spawn

`spawn(command, args = [], options = {}) -> Promise<ChildProcess>` requires a nonempty command and an array of string arguments. Neither may contain NUL. It starts the executable directly and does not invoke a shell. The promise resolves after startup succeeds; start failures reject it.

Options:

| Option | Default | Meaning |
| --- | --- | --- |
| `stdin`, `stdout`, `stderr` | `"pipe"` | Each accepts `"pipe"`, `"inherit"`, or `"ignore"`. A pipe returns a Web Stream; inherited or ignored streams return `null`. |
| `cwd` | current working directory | Child working directory. |
| `env` | the Profile's default command environment | When `Profile.Environment.Env` is unset, that default inherits the host-configured environment. Omitting `options.env` inherits the selected default. An explicit object replaces it; entries with a `null` value are omitted. |

All three streams default to pipes. Consume stdout and stderr concurrently when both are piped, or choose `inherit`/`ignore` for an unused stream. Convert text explicitly with `TextEncoder` and `TextDecoder`.

~~~js
import { spawn } from "node:child_process";

const child = await spawn(process.argv[0], ["eval", "console.log('child output')"], {
  stdout: "pipe",
  stderr: "inherit",
});
try {
  const output = await new Response(child.stdout).text();
  const status = await child.exited;
  console.log(output.trim(), status.code);
} finally {
  await child.close();
}
~~~

The result is `{pid, stdin, stdout, stderr, exited, kill, close}`. `exited` resolves to `{code, signal}` after the child exits, including nonzero exit codes. A nonzero exit does not reject `exited`; supervision failures do. Output EOF and process exit are separate, so read piped output through EOF when its final bytes matter. If both output streams are piped, drain them concurrently to avoid filling a pipe and blocking the child.

## ChildProcess.pid

`pid` is the operating-system process ID of the started child command, not an internal supervisor process.

## ChildProcess.stdin

When `stdin: "pipe"`, this is a `WritableStream<Uint8Array>`. Write bytes with a writer or `pipe`; closing the stream sends EOF to the child. It is `null` when `stdin` is inherited or ignored. Aborting the writable closes the child's input side.

## ChildProcess.stdout

When `stdout: "pipe"`, this is a `ReadableStream<Uint8Array>` that ends at child EOF. Read it while the process runs. It is `null` when `stdout` is inherited or ignored. Cancelling the stream closes that read end.

## ChildProcess.stderr

When `stderr: "pipe"`, this is a `ReadableStream<Uint8Array>` that ends at child EOF. Drain it concurrently with stdout if both are piped. It is `null` when `stderr` is inherited or ignored. Cancelling the stream closes that read end.

## ChildProcess.exited

This Promise resolves to `{code, signal}` when process supervision finishes. `code` is the ordinary exit code, including nonzero values; `signal` identifies signal termination where the OS reports one. The other field may be `null` when it does not apply. Await it to observe completion; it does not terminate the child.

## ChildProcess.kill

`await child.kill(signal = "SIGTERM")` sends a stop request to the owned process group or Windows Job. This requests termination; use `exited` to wait for it. Signal and termination behavior depend on the operating system. On Windows, termination uses the Job rather than converting an exit code into a POSIX signal.

## ChildProcess.close

`await child.close()` is idempotent and performs forced cleanup. It closes owned pipes, stops a process that is still running, waits for process supervision and stream pumps, and releases the handle. Unread output may be discarded. Call it in `finally`; runtime shutdown also reclaims owned children. An inherited-output forwarding failure is reported by close.

The child and its pipes belong to the execution that starts them, including Worker executions, and cannot outlive that runtime. POSIX uses one Go supervision process per spawned child; if the owning run_js runtime is forcibly stopped, the supervisor detects the owner's pipe closing and stops the process group. This is not a detached-process API.

## ChildProcess[Symbol.asyncDispose]

`child[Symbol.asyncDispose]() -> Promise<void>` calls NOE's `close()` and waits
for cleanup. With `await using child = await spawn(...)`, leaving the block
closes owned pipes and stops/reaps a running child. This is forced cleanup;
consume output and await `child.exited` first when normal completion matters.
Explicit `close()` and execution-owned cleanup remain available.

### Source basis

NOE-maintained implementation: captured commands are in internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib; live process handles are in internal/nativeio. internal/noestdlib composes both implementations into one module. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
