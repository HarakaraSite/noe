# node:url

### API information

Reference: `node:url`; aliases: `url`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/extras.go. Reading this reference evaluates no target module and makes no provider request.

Exposes Web URL and URLSearchParams plus a small set of URL and file-URL helpers. parse and format wrap Web URL behavior; they are not Node's legacy URL parser/formatter.

### Exports

`URL`, `URLSearchParams`, `default`, `domainToASCII`, `domainToUnicode`, `fileURLToPath`, `format`, `parse`, `pathToFileURL`, `resolve`

domainToASCII lowercases its input and domainToUnicode returns it unchanged in this implementation; they do not perform full IDNA conversion.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
