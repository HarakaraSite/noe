# node:querystring

Parses and serializes legacy query-string text. Repeated keys parse into arrays, and spaces are encoded as +. Prefer URLSearchParams for new code using the Web URL API.

### Exports

`decode`, `default`, `encode`, `escape`, `parse`, `stringify`, `unescape`

The parser returns an object with no prototype. Malformed percent escapes are preserved as text by unescape.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
