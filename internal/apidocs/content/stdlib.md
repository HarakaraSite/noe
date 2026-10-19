# NOE standard-library modules

NOE exposes exactly the 26 built-in modules listed below from the pinned go-quickjs v0.27.0 standard library. Each module is registered under its canonical node: ID and bare alias; both names resolve to the same implementation. A reserved node: prefix does not make other Node modules available. Each module page lists the actual named exports, including default. Details are included only for documented members.

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

NOE installs these modules using its stdlib.Config. Filesystem and process access are enabled for the local command-line runtime; fetch, HTTP serving, and WebSockets are also enabled. child_process.execFile already returns a promise. fs.createReadStream and fs.createWriteStream return Web Streams, not Node streams. Workers load through NOE's local loader, whose limitations are documented in node:worker_threads.

Use noe:fs-handle for positional file access and noe:sockets for raw TCP/Unix byte streams. HTTP and WebSocket APIs handle requests and messages, not raw byte-stream sockets. In go-quickjs, Config.Sockets configures WebSockets; it is distinct from NOE's noe:sockets raw network API. Use noe:process for NOE-managed process handles. See globals for APIs installed directly on globalThis.

### Source basis

Dependency version: github.com/go-quickjs/go-quickjs@v0.27.0 from go.mod. The public module allowlist is internal/modulemap/builtins.go; NOE's selected capabilities and Worker loader are in internal/runtime/stdlib.go.
