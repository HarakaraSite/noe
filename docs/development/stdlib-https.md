# HTTPS listeners and WSS upgrades

2026-11-07. The user approved adding HTTPS/WSS listeners to `noe:http.serve`
after the v2.16.0 release and local binary installation. This additive slice
is included in [v2.17.0](../releases/v2.17.0.md). The user separately authorized
that release and its local installation on 2026-11-07, and instructed that
future releases include installation. Running-service restarts remain separate.
The [session handoff](stdlib-handoff.md) records publication/installation status
and the continuation point.

## Selected contract

JavaScript supplies PEM text through `serve({cert, key, ...options}, handler)`.
Both must be nonempty strings. Certificate chains use the usual concatenated
PEM certificates. Neither option means HTTP unless the Go host configured TLS.
One missing option, null/non-string input, malformed PEM, or mismatched key
throws TypeError synchronously before authorization or opening a listener.
Errors do not include supplied certificate or private-key text. The handler
and existing hostname/port/error options retain their contracts.

These options contain PEM data, not file paths. Read files explicitly through
`noe:fs` when needed; filesystem Root/Cwd/ReadOnly authority still applies.
Inline TLS does not install or grant FS, Fetch, or WebSockets.

```js
import {readFile} from "noe:fs";
import {serve} from "noe:http";

const server = serve({
  hostname: "127.0.0.1",
  port: 8443,
  cert: await readFile("server-cert.pem", "utf8"),
  key: await readFile("server-key.pem", "utf8"),
}, request => Response.json({url: request.url}));
console.log(server.url);
```

The returned server URL uses `https://` for TLS and `http://` otherwise.
Ordinary incoming HTTPS requests expose an HTTPS Request URL. TLS is HTTP/1.1
in this slice, preserving the existing WebSocket upgrade transport. HTTP/2,
ACME automation, automatic certificate generation, certificate hot reload,
and changes to client trust are separate work.

## Go embedding

`stdlib.ServeOptions.TLS *tls.Config` supplies a host-owned TLS configuration
without putting private keys in JavaScript. Nil retains HTTP/default JS
selection. Non-nil selects HTTPS for every listener under that Serve selection.
It needs Certificates, GetCertificate, or GetConfigForClient. Standard TLS
policy such as client certificates remains on that host configuration.
JavaScript cert/key options conflict with a host TLS configuration and throw
TypeError rather than overriding or silently merging its policy.

Profile copies and listeners clone TLS configuration without mutating the
caller's value. Certificate/key objects, pools and callbacks retain Go's
normal shared, immutable-in-use ownership. Listeners advertise HTTP/1.1;
configurations returned by a host GetConfigForClient callback are cloned and
normalized for the same supported protocol. Client-side WebSockets.TLS and
Fetch.Client settings are independent.

This adds a field to the exported ServeOptions struct. External unkeyed struct
literals must move to named fields. No existing field, capability selection,
or dependency changes. Workers receive their own configuration and runtime
binding while using the same underlying server implementation.

## Upgrade, authority, and cleanup

`upgradeWebSocket` accepts requests from either HTTP or HTTPS listeners when
Serve and WebSockets are both selected. A client uses a corresponding ws/wss
URL. The upgraded TLS connection and buffered reader pass to the existing
socket backend; close-frame exchange, native interruption and Worker cleanup
retain their ownership. Closing the listener does not close upgraded sockets.

The existing Serve.Allow(address) check authorizes the same TCP bind address
before a valid listener is opened. No implicit outbound or filesystem grant
is added. HTTP body limits, streaming/backpressure, Expect behavior, handler
admission, shared asynchronous close, async disposal, the graceful deadline,
and independent forced teardown are shared with plain HTTP. Pending TLS
handshakes are subject to the configured header timeout and forced cleanup.

## Verification

Evidence directory: `/var/tmp/noe-https-vm9c6p5e`.
Tests use ephemeral certificates and explicit client roots, without real
credentials or global trust changes. Completed checks:

- Existing HTTP/Serve/WebSocket/Worker tests pass. New public API cases cover
  inline HTTPS, plain HTTP regression, Request URLs/bodies, async close and
  disposal, port reuse, invalid PEM/configuration before Allow/bind, and
  unchanged capability selection.
- Go-host cases cover default HTTPS, JS/host conflicts, runtime snapshots,
  callback-selected certificates and HTTP/1.1 ALPN without caller mutation,
  Worker inheritance/termination, and forced cancellation of a stalled TLS
  handshake. New cases also pass with the race detector.
- WSS Blob/binary/text echo and clean close pass through the shared backend.
  The real CLI example additionally passes HTTPS health/JSON requests, raw
  WSS frames, HTTP/1.1 negotiation, interrupt cleanup, and port reuse.
- The public Go consumer runs against both the checkout and an isolated
  module artifact, with CGO disabled, and builds for Linux, macOS, and Windows
  on amd64/arm64. The tested runtime/dependency sources remain unchanged.
- Positive/negative TypeScript fixtures, example lint, embedded-doc catalog
  tests, affected-package vet, and a fresh CLI build/docs command pass.
- A bounded static review found no actionable production-path issues in
  validation, host policy, ALPN, ownership, WSS transport, or forced cleanup.

The aggregate race run initially failed only in the pre-existing
`TestWebSocketQueuedOpeningAfterClose`. Inspection established that Eval can
drain the opening notification before the test pauses owner delivery, which
admits long-lived socket workers into its opening-only wait. The test now
gates the handshake response until Eval returns. Its focused race recheck
passes; no socket production code changed for this correction. The initial
failure and all successful results are retained in the evidence directory.

The implementation CLI candidate is `/var/tmp/noe-https-vm9c6p5e/noe`.
v2.17.0 is now published, with the release-flags candidate, downloaded assets,
trusted HTTPS/WSS native smoke, and direct public Go consumer evidence at
`/var/tmp/noe-v2.17.0-release-yk3xb7im`. See the
[publication checkpoint](../releases/release-checkpoints.md#v2170-publication--2026-11-07).
Both installed executables now match the verified v2.17.0 asset and support
these listener options. Backups, installed hashes/versions, PATH, arithmetic,
and helper-help checks are recorded at
`/var/tmp/noe-v2.17.0-install-edf2149e/installation.json`.
