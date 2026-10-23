# node:util

### API information

Reference: `node:util`; aliases: `util`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/node.go. Reading this reference evaluates no target module and makes no provider request.

Provides selected formatting, inspection, type, inheritance, and callback/promise conversion helpers. This is a curated subset, not every Node utility.

### Exports

`callbackify`, `default`, `deprecate`, `format`, `inherits`, `inspect`, `isDeepStrictEqual`, `promisify`, `types`

inspect is also used by the runtime console. Behavior follows the pinned go-quickjs implementation.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
