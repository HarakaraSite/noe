# node:timers

### API information

Reference: `node:timers`; aliases: `timers`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/extras.go, internal/stdlib/timers.go. Reading this reference evaluates no target module and makes no provider request.

Exports callback-style timer functions. NOE also installs setTimeout, setInterval, clearTimeout, and clearInterval as globals. Timers run on the runtime event loop.

### Exports

`clearImmediate`, `clearInterval`, `clearTimeout`, `default`, `setImmediate`, `setInterval`, `setTimeout`

setImmediate is implemented as a zero-delay timeout. These module exports do not add a global setImmediate.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
