# node:path/posix

The explicit POSIX path-flavor module. It exposes the same named function set and namespace properties as node:path; use it when POSIX separators and roots must be selected regardless of host.

### Exports

`_makeLong`, `basename`, `default`, `delimiter`, `dirname`, `extname`, `format`, `isAbsolute`, `join`, `normalize`, `parse`, `posix`, `relative`, `resolve`, `sep`, `toNamespacedPath`, `win32`

See node:path for details of join, resolve, dirname, basename, and extname. This module does not access the filesystem.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
