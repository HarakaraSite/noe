# node:path/win32

The explicit Windows path-flavor module. It exposes the same named function set and namespace properties as node:path; use it to process Windows path strings regardless of host.

### Exports

`_makeLong`, `basename`, `default`, `delimiter`, `dirname`, `extname`, `format`, `isAbsolute`, `join`, `normalize`, `parse`, `posix`, `relative`, `resolve`, `sep`, `toNamespacedPath`, `win32`

See node:path for method examples. This module performs Windows path-string calculations on every host and does not access files.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
