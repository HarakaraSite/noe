# node:util

Provides selected formatting, inspection, type, inheritance, and callback/promise conversion helpers. This is a curated subset, not every Node utility.

### Exports

`callbackify`, `default`, `deprecate`, `format`, `inherits`, `inspect`, `isDeepStrictEqual`, `promisify`, `types`

inspect is also used by the runtime console. Behavior follows the pinned go-quickjs implementation.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
