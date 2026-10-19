# node:timers/promises

Provides Promise and async-iterator timer forms. Timers use the NOE event loop and support AbortSignal in their options where applicable.

### Exports

`default`, `scheduler`, `setImmediate`, `setInterval`, `setTimeout`

The module is available because NOE installs an event loop. Use global callback timers when work should run as a callback.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
