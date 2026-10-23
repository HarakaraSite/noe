# node:string_decoder

### API information

Reference: `node:string_decoder`; aliases: `string_decoder`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/extras.go. Reading this reference evaluates no target module and makes no provider request.

Decodes byte chunks without losing a UTF-8 character split across chunk boundaries. The implementation supports UTF-8 only.

### Exports

`StringDecoder`, `default`

A non-UTF-8 encoding name throws TypeError. Use TextDecoder for general text conversion.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
