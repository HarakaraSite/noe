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
| Filesystem | One asynchronous-first `noe:fs`; explicit Sync helpers, iterator watching, concise FileHandle read/write and readAt/writeAt. | Implemented and verified locally, unreleased; all four old FS imports removed without adapters. See [implementation and migration](stdlib-noe-fs.md). |
| Processes and standard I/O | Move Promise/Web Streams command, lifecycle, and host-I/O APIs to a coherent `noe:process` contract. Evaluate global process metadata separately. | Next; exact grouping and breaking changes still require a slice decision. |
| HTTP helpers and nonstandard extensions | Keep standard Fetch/WebSocket APIs; review listener helpers, upgrade helpers, and constructor extensions for explicit NOE entrances such as `noe:http`. | Follow-up; no removal authorized by the filesystem decision. |
| Remaining Node modules and globals | Review Buffer/process and utility module names against actual callers and semantic differences. | Demand-driven; avoid churn with no clear usability benefit. |

The asynchronous `noe:sqlite` API already follows this direction. EventSource
remains a standard global. Expansion phases such as Web Crypto, additional
storage, and networking continue in the roadmap, subject to these priorities.
