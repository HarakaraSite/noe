# node:os

### API information

Reference: `node:os`; aliases: `os`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.OS; values follow the supplied host options.

Public-definition source: internal/stdlib/os.go. Reading this reference evaluates no target module and makes no provider request.

Reports selected host system information. This module is read-only and intentionally small; the listed values and functions are its complete export surface.

### Exports

`EOL`, `arch`, `cpus`, `default`, `homedir`, `hostname`, `platform`, `tmpdir`, `type`

These values describe the host running NOE. The module does not provide broader Node helpers for temporary-file creation or user/group management.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
