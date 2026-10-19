# node:os

Reports selected host system information. This module is read-only and intentionally small; the listed values and functions are its complete export surface.

### Exports

`EOL`, `arch`, `cpus`, `default`, `homedir`, `hostname`, `platform`, `tmpdir`, `type`

These values describe the host running NOE. The module does not provide broader Node helpers for temporary-file creation or user/group management.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
