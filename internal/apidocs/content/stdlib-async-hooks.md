# node:async_hooks

Tracks async execution context for values that must follow work across promises and timers. AsyncLocalStorage is the main request/task-local API. The implementation is a subset of Node async hooks.

### Exports

`AsyncLocalStorage`, `AsyncResource`, `createHook`, `default`, `executionAsyncId`, `executionAsyncResource`, `triggerAsyncId`

Async context is propagated through NOE's event loop, including timers.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
