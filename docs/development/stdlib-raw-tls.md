# Raw TLS sockets

2026-11-07. The user approved implementing TLS connections and listeners in
`noe:sockets`. This slice is included in v2.18.0, whose publication and local
installation the user authorized on 2026-11-07. UDP has its own datagram contract.

## Contract

`connect({network: "tls", host, port, serverName?, ca?, cert?, key?, signal?})`
returns the existing Connection after TCP connection and TLS handshake.
`listen({network: "tls", host, port, cert?, key?, signal?})` returns the existing
Listener. Its address is `{network: "tls", host, port}`; `accept()` returns a
Connection after the server handshake. One failed handshake rejects that
accept, without closing the listener. Multiple accepts may progress separately.
Port rules match TCP: 1–65535 for connect, 0–65535 for listen.

TLS clients verify chains and hostnames by default using system roots. Optional
`serverName` selects the name to verify/send as SNI; otherwise use `host`.
Optional `ca` is nonempty PEM text containing roots for this connection only,
replacing system roots. Optional client `cert`/`key` supply a matching PEM pair.
JS does not expose a certificate-verification bypass. Listeners require a PEM
pair unless their Go host supplies a server configuration. Chains may contain
concatenated PEM certificates. Values contain data, never implicit file paths.

Invalid shapes, empty values, malformed CA/identity PEM, mismatched pairs,
missing listener identity, and host/JS configuration conflicts reject with
TypeError before dial/bind, without including PEM material in messages.
TLS fields are refused on TCP/Unix options; ca/serverName are client-only.
AbortSignal cancels a pending connect through its handshake, or a pending bind.
It no longer owns an established connection or listener after return.

## Go embedding

Keep `Modules.RawSockets bool`. Add `Modules.RawSocketsTLS *SocketTLSOptions`,
where SocketTLSOptions contains `Client *tls.Config`, `Server *tls.Config`, and
`HandshakeTimeout time.Duration`. Configuration alone does not select sockets.
External unkeyed Modules literals must become keyed for the added field.

A non-nil direction configuration is authoritative: corresponding inline JS
TLS fields conflict instead of overriding host settings. A client configuration
with empty ServerName still derives it from the connection host. Server
configurations require Certificates/GetCertificate/GetConfigForClient. Unlike
HTTP listeners, raw TLS preserves host ALPN choices. Fetch/Serve/WebSockets TLS
configurations and capability selections are independent.

Profile/runtime/Worker/listener copies clone configurations, without mutating
the caller. Go certificate/key objects, pools, caches and callbacks retain
shared immutable-in-use ownership. HandshakeTimeout zero selects 10 seconds,
positive values override it, and a negative value disables the handshake timer.
The timer begins after TCP connection/accept; owner cancellation remains active.

## Streams and ownership

Reuse the native TCP/Unix connection, listener, pending-operation ownership,
Web Streams, and async-disposal backend. Only authenticated TLS connections
reach JS. Listener close releases pending accepts/handshakes and queued,
undelivered results; already delivered connections retain independent ownership.
Runtime cancellation and Worker termination interrupt handshakes and I/O.

`writer.close()` sends TLS close_notify while leaving reads available. Local
read cancellation interrupts only the read side; TLS has no matching protocol
read-shutdown message. Cancellation does not wait for TLS protocol-alert writes,
which may be blocked behind an application writer. The connection retains that
native work and joins it on full close. `writer.abort()` interrupts writes
without waiting to send a graceful TLS alert. Full close/disposal and execution cleanup close the TCP
transport promptly; use writer.close first when a graceful TLS EOF is needed.

Go references: [HandshakeContext](https://pkg.go.dev/crypto/tls#Conn.HandshakeContext),
[CloseWrite](https://pkg.go.dev/crypto/tls#Conn.CloseWrite), and
[Config ownership](https://pkg.go.dev/crypto/tls#Config).

## Verification

Evidence: `/var/tmp/noe-raw-tls-940je7qo`. Implementation and verification are
complete locally. Certificates are ephemeral, roots explicit, and no noebot or
real-account calls were made. Dependencies are unchanged.

- Existing native TCP/Unix, profile attachment/cloning, borrowed-runtime, and
  asynchronous-disposal checks pass alongside the new public TLS cases.
- Public checks cover inline binary/text and TLS half-close, untrusted and
  wrong-host certificates, pre-I/O validation/redaction, host mutual TLS and
  snapshots, independent capability selection, listener close ownership,
  Worker handshake cancellation/port reuse, runtime cancellation, AbortSignal
  reason and post-handoff behavior, and configured handshake timeout.
- Native race regressions cover authenticated results queued before owner
  delivery, context-aware certificate callback cancellation, blocked writes,
  and local read cancellation behind a TLS protocol-alert write. The last
  case deterministically failed before its fix and passes afterward. Full
  close retains and joins native work, without waiting for a graceful alert.
- Affected native/profile/public race checks pass. The real CLI round-trip
  example, positive/negative TypeScript fixtures, example lint, embedded-doc
  catalog, affected-package vet, and a fresh CGO-disabled CLI build pass.
- Public consumers run with CGO disabled against the checkout and an isolated
  Go module artifact, and build for Linux/macOS/Windows on amd64/arm64. The
  raw TLS consumer verifies an arbitrary host ALPN and stream half-close.
- Bounded independent review checked handshake verification/ownership,
  cancellation, configuration snapshots/grants, stream shutdown, and forced
  cleanup. Its read-cancellation finding was reproduced and fixed; one scoped
  re-review found no remaining actionable issue.

The first CLI example check exposed a fixture hostname mismatch: the example
used localhost with a certificate valid for other names. It now connects to
the listener's explicit IPv4 address, and its final CLI E2E passes. Initial
failures and successful correction logs remain in the evidence directory.

The implementation executable is `/var/tmp/noe-raw-tls-940je7qo/noe`.
v2.18.0 release and installation evidence is retained at
`/var/tmp/noe-v2.18.0-release-ur4u1vbf`; see the session handoff and release
checkpoints for current publication/installation status.
