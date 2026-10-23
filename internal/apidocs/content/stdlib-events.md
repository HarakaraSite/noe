# node:events

### API information

Reference: `node:events`; aliases: `events`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/node.go. Reading this reference evaluates no target module and makes no provider request.

Provides a small EventEmitter implementation and a promise helper. Listeners run synchronously in registration order when an event is emitted. This is not the full Node event utility surface.

### Exports

`EventEmitter`, `default`, `once`

An unhandled error event throws. once(emitter, name) resolves with the event arguments and rejects if an error event arrives first.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
