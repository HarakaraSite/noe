# node:string_decoder

Decodes byte chunks without losing a UTF-8 character split across chunk boundaries. The implementation supports UTF-8 only.

### Exports

`StringDecoder`, `default`

A non-UTF-8 encoding name throws TypeError. Use TextDecoder for general text conversion.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
