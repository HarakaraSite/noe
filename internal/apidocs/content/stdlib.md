# NOE standard-library modules

### API information

Reference: `stdlib`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: This is the complete reference, not a report of the current runtime. Globals and host operations depend on Profile.Modules; ECMAScript built-ins come from the pinned engine.

Public-definition source: internal/stdlib/stdlib.go, internal/jsexec/profile.go. Reading this reference evaluates no target module and makes no provider request.

NOE exposes the 26 built-in Node-style modules listed below from its NOE-maintained standard library, based on go-quickjs v0.27.0. Each module is registered under its canonical node: ID and bare alias; both names resolve to the same implementation. A reserved node: prefix does not make other Node modules available. Each module page lists the actual named exports, including default. Details are included only for documented members.

For HTTP, prefer `import { fetch, serve } from "noe:http"`. That NOE module shares the existing global functions and Web types; the Node-style module list below is unchanged.

| Module | Summary |
| --- | --- |
| node:assert | Assertions for values and errors. |
| node:async_hooks | Async execution context and hooks. |
| node:buffer | Byte buffers and base64 helpers. |
| node:child_process | Synchronous and promise-based captured child commands. |
| node:crypto | Hashes, randomness, key derivation, and limited Web Crypto. |
| node:dns | System resolver lookup functions. |
| node:dns/promises | Promise-oriented system resolver API. |
| node:events | EventEmitter and once. |
| node:fs | File operations and Web Streams. |
| node:fs/promises | Promise-based file operations. |
| node:http | HTTP serve backed by Request/Response. |
| node:os | Selected host information. |
| node:path | POSIX path operations plus both path namespaces. |
| node:path/posix | Explicit POSIX path operations. |
| node:path/win32 | Explicit Windows path operations. |
| node:process | NOE process object. |
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

The local command-line runtime selects these modules through its execution profile. A Go host selects filesystem, process/commands, DNS, fetch, serving, WebSockets, and Workers independently; `noe:docs` remains a full catalog even when a capability is disabled. `node:fs` and `node:fs/promises` require the filesystem capability. `node:child_process` and `noe:process` require the command capability. `noe:fs-handle` uses the same filesystem view. `child_process.execFile` already returns a promise. `fs.createReadStream` and `fs.createWriteStream` return Web Streams, not Node streams. Workers load through the selected execution's loader factory, whose contract is documented in node:worker_threads.

Use noe:fs-handle for positional file access and noe:sockets for raw TCP/Unix byte streams. HTTP and WebSocket APIs handle requests and messages, not raw byte-stream sockets. The WebSockets capability selects the WebSocket client and server APIs; it is distinct from NOE's raw network socket module. Use noe:process for NOE-managed process handles. See globals for APIs installed directly on globalThis.

### Source basis

NOE-maintained implementation: internal/stdlib. Upstream origin: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib, recorded in internal/stdlib/UPSTREAM.md. The JavaScript engine remains the go-quickjs dependency in go.mod. The public module allowlist is internal/modulemap/builtins.go; NOE's selected capabilities and Worker loader are in internal/runtime/stdlib.go.
