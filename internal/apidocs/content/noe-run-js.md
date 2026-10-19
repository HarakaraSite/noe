# `noe:run_js` agent tool

`noe:run_js` is a tool available to managed agents. It is a tool name and documentation topic, not an importable module or an additional tool for ordinary JavaScript. It runs supplied code in a fresh NOE process; variables, module state, and open runtime resources do not persist to the next call. The process uses the current working directory and inherits the configured import map.

Every call starts a new process. Relative imports in the temporary module resolve from that module's source location; use a configured import-map alias or a local `.js`/`.mjs` path. HTTP imports, npm discovery, and unmapped bare package imports are unavailable. Resources created during the call belong to its runtime; owned work is recovered when the task ends or is cancelled.

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
