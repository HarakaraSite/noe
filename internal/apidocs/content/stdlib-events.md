# node:events

Provides a small EventEmitter implementation and a promise helper. Listeners run synchronously in registration order when an event is emitted. This is not the full Node event utility surface.

### Exports

`EventEmitter`, `default`, `once`

An unhandled error event throws. once(emitter, name) resolves with the event arguments and rejects if an error event arrives first.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
