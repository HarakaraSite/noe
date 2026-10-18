# TCP / Unix socket spike results

## Environment and command

Observed environment: Linux amd64, Go 1.27.2, repository-pinned `github.com/go-quickjs/go-quickjs` v0.27.0.

```sh
go test -tags lowlevel2_spike ./spikes/low-level-group2/net -count=1 -v
```

Result: **PASS** (`ok noe/spikes/low-level-group2/net 0.250s`). The command exercised 20 TCP/UDS subcases, including runtime-driven socket reclamation and the child-process pathname check.

Additional checks passed: `go test -race -tags lowlevel2_spike ./spikes/low-level-group2/net -count=1` (`2.266s`), `go vet -tags lowlevel2_spike ./spikes/low-level-group2/net` (no findings), and `gofmt -d spikes/low-level-group2/net/net_spike_test.go` (no diff).

`go list ./...` omitted this directory, confirming the test file is opt-in. A direct untagged `go test ./spikes/low-level-group2/net` returned `build constraints exclude all Go files`, as expected when explicitly naming a directory containing only the tagged test.

## Observed behavior

- **TCP and UDS half-close through QuickJS/Web Streams:** two JSONL writes arrived at the server; `writer.close()` made the server's read loop observe EOF; the server then returned a response; the client received it. The response preserved a binary `0x00, 0xff` prefix and a JSONL record. Thus closing the write side did not close the read side.
- **Read cancellation:** the bridge's Go worker reached the signal immediately before its `Read` call, and its completion signal was still open immediately before `ReadableStream.cancel()`. `CloseRead()` then returned the worker with an error; the pending JS read resolved as cancelled. On TCP and UDS, a subsequent client write still reached the server. The Go-only socket case checks the same started-but-incomplete `Read` condition before `CloseRead()`.
- **Write abort:** the bridge's Go worker reached the signal immediately before its `Write` call with a 32 MiB payload, and the worker completion signal and JS write Promise were still unsettled immediately before `WritableStream.abort()`. The write deadline then let the Go worker return, and the JS write Promise rejected with an `Error`. `CloseWrite()` delivered EOF to the peer, and the client still received the server's response. The Go-only socket case also checks that its large `Write` is incomplete before deadline/`CloseWrite` and returns with an error afterward.
- **Full close:** the Web Streams bridge's explicit `close()` and Go's `net.Conn.Close()` were each invoked after the Go read worker reached the signal immediately before `Read` and remained incomplete. Both returned the reader with an error on TCP and UDS, and the peer observed EOF. It closes both directions, unlike either directional operation.
- **Listener and event loop:** an open listener represented by referenced `AsyncWork` kept `stdlib.Loop.Run` active without a JS timer. Closing it from Go released the loop. Closing a listener with `Accept` pending also woke the accept worker, whose promise rejected.
- **Runtime cleanup:** closing QuickJS after its socket read worker reached the signal immediately before `Read` but before the worker completed invoked the Go ownership hook, closed the connection, and let the Go read return with an error without a JS callback.
- **Unix pathname:** ordinary `net.Listen("unix", path)` followed by `Close()` removed the pathname. A separate child test process was killed after it had bound and reported readiness; the socket pathname remained and still had socket file type. The test's temporary directory removes the fixture after observation.

The Go-only socket tests establish standard-library direction and wake-up behavior. The Web Streams cases additionally exercise QuickJS `Runtime.Go`, Promise delivery on the runtime loop, and the bridge's JavaScript adapter. Neither layer runs through NOE's actual `noe:io` API or a built NOE executable.

For the cancellation cases, the start signal is emitted by the Go worker immediately before its `Read`/`Write` call; the test separately checks that the worker has not completed immediately before cancellation and that it terminates afterward. This establishes a started-but-incomplete Go operation, but the spike does not inspect Go's netpoller or prove the goroutine was already parked inside an OS wait at the instant of cancellation. The write-abort case additionally verifies that the JavaScript `write()` Promise is unsettled before abort and rejects afterward.

## Contract recommendation

If sockets are selected, represent the connection as a duplex resource whose read and write directions have independent lifetimes:

- reader cancel shuts down reads and wakes an outstanding read while leaving writes usable;
- writer close sends EOF to the peer by closing only the local write direction;
- writer abort interrupts a pending write, terminates the local write direction, and leaves reads usable;
- explicit connection close shuts down both directions and wakes all pending I/O.

The host should own the listener and connection resources in Go and release them on explicit close or runtime shutdown without depending on a JS finalizer/callback. A listener intended to keep a script alive needs an explicit referenced runtime work/resource while open; merely keeping a Go pointer in an ownership map does not itself keep the event loop alive. Closing the listener must release that reference and unblock `Accept`.

For UDS, specify that normal listener close removes its bound pathname, while forced process termination can leave it behind. The spike gives no evidence that applications need automatic stale-path deletion or a new supervisor; those are not required for the observed contract.

## Limits and unverified cases

- This is Linux-only evidence for the stated environment. Other operating systems may differ in socket shutdown details and Unix socket cleanup.
- It does not verify integration with NOE's `nativeio` endpoint registry, runtime sessions, cancellation path, or CLI.
- It checks repeated JSONL records and one binary response, but does not test concurrent clients, high-volume throughput, backpressure tuning, or recovery after transport errors.
- It does not test Unix socket permissions, abstract namespace sockets, address reuse, or stale pathname replacement. In particular, it does not recommend deleting an existing path before bind.
- The write-abort behavior depends on the chosen explicit deadline policy. A product contract would need to state that abort is terminal for the write side; resetting that side to usable after abort was not tested.
