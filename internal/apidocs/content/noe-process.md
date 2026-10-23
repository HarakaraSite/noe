# `noe:process`

### API information

Public exports: `spawn`.

Reference: `noe:process`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Commands. Processes belong to the execution that starts them, including Worker executions.

Public-definition source: internal/nativeio/host.go, internal/nativeio/js/streams.js. Reading this reference evaluates no target module and makes no provider request.

`noe:process` exports `spawn(command, args?, options?)`. It starts a child command directly; it does not invoke a shell. Its standard streams use Web Streams of `Uint8Array`, not Node.js child-process stream events. The module is available in the main runtime and Workers. A child process and its pipes are owned by the creating runtime and cannot outlive it.

## spawn

```js
import { spawn } from "noe:process";
```

`spawn(command, args = [], options = {}) -> Promise<ChildProcess>` requires a nonempty command and an array of string arguments. Neither may contain NUL. `options` supports:

| Option | Default | Meaning |
| --- | --- | --- |
| `stdin`, `stdout`, `stderr` | `"pipe"` | Each accepts `"pipe"`, `"inherit"`, or `"ignore"`. A pipe returns a Web Stream; inherited or ignored streams return `null`. |
| `cwd` | current working directory | Child working directory. |
| `env` | current environment | String values override inherited variables; `null` removes a variable. |

All three streams default to pipes. Consume stdout and stderr concurrently when both are piped, or choose `inherit`/`ignore` for an unused stream. Convert text explicitly with `TextEncoder` and `TextDecoder`.

The result is `{pid, stdin, stdout, stderr, exited, kill, close}`. `exited` resolves to `{code, signal}` after the child exits, including nonzero exit codes. Invalid arguments throw `TypeError`; OS start, I/O, and signal failures reject the corresponding Promise. A child that exits unsuccessfully does not make `exited` reject merely because its exit code is nonzero.

```js
import { spawn } from "noe:process";

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
```

## ChildProcess.pid

`pid` is the operating-system process ID of the started child command.

## ChildProcess.stdin

When `stdin: "pipe"`, this is a `WritableStream<Uint8Array>`. Write bytes with a writer or `pipe`; closing the stream sends EOF to the child. It is `null` when `stdin` is inherited or ignored.

## ChildProcess.stdout

When `stdout: "pipe"`, this is a `ReadableStream<Uint8Array>` that ends at child EOF. Read it while the process runs. It is `null` when `stdout` is inherited or ignored.

## ChildProcess.stderr

When `stderr: "pipe"`, this is a `ReadableStream<Uint8Array>` that ends at child EOF. Drain it concurrently with stdout if both are piped. It is `null` when `stderr` is inherited or ignored.

## ChildProcess.exited

This Promise resolves to `{code, signal}` when process supervision finishes. `code` is the ordinary exit code, including nonzero values; `signal` identifies signal termination where the OS reports one. The other field may be `null` when it does not apply. Await it to observe completion; it does not terminate the child.

## ChildProcess.kill

`await child.kill(signal = "SIGTERM")` sends the named signal to the owned process scope. The default is `SIGTERM`. Signal support and behavior depend on the operating system. This requests termination; use `close()` when the handle must be fully recovered.

## ChildProcess.close

`await child.close()` is idempotent. It closes owned pipes, stops a process that is still running, waits for process supervision and stream pumps, and releases the handle. Call it in `finally`; runtime shutdown also reclaims owned children.
