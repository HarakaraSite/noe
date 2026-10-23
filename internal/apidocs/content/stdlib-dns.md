# node:dns

### API information

Reference: `node:dns`; aliases: `dns`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.DNS.

Public-definition source: internal/stdlib/dns.go. Reading this reference evaluates no target module and makes no provider request.

Resolves names using the host system resolver. The query functions in this module require callbacks; use node:dns/promises or the promises export for the Promise-based operations.

### Exports

`ADDRCONFIG`, `ADDRGETNETWORKPARAMS`, `ALL`, `BADFAMILY`, `BADFLAGS`, `BADHINTS`, `BADNAME`, `BADQUERY`, `BADRESP`, `BADSTR`, `CANCELLED`, `CONNREFUSED`, `DESTRUCTION`, `EOF`, `FILE`, `FORMERR`, `LOADIPHLPAPI`, `NODATA`, `NOMEM`, `NONAME`, `NOTFOUND`, `NOTIMP`, `NOTINITIALIZED`, `REFUSED`, `Resolver`, `SERVFAIL`, `TIMEOUT`, `V4MAPPED`, `default`, `getDefaultResultOrder`, `getServers`, `lookup`, `lookupService`, `promises`, `resolve`, `resolve4`, `resolve6`, `resolveAny`, `resolveCaa`, `resolveCname`, `resolveMx`, `resolveNaptr`, `resolveNs`, `resolvePtr`, `resolveSoa`, `resolveSrv`, `resolveTlsa`, `resolveTxt`, `reverse`, `setDefaultResultOrder`, `setServers`

Resolution uses the host resolver, not a direct DNS packet client. For example, lookup(hostname, callback) calls callback(error, address, family); with {all: true}, it calls callback(error, addresses). Missing callbacks throw TypeError. The query functions return undefined, not a Promise. The Resolver class in this module uses callbacks too; node:dns/promises.Resolver returns Promises.

~~~js
import { lookup } from "node:dns";
import { lookup as lookupAsync } from "node:dns/promises";

lookup("localhost", (error, address, family) => {
  if (error) throw error;
  console.log(address, family);
});
console.log(await lookupAsync("localhost"));
~~~



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
