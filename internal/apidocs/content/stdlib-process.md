# node:process

### API information

Reference: `node:process`; aliases: `process`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed when Profile.Modules.ProcessInfo or Profile.Modules.Stdio is selected. Process metadata and the global `process` object require ProcessInfo. Named standard I/O exports require Stdio. Workers receive empty stdin (immediate EOF) and use the configured output destinations.

Public-definition source: internal/stdlib/process.go and internal/nativeio/host.go. Reading this reference evaluates no target module and makes no provider request.

The `node:process` module exposes NOE's process metadata and host standard I/O as Web Streams; it is not a full Node process object. It does not provide Node stream or EventEmitter compatibility for standard I/O.

### Exports

`default`; with Stdio, `stdin`, `stdout`, and `stderr`.

When ProcessInfo is selected, the default export is the same object as the global `process`. It reports NOE's arguments, environment, working directory, process ID, and process controls. With Stdio alone, the default is an object containing only `stdin`, `stdout`, and `stderr`; no global `process` or process metadata is installed. With both capabilities selected, the default/global object's standard stream properties are the same Web Stream instances as the named exports. Process metadata is not available as named exports: import the default or use the global `process` when ProcessInfo is selected.

With ProcessInfo selected, process.argv contains the executable, current source name, then script arguments. process.env is a JavaScript snapshot; editing it does not change the host environment. process.exit() ends the current NOE run.

## cwd

Syntax: process.cwd() -> string. It returns the working directory captured for this NOE run.

~~~js
import process from "node:process";

console.log(process.cwd());
~~~

## env

process.env is an object containing the environment variables present when NOE created the runtime. It can be read or changed as a JavaScript object, but changing it does not mutate the parent shell's environment.

~~~js
import process from "node:process";

console.log(process.env.HOME ?? "(unset)");
~~~

NOE passes its configured environment to child programs; set a child call's env option when that call needs a different environment.

## argv

process.argv has the shape [executable, source, ...scriptArgs]. For noe run script.js one, the source is script.js and the remaining entry is one.

~~~js
import process from "node:process";

console.log(process.argv.slice(2));
~~~

## exit

Syntax: process.exit(code = 0). It requests termination of the current NOE runtime with the numeric exit code. In the main runtime NOE records the requested code and closes its runtime; inside a Worker it ends that Worker.

~~~js
import process from "node:process";

if (!process.env.API_TOKEN) process.exit(2);
~~~

This ends the run and its owned runtime resources. Set process.exitCode when the script should finish naturally with a non-zero code.

## stdin

Availability: Requires Profile.Modules.Stdio. `stdin` is a `ReadableStream<Uint8Array>` for the configured host input. It is demand-driven; importing it does not start reading. Workers receive immediate EOF.

~~~js
import { stdin } from "node:process";

const text = await new Response(stdin).text();
~~~

Use a stream reader or async iteration to process bytes incrementally. A standard input stream can be consumed once.

## stdout

Availability: Requires Profile.Modules.Stdio. `stdout` is a `WritableStream<Uint8Array>` for the configured host output. Writes are asynchronous and complete when the host writer accepts the bytes.

~~~js
import { stdout } from "node:process";

const writer = stdout.getWriter();
try {
  await writer.write(new TextEncoder().encode("hello\n"));
} finally {
  writer.releaseLock();
}
~~~

When ProcessInfo is also selected, `process.stdout` is the same stream instance. Closing this stream endpoint does not close the caller's borrowed host writer.

## stderr

Availability: Requires Profile.Modules.Stdio. `stderr` is a `WritableStream<Uint8Array>` for the configured host error output. It has the same byte, ownership, and lifecycle behavior as `stdout`.

When ProcessInfo is also selected, `process.stderr` is the same stream instance. Use `TextEncoder` when writing text. For stream connections, use Web Streams `pipeTo`; see the `node:stream/web` reference.

### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
