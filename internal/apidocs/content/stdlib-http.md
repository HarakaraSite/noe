# node:http

Provides an HTTP server backed by the Web Request and Response APIs. It does not implement Node's client request API or Node request/response streams. serve is also installed as a global.

### Exports

`default`, `serve`

Server resources keep the event loop alive until closed or the runtime ends. Use noe:sockets for raw TCP/Unix connections rather than HTTP request handling.



### Source basis

NOE-maintained implementation: internal/stdlib, based on github.com/go-quickjs/go-quickjs@v0.27.0/stdlib. NOE installs this module through internal/runtime/stdlib.go and reserves its bare-name alias through internal/modulemap/builtins.go.
