# node:timers

Exports callback-style timer functions. NOE also installs setTimeout, setInterval, clearTimeout, and clearInterval as globals. Timers run on the runtime event loop.

### Exports

`clearImmediate`, `clearInterval`, `clearTimeout`, `default`, `setImmediate`, `setInterval`, `setTimeout`

setImmediate is implemented as a zero-delay timeout. These module exports do not add a global setImmediate.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
