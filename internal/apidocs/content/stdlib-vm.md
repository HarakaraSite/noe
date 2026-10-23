# node:vm

### API information

Reference: `node:vm`; aliases: `vm`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/vm.go. Reading this reference evaluates no target module and makes no provider request.

Creates JavaScript contexts and compiles or runs scripts inside the current QuickJS runtime. A context isolates its globals but is not an operating-system security boundary.

### Exports

`Script`, `compileFunction`, `constants`, `createContext`, `createScript`, `default`, `isContext`, `measureMemory`, `runInContext`, `runInNewContext`, `runInThisContext`

The module operates within NOE's existing runtime. It does not create another process or grant additional capabilities.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
