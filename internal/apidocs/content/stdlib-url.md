# node:url

### API information

Reference: `node:url`; aliases: `url`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers. Operations still follow the selected host capabilities and the limits below.

Public-definition source: internal/stdlib/webapis.go and internal/stdlib/extras.go. Reading this reference evaluates no target module and makes no provider request.

Exposes Web URL and URLSearchParams plus a small set of URL and file-URL helpers. parse and format wrap Web URL behavior; they are not Node's legacy URL parser/formatter.

### Exports

`URL`, `URLSearchParams`, `default`, `domainToASCII`, `domainToUnicode`, `fileURLToPath`, `format`, `parse`, `pathToFileURL`, `resolve`

domainToASCII lowercases its input and domainToUnicode returns it unchanged in this implementation; they do not perform full IDNA conversion.

### Normalization and query encoding

URL and URLSearchParams are the same constructors as the global Web interfaces.
URL uses component-specific UTF-8 percent encoding, normalizes literal/encoded dot
segments and default ports, and retains one searchParams object across href/search
assignments. parse/canParse preserve conversion exceptions while reporting parsing
failure as null/false. Full IDNA/IP parsing and Windows file-URL conformance are
outside this correction.

URLSearchParams uses form encoding with + for spaces and replacement UTF-8 decoding
for malformed byte sequences. Generic pair iterables, records, optional value
filters, readonly size and live iteration follow the supported Web contract.
The node:querystring module retains its separate conventions.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
