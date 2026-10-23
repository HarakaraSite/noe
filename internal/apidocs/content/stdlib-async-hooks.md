# node:async_hooks

### API information

Reference: `node:async_hooks`; aliases: `async_hooks`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/async_hooks.go. Reading this reference evaluates no target module and makes no provider request.

Tracks async execution context for values that must follow work across promises and timers. AsyncLocalStorage is the main request/task-local API. The implementation is a subset of Node async hooks.

### Exports

`AsyncLocalStorage`, `AsyncResource`, `createHook`, `default`, `executionAsyncId`, `executionAsyncResource`, `triggerAsyncId`

Async context is propagated through NOE's event loop, including timers.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
