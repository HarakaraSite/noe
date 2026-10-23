# node:dns/promises

### API information

Reference: `node:dns/promises`; aliases: `dns/promises`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.DNS.

Public-definition source: internal/stdlib/dns.go. Reading this reference evaluates no target module and makes no provider request.

Promise-oriented DNS resolver module. It uses the same system resolver as node:dns; operations resolve to addresses or records and reject on lookup errors.

### Exports

`ADDRGETNETWORKPARAMS`, `BADFAMILY`, `BADFLAGS`, `BADHINTS`, `BADNAME`, `BADQUERY`, `BADRESP`, `BADSTR`, `CANCELLED`, `CONNREFUSED`, `DESTRUCTION`, `EOF`, `FILE`, `FORMERR`, `LOADIPHLPAPI`, `NODATA`, `NOMEM`, `NONAME`, `NOTFOUND`, `NOTIMP`, `NOTINITIALIZED`, `REFUSED`, `Resolver`, `SERVFAIL`, `TIMEOUT`, `default`, `getDefaultResultOrder`, `getServers`, `lookup`, `lookupService`, `resolve`, `resolve4`, `resolve6`, `resolveAny`, `resolveCaa`, `resolveCname`, `resolveMx`, `resolveNaptr`, `resolveNs`, `resolvePtr`, `resolveSoa`, `resolveSrv`, `resolveTlsa`, `resolveTxt`, `reverse`, `setDefaultResultOrder`, `setServers`

See node:dns for the related constants and resolver-selection behavior.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
