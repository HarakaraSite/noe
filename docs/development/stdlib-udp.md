# UDP datagram sockets

2026-11-07. The user approved adding UDP send/receive to `noe:sockets` after
the raw TLS slice. Both are included in v2.18.0, whose publication and local
installation the user authorized on 2026-11-07.

## Contract

Add `bind({network: "udp", host, port, signal?}) -> Promise<DatagramSocket>`.
Host is a nonempty string; port is 0–65535, with zero requesting an OS-selected
port. The returned socket has:

- `address: {network: "udp", host, port}` for the bound endpoint.
- `send(data, address, {signal}?) -> Promise<number>`. Data is Uint8Array,
  copied before asynchronous work; destination network is udp and port 1–65535.
  One call sends one datagram, including an empty one. Success returns its byte
  count, not a delivery acknowledgement. OS routing/size failures reject.
- `receive({signal}?) -> Promise<{data, remoteAddress}>`. Data is a fresh
  Uint8Array containing one complete datagram, including zero-byte datagrams.
  remoteAddress is `{network: "udp", host, port}`. There is no stream EOF.
- `close() -> Promise<void>` stops admission, releases the socket and pending
  native I/O, and resolves undefined. Repeated calls return the same Promise.
- `[Symbol.asyncDispose]()` delegates to close.

Keep connect/listen and their existing stream return types unchanged. UDP does
not use accept, readable/writable byte streams, connection EOF, or FIN. This is
an additive export and type surface; no public Go field or dependency changes.

Invalid option shapes, non-Uint8Array data, TLS fields on UDP bind, and invalid
ports reject with TypeError/RangeError before I/O. Bind signal owns only the
pending bind. Send/receive signals cancel only their operation and preserve
the exact JS reason. Queued operations may be canceled without disturbing
another operation. Once a result is handed off, later abort has no effect.
Cancellation may discard a datagram already received or follow a send already
accepted by the OS; it does not guarantee withdrawal or redelivery.

## Transport and lifecycle

Use Go UDPConn with the existing native Host resource/worker/async-job model.
No additional capability is selected: `Modules.RawSockets` remains the grant,
available in main and Workers, independently of FS/Fetch/HTTP/WebSockets/TLS.
Pending bind results are owned before JS publication. Closing the owning
runtime or Worker cancels operations and closes the UDP socket.

IPv4/IPv6 and host resolution follow the native network stack. Use explicit
loopback addresses in tests. DNS lookup for send destinations is cancellable.
Serialize reads and writes independently so an operation's cancellation
deadline cannot affect another operation on the same descriptor. Stop/join
the cancellation callback and clear its deadline before releasing that gate.
Only an explicit receive consumes a packet; no unbounded JS packet queue.

Use a 65,536-byte receive buffer for ordinary UDP datagrams. Receive errors
reject; a full buffer rejects with RangeError/ERR_UDP_MESSAGE_TOO_LARGE instead
of exposing a possible truncated prefix.
UDP supplies no delivery, order, duplicate suppression, or retry guarantee.
Multicast membership/broadcast configuration and connected UDP are separate.

Go references: [PacketConn](https://pkg.go.dev/net#PacketConn),
[UDPConn](https://pkg.go.dev/net#UDPConn), and the
[cancellable I/O example](https://pkg.go.dev/context#example-AfterFunc-Connection).

## Verification

Evidence: `/var/tmp/noe-udp-mkdxtl0z`. Implementation and verification are
complete locally. Tests use local sockets; noebot and real provider/account
traffic were excluded. No dependencies or public Go fields changed.

- Public tests verify binary/text/zero-byte and maximum ordinary IPv4 packets,
  byte snapshots, multiple peers/source endpoints, input validation, bind-signal
  ownership transfer, exact abort reasons, cancellation isolation/reuse, shared
  close, async disposal, port reuse, and Worker/runtime cleanup. Hostname
  destination selection and IPv6 loopback also pass on this environment.
- Native tests verify that Host close reclaims a queued bind result without
  owner delivery, cancellation disposes a late bind result, a canceled blocked
  write clears its deadline before the next send, and a full receive buffer
  rejects without exposing a packet prefix. Borrowed runtimes remain usable.
- Affected native/profile/public tests and race checks pass, including TCP,
  Unix, and raw TLS regressions. TypeScript fixtures, example lint, embedded
  docs, affected-package vet, the real CLI example, and a fresh CLI build pass.
- Public consumers run against checkout and isolated Go module artifact with
  CGO disabled and build for Linux/macOS/Windows on amd64/arm64. The UDP
  consumer covers packet/reply boundaries, empty data, cancellation/reuse,
  independent grants, and shared close. Native execution is Linux amd64.
- A bounded independent static review found no actionable issue in pending
  bind ownership, Host/Worker teardown, conversion cleanup, cancellation/DNS,
  deadline isolation, byte snapshots, or datagram boundaries. Existing source
  and dependencies are fingerprinted for reuse at release preparation.

The implementation executable is `/var/tmp/noe-udp-mkdxtl0z/noe`.
v2.18.0 release and installation evidence is retained at
`/var/tmp/noe-v2.18.0-release-ur4u1vbf`; see the session handoff and release
checkpoints for current publication/installation status. No running-service
restart or remote application change is included.
