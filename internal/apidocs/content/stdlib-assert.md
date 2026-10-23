# node:assert

### API information

Reference: `node:assert`; aliases: `assert`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/node.go. Reading this reference evaluates no target module and makes no provider request.

Synchronous assertion helpers for checking values and thrown errors. Failed assertions throw. This small Node-style subset does not add process or filesystem access.

### Exports

`default`, `AssertionError`, `deepEqual`, `deepStrictEqual`, `doesNotMatch`, `doesNotReject`, `doesNotThrow`, `equal`, `fail`, `match`, `notDeepStrictEqual`, `notEqual`, `notStrictEqual`, `ok`, `rejects`, `strict`, `strictEqual`, `throws`

The strict export is an assertion object. The default export is the module object. See node:util for isDeepStrictEqual.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
