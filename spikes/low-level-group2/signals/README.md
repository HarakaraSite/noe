# OS signal spike

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
The full reproduction and helper code is in [signals_test.go](signals_test.go).
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
