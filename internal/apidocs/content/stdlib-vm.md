# node:vm

Creates JavaScript contexts and compiles or runs scripts inside the current QuickJS runtime. A context isolates its globals but is not an operating-system security boundary.

### Exports

`Script`, `compileFunction`, `constants`, `createContext`, `createScript`, `default`, `isContext`, `measureMemory`, `runInContext`, `runInNewContext`, `runInThisContext`

The module operates within NOE's existing runtime. It does not create another process or grant additional capabilities.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
