# Standard library API policy

Approved on 2026-11-06. This is the current design policy for standard-library
work. It supersedes public naming decisions in the earlier
[integration roadmap](stdlib-roadmap.md) and its phase records. Those records
and release notes continue to describe the versions they document.

## Priorities

1. Follow Web standards where they apply. Use standard names and contracts for
   `fetch`, Request/Response, Web Streams, URL, AbortSignal, EventTarget,
   EventSource, and other implemented Web APIs. A familiar name should give a
   caller the corresponding semantics.
2. Design NOE's host APIs for clear, convenient use. Filesystem access,
   processes, host I/O, listeners, and databases do not need a Node-compatible
   facade. Publish NOE-specific contracts through explicit `noe:` modules.
3. Prefer promises for I/O, typed arrays for bytes, Web Streams for streaming,
   AbortSignal for cancellation where supported, and explicit close plus
   asynchronous disposal for owned resources. Keep synchronous helpers when
   there is a concrete use, with an explicit `Sync` suffix.
4. Reuse the existing shared implementations, grants, and resource ownership.
   A naming change must preserve Root, Cwd, ReadOnly, command/network policy,
   execution cleanup, and main/Worker behavior. Do not restore separate native
   implementations merely to restore a convenient method name.

Full Node compatibility is not a product goal. Existing useful `node:` APIs
can remain until their own migration is justified, but a Node module name
must not determine the contract of a new NOE API. Keeping selected familiar
function names or POSIX flag strings is compatible with this policy.

Web-standard globals remain the normal entry points. Host-specific helpers
and substantive extensions belong in `noe:` modules; do not rename standard
`fetch()` to `noe:fetch`. Browser-only permission and storage models need not
be imposed on ordinary host filesystem access.

## Migration rules

Explain breaking imports, signatures, return values, and behavior before each
slice. Decide whether a compatibility route is needed for that slice with the
user. Do not add aliases or adapters by default, and do not remove all Node
modules at once. Audit examples, hidden libraries, embedded instructions,
public Go consumers, types, and the remote noebot source when affected.

Keep current API references and examples on the new contract. Leave historical
release and validation records intact, linking to the newer policy where
needed. Record consumer migration instructions before the next release;
changing NOE does not authorize editing or deploying noebot.

## Sequence

| Slice | Direction | Status |
| --- | --- | --- |
| Filesystem | One asynchronous-first `noe:fs`; explicit Sync helpers, iterator watching, concise FileHandle read/write and readAt/writeAt. | Included in v2.16.0; all four old FS imports removed without adapters. See [implementation and migration](stdlib-noe-fs.md). |
| Processes and standard I/O | One `noe:process` namespace for independently selected commands, standard I/O, and the named `process` metadata/control object. Global `process` retains its current behavior. | Included in v2.16.0; all four old process/command imports removed without adapters. See [implementation and migration](stdlib-noe-process.md). |
| HTTP helpers and nonstandard extensions | Global standard Fetch/WebSocket APIs; explicit `noe:http` listener, upgrade, and custom-handshake helpers. | Included in v2.16.0 under the user's autonomous/breaking-change approval. Old HTTP imports/helper globals and constructor-header extension removed. See [migration](stdlib-noe-http.md) and separate [Fetch response-limit correction](stdlib-fetch-body-limit.md). |
| HTTP server limits and lifecycle | Request overflow errors, asynchronous idempotent close, and async disposal over the shared server backend. | Included in v2.16.0. See [Serve lifecycle and migration](stdlib-serve-lifecycle.md). |
| WebSocket opening lifecycle | Cancel pending handshakes, prevent late attachment, and validate close arguments. | Included in v2.16.0 under the autonomous approval; see [lifecycle correction](stdlib-websocket-lifecycle.md). |
| WebSocket connected lifecycle | Interrupt blocked writes and complete the close exchange with peer-derived results. | Included in v2.16.0 under the approved continuation; see [connected close](stdlib-websocket-closing.md). |
| HTTPS listeners and WSS upgrades | PEM-text `cert`/`key` options and host `ServeOptions.TLS`, over the shared HTTP/1.1 listener and socket backend. | Included in v2.17.0; implemented and verified at `6cdc02f`. See [contract and evidence](stdlib-https.md). |
| Raw TLS sockets | TLS connect/listen over the existing native Web Streams backend, with verified handshakes and independent host configuration. | Included in v2.18.0; implemented at `ba5c37d`. See [contract and verification](stdlib-raw-tls.md). |
| UDP datagrams | Explicit bind/send/receive with packet boundaries, source addresses, operation-scoped cancellation, and native ownership. | Included in v2.18.0; implemented at `8acc44b`. See [contract and verification](stdlib-udp.md). |
| Web Storage | Runtime-local sessionStorage and explicit-file localStorage using existing FS authority. | Included in v2.19.0; implemented at `dfa497e`. See [contract and verification](stdlib-web-storage.md). |
| Existing Web Crypto contracts | Private key policy, standard input/errors, native asynchronous computation, gap inventory, and real SDK interoperability. | Included in v2.20.0; no new algorithm family. See [contract, gaps, and verification](stdlib-webcrypto.md). |
| Encoding, User Timing and CustomEvent | Complete UTF-8 destination writes, real mark/measure timelines, and Event detail with standard initialization. | Included in v2.21.0. See [contract and verification](stdlib-web-basics.md). |
| UTF-8 decoding and EventTarget listeners | Streaming/error/BOM state, BufferSource/options conversion, callback identity/capture/once/passive and real abort cleanup. | Included in v2.21.0. See [contract and verification](stdlib-decoder-events.md). |
| Remaining Node modules and globals | Review Buffer/process and utility module names against actual callers and semantic differences. | Demand-driven; avoid churn with no clear usability benefit. |

The asynchronous `noe:sqlite` API already follows this direction. EventSource
remains a standard global. Expansion phases such as Web Crypto, additional
storage, and networking continue in the roadmap, subject to these priorities.
