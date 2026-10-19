# node:buffer

Provides byte-oriented Buffer helpers and the Buffer global. Buffers are Uint8Array-compatible byte arrays; file APIs may also return Uint8Array directly.

### Exports

`Buffer`, `atob`, `btoa`, `default`

Buffer is installed globally as well as in this module. atob and btoa are also global APIs.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
