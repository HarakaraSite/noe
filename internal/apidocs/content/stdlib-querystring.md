# node:querystring

### API information

Reference: `node:querystring`; aliases: `querystring`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/extras.go. Reading this reference evaluates no target module and makes no provider request.

Parses and serializes legacy query-string text. Repeated keys parse into arrays, and spaces are encoded as +. Prefer URLSearchParams for new code using the Web URL API.

### Exports

`decode`, `default`, `encode`, `escape`, `parse`, `stringify`, `unescape`

The parser returns an object with no prototype. Malformed percent escapes are preserved as text by unescape.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
