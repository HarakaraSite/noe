# node:url

Exposes Web URL and URLSearchParams plus a small set of URL and file-URL helpers. parse and format wrap Web URL behavior; they are not Node's legacy URL parser/formatter.

### Exports

`URL`, `URLSearchParams`, `default`, `domainToASCII`, `domainToUnicode`, `fileURLToPath`, `format`, `parse`, `pathToFileURL`, `resolve`

domainToASCII lowercases its input and domainToUnicode returns it unchanged in this implementation; they do not perform full IDNA conversion.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
