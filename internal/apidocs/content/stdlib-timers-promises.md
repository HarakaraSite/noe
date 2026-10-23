# node:timers/promises

### API information

Reference: `node:timers/promises`; aliases: `timers/promises`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/extras.go. Reading this reference evaluates no target module and makes no provider request.

Provides Promise and async-iterator timer forms. Timers use the NOE event loop and support AbortSignal in their options where applicable.

### Exports

`default`, `scheduler`, `setImmediate`, `setInterval`, `setTimeout`

The module is available because NOE installs an event loop. Use global callback timers when work should run as a callback.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
