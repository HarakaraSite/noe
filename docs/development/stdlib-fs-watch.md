# Filesystem watching implementation

[Development guide](README.md) · [Standard library roadmap](stdlib-roadmap.md)

2026-11-06, based on NOE v2.14.0. Phase 5 adds nonrecursive file and directory
watching through the existing filesystem modules. This is an additive API;
no preexisting import, method, result, or noebot caller needs migration.
Implemented and locally verified; not released.

## API and shared backend

`node:fs.watch` / `fs.watch` return an EventEmitter watcher immediately. The
listener receives `(eventType, filename)` through the `change` event. The
`error` event reports a terminal backend failure; `close` signals normal or
error shutdown. `close()` stops watching synchronously and returns undefined.
`ref()` and `unref()` return the watcher and toggle execution liveness.
`[Symbol.asyncDispose]()` calls close and waits for its close notification.

`node:fs/promises.watch`, `fs/promises.watch`, and the corresponding
`fs.promises` and default namespaces share a function. It returns an owned
async iterator immediately; native registration starts at its first `next()`.
Argument validation is immediate. Values are `{eventType, filename}`;
`return()` and `for await` break release the watch. NOE adds an explicit
`close() -> Promise<void>` and `Symbol.asyncDispose` to this iterator.

Both forms share the Go watch host and fsnotify v1.10.1. Its BSD-licensed,
CGO-free backend supports the six existing distribution targets. The new
public dependency is resolved through normal Go module distribution. No
engine fork, NOE-specific filesystem module, or polling loop is introduced.

| Contract | Behavior |
| --- | --- |
| Paths | Nonempty strings without NUL; the shared filesystem Root and frozen Cwd apply. |
| ReadOnly | Watching is observation and remains available. |
| Notifications | Create/remove/rename become `rename`; write/chmod become `change`. Rename takes precedence for combined native flags. |
| Filenames | Basenames, never physical absolute paths. UTF-8 strings by default, Buffer with `encoding:"buffer"`, or null if unavailable. |
| Lifetime | Persistent by default, including time between iterator reads. Callback watcher ref/unref and `persistent:false` change liveness. |
| Queue | At most `maxQueue` events waiting for reads, default 2048; positive safe integer. No eager allocation of the entire requested limit. |
| Overflow | `ignore` (default) drops excess events and reports their count when consumed; `error` terminates with `ERR_FS_WATCH_QUEUE_OVERFLOW`. Native OS overflow is `ERR_FS_EVENT_OVERFLOW`. |
| Callback cancellation | Actual signal abortion closes without an error event. A pre-aborted signal registers no native watch. |
| Iterator cancellation | Pending reads reject with `AbortError`, `ABORT_ERR`, and exact `signal.reason` in `cause`; subsequent reads are done. |
| Explicit close | Idempotent; concurrent pending iterator reads become done. Queued events are discarded. |
| Teardown | Runtime and execution-scope cleanup close native watchers and join readers without running JS. Loop/context cancellation initiates the same cleanup. |

`AbortSignal.any` validates and normalizes the optional signal, reusing the
previous phase's actual-abort propagation. Synthetic abort events do not stop
watching. The callback adapter reports listener exceptions through the
selected event loop, including low-level embeddings without an unhandled
promise-rejection handler. Callback watches require a Loop and the pure
`node:events` module. Promise iteration needs neither; byte filenames need
`node:buffer`, and signals need the web APIs.

## Scope and operating system behavior

The first implementation observes a file or a directory's immediate entries.
`recursive:true`, `ignore` filters, and `throwIfNoEntry:false` are rejected with
`ERR_NOT_SUPPORTED`. URL/Buffer paths and alternate text encodings are not
implemented. The full Node.js filesystem API is not claimed.

Notifications are invalidation hints, not a durable change log. Events may
coalesce or arrive more than once, and OS/backend/filesystem behavior varies.
Watch the parent directory when an editor or application replaces a file
atomically; observing the original file object does not promise automatic
attachment to a replacement. There is no recursive traversal of child links.
The selected target follows the shared backend's initial path/link policy,
as existing open-file APIs do. Event and error conversion do not expose the
host root. Existing backend path-replacement race limitations are unchanged.

Primary references are the official Node.js
[watch APIs and caveats](https://nodejs.org/api/fs.html#fswatchfilename-options-listener),
[Promise iterator API](https://nodejs.org/api/fs.html#fspromiseswatchfilename-options),
and the [fsnotify v1.10.1 documentation](https://pkg.go.dev/github.com/fsnotify/fsnotify@v1.10.1).
They guide the supported subset; the explicit iterator close and disposal
helpers are NOE additions.

## Validation record

Validation passed:

- Affected full unit suites: internal stdlib/nativeio/noestdlib/jsexec/runtime,
  CLI, API catalog, and public stdlib. The external-consumer unit was covered
  by the independent consumer checks below rather than run twice.
- All watch and Go-profile tests under race on the final source, including
  real create/write/rename/remove events, UTF-8/Buffer names, Root/Cwd/ReadOnly,
  outward-link refusal, queue overflow policies, ref/unref, pending-read close,
  callback errors, FIFO settlement, and runtime/loop/scope teardown.
- CLI run/eval, Worker, event recording, fixture-agent run_js, and natural exit
  with unreferenced callback/iterator watches. These use local fixtures only.
- Separate Go consumers with a local replacement and a self-contained module
  artifact both built and ran. The artifact resolved fsnotify v1.10.1 through
  normal public module resolution. Consumer builds passed all six targets.
- Positive/negative TypeScript declarations, API catalog tests, JS lint/syntax,
  go vet including the new E2E tests, and whitespace checks.
- Independent bounded review and one re-review of the native lifetime and JS
  adapters. No actionable findings remain.

Review fixes removed unsafe runtime-hook unregistration from background cleanup,
kept terminal resources owned until delivery and reader completion, moved JS
promise settlement outside the resource mutex, preserved in-flight and queued
reads during reentrant close, and routed callback failures through the loop.
A real-iterator regression closes from an inherited then getter with two reads
pending; the native FIFO regression observes settlement order as well as values.

The initial fixture-agent test incorrectly required empty stderr, although the
existing agent emits progress records there. The tool execution itself succeeded;
the corrected assertion and final fixture-agent run passed. Production agent
logging was unchanged.

Evidence is retained under `/tmp/noe-fs-watch-7lk4w_xp/results.json`, with full
unit output, initial/final E2E logs, final watch/profile race results, static
checks, consumer source manifests, and six-target build logs. The final
close-settlement-order follow-up was checked by the final race suite and both
external-consumer paths; existing cross-build results were reused because that
follow-up changed no platform code or dependency/API types. Native execution
was Linux amd64; other targets received build verification, not native OS tests.
Installed binaries, actual noebot repositories, and running services were not
changed. No provider requests or login flows were needed.
