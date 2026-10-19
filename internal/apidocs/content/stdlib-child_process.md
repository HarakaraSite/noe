# node:child_process

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

Syntax: execFileSync(file, args = [], options = {}) -> string. It waits for the program and returns stdout as text. Supported options are cwd, env, and input; a non-zero exit or start failure throws. Unlike spawnSync, this form does not return a status object.

~~~js
import { execFileSync } from "node:child_process";

const branch = execFileSync("git", ["branch", "--show-current"]).trim();
console.log(branch);
~~~

## exec

Syntax: exec(command, options = {}) -> Promise<{stdout, stderr}>. The command string is passed to /bin/sh -c on Unix-like systems or cmd.exe on Windows. It has the same captured-output, rejection, encoding, and runtime-lifetime behavior as execFile.

~~~js
import { exec } from "node:child_process";

const { stdout } = await exec("printf '%s\\n' ready");
console.log(stdout.trim());
~~~

Use execFile with an argument array when shell syntax is unnecessary.

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
