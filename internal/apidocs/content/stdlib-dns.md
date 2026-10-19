# node:dns

Resolves names using the host system resolver and exposes Node-style method names as promise-returning functions. dns/promises is a separate promise-oriented namespace with the same resolver operations.

### Exports

`ADDRCONFIG`, `ADDRGETNETWORKPARAMS`, `ALL`, `BADFAMILY`, `BADFLAGS`, `BADHINTS`, `BADNAME`, `BADQUERY`, `BADRESP`, `BADSTR`, `CANCELLED`, `CONNREFUSED`, `DESTRUCTION`, `EOF`, `FILE`, `FORMERR`, `LOADIPHLPAPI`, `NODATA`, `NOMEM`, `NONAME`, `NOTFOUND`, `NOTIMP`, `NOTINITIALIZED`, `REFUSED`, `Resolver`, `SERVFAIL`, `TIMEOUT`, `V4MAPPED`, `default`, `getDefaultResultOrder`, `getServers`, `lookup`, `lookupService`, `promises`, `resolve`, `resolve4`, `resolve6`, `resolveAny`, `resolveCaa`, `resolveCname`, `resolveMx`, `resolveNaptr`, `resolveNs`, `resolvePtr`, `resolveSoa`, `resolveSrv`, `resolveTlsa`, `resolveTxt`, `reverse`, `setDefaultResultOrder`, `setServers`

Resolution uses the host resolver, not a direct DNS packet client. Functions return promises even where Node also accepts callbacks.



### Source basis

Pinned implementation: github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
