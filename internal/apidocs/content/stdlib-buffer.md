# node:buffer

### API information

Reference: `node:buffer`; aliases: `buffer`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/node.go. Reading this reference evaluates no target module and makes no provider request.

Provides byte-oriented Buffer helpers and the Buffer global. Buffers are Uint8Array-compatible byte arrays; file APIs may also return Uint8Array directly.

### Exports

`Buffer`, `atob`, `btoa`, `default`

Buffer is installed globally as well as in this module. atob and btoa are also global APIs.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
