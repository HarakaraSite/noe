# OS signal spike results

Environment: Linux amd64, Go 1.27.2, go-quickjs v0.27.0, repository HEAD
`2200219`. The spike is guarded by `lowlevel2_spike && linux`; the real OS
signals below are sent only to helper child processes.

## Observed

- With no broker subscription, helper SIGTERM ended the child with the
  `SIGTERM` signal, confirming the default action.
- With main and Worker subscriptions, SIGTERM reached both QuickJS callbacks
  and did not end the child. The Worker unsubscribed itself and exited; the
  main process stayed alive and received two later SIGINTs.
- The second SIGINT reached the main callback after Worker exit. The same
  SIGINT also canceled a simultaneously active `signal.NotifyContext`.
- After the main callback unsubscribed and the main `stdlib.Loop.Run` returned,
  SIGTERM again ended the child with the default action. The test uses no JS
  timers to keep either runtime alive.
- A Go slice retaining a `quickjs.Value` did not make `stdlib.Loop.Run` stay
  open. A referenced `AsyncWork` did keep the subscribed runtime alive.
- After unsubscribe removed the broker's callback, a finalizer on an object
  captured only by that callback ran. Closing a subscribed runtime removed
  its broker entry, ended the `AsyncWork`, and released the runtime too.
- The Worker was an actual `stdlib.Workers` Worker, not a second independent
  runtime standing in for one. Its exit did not end the process or main
  runtime.

## Commands and results

`gofmt -w spikes/low-level-group2/signals/signals_test.go` completed.

`go test -tags=lowlevel2_spike -v ./spikes/low-level-group2/signals`:

```text
=== RUN   TestSignalBrokerOSIntegration
=== RUN   TestSignalBrokerOSIntegration/unsubscribed_SIGTERM_keeps_its_default_action
=== RUN   TestSignalBrokerOSIntegration/owner_delivery,_unsubscribe,_NotifyContext,_and_default_restore
--- PASS: TestSignalBrokerOSIntegration (0.19s)
    --- PASS: TestSignalBrokerOSIntegration/unsubscribed_SIGTERM_keeps_its_default_action (0.00s)
    --- PASS: TestSignalBrokerOSIntegration/owner_delivery,_unsubscribe,_NotifyContext,_and_default_restore (0.18s)
=== RUN   TestGoOwnedCallbackDoesNotKeepRuntimeAlive
--- PASS: TestGoOwnedCallbackDoesNotKeepRuntimeAlive (0.00s)
=== RUN   TestUnsubscribeReleasesGoSubscriberClosure
--- PASS: TestUnsubscribeReleasesGoSubscriberClosure (0.02s)
=== RUN   TestRuntimeCloseUnsubscribesSignalOwner
--- PASS: TestRuntimeCloseUnsubscribesSignalOwner (0.01s)
PASS
ok  	noe/spikes/low-level-group2/signals	0.225s
```

`go test -race -tags=lowlevel2_spike -v ./spikes/low-level-group2/signals`:

```text
=== RUN   TestSignalBrokerOSIntegration
=== RUN   TestSignalBrokerOSIntegration/unsubscribed_SIGTERM_keeps_its_default_action
=== RUN   TestSignalBrokerOSIntegration/owner_delivery,_unsubscribe,_NotifyContext,_and_default_restore
--- PASS: TestSignalBrokerOSIntegration (0.32s)
    --- PASS: TestSignalBrokerOSIntegration/unsubscribed_SIGTERM_keeps_its_default_action (0.01s)
    --- PASS: TestSignalBrokerOSIntegration/owner_delivery,_unsubscribe,_NotifyContext,_and_default_restore (0.31s)
=== RUN   TestGoOwnedCallbackDoesNotKeepRuntimeAlive
--- PASS: TestGoOwnedCallbackDoesNotKeepRuntimeAlive (0.01s)
=== RUN   TestUnsubscribeReleasesGoSubscriberClosure
--- PASS: TestUnsubscribeReleasesGoSubscriberClosure (0.00s)
=== RUN   TestRuntimeCloseUnsubscribesSignalOwner
--- PASS: TestRuntimeCloseUnsubscribesSignalOwner (0.00s)
PASS
ok  	noe/spikes/low-level-group2/signals	1.345s
```

`go vet -tags=lowlevel2_spike ./spikes/low-level-group2/signals` completed
with no diagnostics.

## Planning recommendations

- Keep one process broker while at least one runtime is subscribed. Deliver
  through an owner-thread queue such as QuickJS `AsyncWork`; the OS signal
  goroutine must not use `quickjs.Value`.
- A subscription should keep its owner runtime alive until unsubscribe or
  runtime close. Make unsubscribe idempotent and arrange automatic removal on
  owner close. A Worker subscription belongs to and keeps alive that Worker;
  Worker exit does not imply process exit.
- Subscribe only to signal names the script requested. `signal.Notify`
  suppresses the OS default action for registered signals, as the SIGTERM
  observation demonstrates. The prototype registers both SIGINT and SIGTERM
  for every subscriber, so signal selection still needs a product contract.
- SIGINT is not consumed by a JS handler. In the current CLI,
  `cmd/noe/main.go` installs `signal.NotifyContext` for non-REPL commands;
  the test confirms a parallel JS subscription does not prevent cancellation.
  `internal/cli/repl.go` separately receives SIGINT and cancels the current
  evaluation. REPL and real NOE behavior were not exercised in this spike.
- The callback shape is only a spike mechanism for testing owner delivery.
  The planned API is an explicit signal-name subscription handle that exposes
  an `AsyncIterator`; Go should not add Promise-handler monitoring for it.
  Make handle close and iterator `return()` share idempotent unsubscribe so a
  `break` from `for await` releases the signal registration and runtime
  keepalive. Errors inside JS `for await` processing follow ordinary
  JS/runtime error paths. Define whether owner shutdown drains or drops signal
  values already queued for the iterator, and keep a Worker exit scoped to
  that Worker, consistent with the pinned stdlib's Worker behavior.
- Do not promise exactly-once delivery. `os/signal` and the OS can coalesce or
  drop notifications during bursts. No burst behavior was tested.

The current `internal/runtime` has no generic `Host.own` method. The
Go-retained-value test therefore checks the underlying lifetime distinction
directly: a Go reference alone does not keep the QuickJS loop alive; explicit
runtime work does.
