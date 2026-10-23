# node:child_process

### API information

Reference: `node:child_process`; aliases: `child_process`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Commands.

Public-definition source: internal/stdlib/exec.go. Reading this reference evaluates no target module and makes no provider request.

Runs operating-system programs to completion and returns captured output. execFile invokes a program directly; exec and execSync pass a command string through the platform shell. There is no streaming spawn or asynchronous callback API.

### Exports

`default`, `exec`, `execFile`, `execFileSync`, `execSync`, `spawnSync`

In NOE, child process calls can run local programs. Use noe:process for NOE-managed process handles and their lifecycle controls. Use HTTP/WebSocket APIs for web traffic and noe:sockets for raw TCP/Unix byte streams.

## execFile

Syntax: execFile(file, args = [], options = {}) -> Promise<{stdout, stderr}>. file is an executable name or path; args is an argument array. Supported options are cwd, env, input, and encoding. Output is text unless encoding: "buffer" requests Uint8Array values. The promise rejects with an Error carrying code, stdout, and stderr on start failure, timeout, runtime shutdown, or a non-zero exit. NOE uses a 32 MiB capture limit per output stream and does not set a command timeout.

~~~js
import { execFile } from "node:child_process";

const { stdout } = await execFile("git", ["rev-parse", "--show-toplevel"]);
console.log(stdout.trim());
~~~

execFile already returns a promise; do not wrap it with promisify or pass a Node-style callback. Closing the NOE runtime stops a running child.

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

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
