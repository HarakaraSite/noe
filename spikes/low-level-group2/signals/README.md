# OS signal spike

> Retired after production integration (v1.16.0). The prototype test code was
> removed; the findings and historical reproduction commands below are retained.
> Source before retirement is available in Git at `7b43646`.
> Current regression coverage is in `internal/nativeio`, `internal/signals`,
> `internal/runtime`, and `tests/low_level*_e2e_test.go`.

This opt-in spike tests a process-wide signal broker that fans OS signals out
to QuickJS runtimes on their owner goroutines. It is Linux/POSIX-specific and
adds no module dependency. The test's real SIGINT and SIGTERM are sent only to
helper child processes started by the test.

Run it from the repository root:

```sh
go test -tags=lowlevel2_spike -v ./spikes/low-level-group2/signals
go test -race -tags=lowlevel2_spike -v ./spikes/low-level-group2/signals
```

The `lowlevel2_spike` build tag keeps this experiment out of normal builds and
tests. The test starts one helper with no subscriptions to check SIGTERM's
default action, then another helper with a main runtime and a real
`stdlib.Workers` Worker. Both subscribe without a JavaScript timer. SIGTERM is
delivered to both while subscriptions are active; the Worker unsubscribes and
exits, and the main runtime remains alive to receive two SIGINTs. The final
main unsubscribe lets its event loop return, after which SIGTERM again
terminates the helper by its default action.

The prototype uses one buffered `os/signal` channel and dispatch goroutine for
the active process broker. Each runtime starts a referenced QuickJS
`AsyncWork`; the signal goroutine only posts work, and the callback calls JS
when that runtime's event loop runs it. Explicit unsubscribe and runtime close
share an idempotent cleanup path. The final unsubscribe calls `signal.Stop`
and waits for the dispatch goroutine to stop.

## Evidence and limits

See [RESULTS.md](RESULTS.md) for the observed test results and planning notes.
The full reproduction and helper code is in [the retired signals prototype](https://forge.harakara.site/littleisland/noe/src/commit/7b436464eb0407f5ce196efa9e288d4061f6e78e/spikes/low-level-group2/signals/signals_test.go).
The test verifies delivery to a real go-quickjs Worker, not only to two
independent runtimes. It does not run the NOE CLI or its REPL. In particular,
the existing CLI's SIGINT cancellation remains active alongside a JS
subscription: the helper observes its `signal.NotifyContext` cancellation
when the same SIGINT reaches both QuickJS owners.

The broker in this spike registers SIGINT and SIGTERM for every subscription.
It does not yet expose a per-signal selection API, model multiple OS signal
sets, or promise exactly-once delivery. Go and the OS may coalesce or drop
signals during bursts; the test sends signals one at a time and waits for each
observation.

The callback-shaped API in this spike exists only to verify owner-thread
delivery. The planned public shape is an explicit signal-name subscription
handle with an `AsyncIterator`; Go does not need to inspect handler Promises.
Errors in JS `for await` processing should follow the ordinary JS Promise and
runtime error paths. Define whether shutdown drains or drops values already
queued for the iterator, and keep exit scoped to the owner runtime: a Worker
ending should not implicitly end the process. This spike does not change CLI
signal behavior.
