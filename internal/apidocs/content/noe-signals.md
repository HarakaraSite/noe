# `noe:signals`

`noe:signals` exports `subscribe` to receive supported operating-system signals as an async iterator. It is available in the main runtime and Workers. A live subscription keeps its owner active until it is closed or the owner ends. Signal notifications may be coalesced by the operating system or runtime.

On Linux and macOS, supported names are `SIGHUP`, `SIGINT`, `SIGTERM`, `SIGUSR1`, and `SIGUSR2`. On Windows, only `SIGINT` is supported. Other operating systems may expose no names. An unsupported name throws an `Error` with `code === "ERR_UNSUPPORTED_SIGNAL"`.

SIGINT still cancels the NOE CLI; asynchronous cleanup is not guaranteed after an interrupt. A `SIGTERM` subscription suppresses the process's default termination while the subscription is active, including a subscription owned only by a Worker. Closing a Worker reclaims that Worker's subscriptions; coordinate with the main runtime using messages when the whole process should exit. Runtime shutdown closes owned subscriptions without waiting for JavaScript callbacks.

## subscribe

```js
import { subscribe } from "noe:signals";
```

`subscribe(name) -> SignalSubscription` synchronously registers the named signal and returns an async iterator. `name` must be supported on the current operating system.

```js
import { subscribe } from "noe:signals";

const signals = subscribe("SIGTERM");
try {
  for await (const name of signals) {
    console.log(`received ${name}`);
    break;
  }
} finally {
  await signals.close();
}
```

## SignalSubscription.next

`await subscription.next() -> {value, done}` waits for a signal. `value` is the subscribed signal name and `done` is false for a delivered notification. After close, it resolves as `{value: undefined, done: true}`. Notifications are not a durable event log and repeated OS signals may be coalesced.

## SignalSubscription.close

`await subscription.close()` unregisters this subscription and is idempotent. `for await...of` calls the iterator's `return()` on `break`, which performs the same cleanup. Use `finally` when calling `next()` directly.

## SignalSubscription.return

The async-iterator `return()` method closes the subscription and resolves `{value: undefined, done: true}`. It lets `break` and normal iterator cleanup unregister the signal.
