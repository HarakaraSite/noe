# NOE standard-library modules

### API information

Reference: `stdlib`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: This is the complete reference, not a report of the current runtime. Globals and host operations depend on Profile.Modules; ECMAScript built-ins come from the pinned engine.

Public-definition source: internal/stdlib/stdlib.go, internal/jsexec/profile.go. Reading this reference evaluates no target module and makes no provider request.

NOE exposes the supported built-in Node-style and NOE-specific modules listed below. Supported Node-style modules use their canonical `node:` IDs and selected bare aliases; NOE-specific modules use `noe:` IDs. A reserved `node:` prefix does not make other Node modules available. Each module page lists its actual named exports, including default. Details are included only for documented members.

Use the standard global `fetch()` for HTTP requests. Import NOE-specific listener and WebSocket helpers such as `serve`, `createWebSocket`, and `upgradeWebSocket` from `noe:http`; the module is installed only when Serve or WebSockets is selected. Use `noe:fs` for filesystem APIs.

| Module | Summary |
| --- | --- |
| node:assert | Assertions for values and errors. |
| node:async_hooks | Async execution context and hooks. |
| node:buffer | Byte buffers and base64 helpers. |
| node:crypto | Hashes, randomness, key derivation, and limited Web Crypto. |
| node:dns | System resolver lookup functions. |
| node:dns/promises | Promise-oriented system resolver API. |
| node:events | EventEmitter and once. |
| noe:fs | Filesystem operations, handles, watching, and Web Streams. |
| noe:process | Captured commands, owned child processes, selected process metadata, and host standard I/O Web Streams. |
| node:os | Selected host information. |
| node:path | POSIX path operations plus both path namespaces. |
| node:path/posix | Explicit POSIX path operations. |
| node:path/win32 | Explicit Windows path operations. |
| node:querystring | Legacy query-string parsing and serialization. |
| node:stream/web | Web Streams constructors. |
| node:string_decoder | UTF-8 decoding across byte chunks. |
| node:timers | Callback timers. |
| node:timers/promises | Promise and async-iterator timers. |
| node:url | Web URL types and selected helpers. |
| node:util | Selected formatting and conversion helpers. |
| node:vm | QuickJS contexts and script execution. |
| node:worker_threads | Worker runtimes and message ports. |
| node:zlib | Compression and decompression. |

### Runtime behavior

The local command-line runtime selects these modules through its execution profile. A Go host selects filesystem, process metadata, standard I/O, commands, DNS, fetch, serving, WebSockets, and Workers independently; `noe:docs` remains a full catalog even when a capability is disabled. `noe:process` is installed when Commands, ProcessInfo, or Stdio is selected; with none of them it is absent. Commands select the six command exports, ProcessInfo selects the named `process` export and the global process object, and Stdio selects the named stream exports. The default namespace contains exactly the selected named exports, so process metadata is under `default.process`. The named process export is the same object as global `process`; its stream properties are present when Stdio is also selected. `noe:fs` requires the filesystem capability and uses the same filesystem view for its async, sync, handle, watch, and stream operations. `execFile` already returns a promise. `noe:fs.createReadStream` and `noe:fs.createWriteStream` return Web Streams, not Node streams. Workers load through the selected execution's loader factory, whose contract is documented in node:worker_threads.

Use `open` from `noe:fs` for reads and writes through an open file handle, and `noe:sockets` for raw TCP/Unix byte streams. HTTP and WebSocket APIs handle requests and messages, not raw byte-stream sockets. The WebSockets capability selects the WebSocket client and server APIs; it is distinct from NOE's raw network socket module. Fetch also enables the global EventSource API for SSE subscriptions. Use the NOE-specific `spawn` export from `noe:process` for owned child-process handles. See globals for APIs installed directly on globalThis.

### Source basis

NOE v2.19.0 and later installs the global Storage interface and
runtime-local sessionStorage. The persistent localStorage global requires
explicit `Modules.LocalStorage` selection and FS authority; CLI run/eval use
`--local-storage path`. Earlier releases do not include Web Storage.
See globals for synchronous methods, Worker sharing, and file limitations.

NOE-maintained implementation: internal/stdlib. Upstream origin: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib, recorded in internal/stdlib/UPSTREAM.md. The JavaScript engine remains the go-quickjs dependency in go.mod. The public module allowlist is internal/modulemap/builtins.go; NOE's selected capabilities and Worker loader are in internal/runtime/stdlib.go.
