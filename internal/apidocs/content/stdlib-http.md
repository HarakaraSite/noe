# node:http

### API information

Reference: `node:http`; aliases: `http`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Serve.

Public-definition source: internal/stdlib/serve.go. Reading this reference evaluates no target module and makes no provider request.

Provides an HTTP server backed by the Web Request and Response APIs. It does not implement Node's client request API or Node request/response streams. serve is also installed as a global. Prefer `noe:http` for named imports of both fetch and serve; its serve export is this same function.

### Exports

`default`, `serve`

Server resources keep the event loop alive until closed or the runtime ends. Use noe:sockets for raw TCP/Unix connections rather than HTTP request handling.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
