# `noe:signals`

### API information

Public exports: `subscribe`.

Reference: `noe:signals`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Requires Profile.Modules.Signals. Supported signal names depend on the operating system.

Public-definition source: internal/nativeio/signals.go, internal/nativeio/js/signals.js. Reading this reference evaluates no target module and makes no provider request.

`noe:signals` exports `subscribe` to receive operating-system signals as an async iterator. It is available in the main runtime and Workers. See `subscribe` for supported names and signal behavior; subscriptions are reclaimed with their owning runtime.

## subscribe

```js
import { subscribe } from "noe:signals";
```

`subscribe(name) -> SignalSubscription` synchronously registers the named signal and returns an async iterator. `name` must be supported on the current operating system.

Linux and macOS support `SIGHUP`, `SIGINT`, `SIGTERM`, `SIGUSR1`, and `SIGUSR2`; Windows supports only `SIGINT`, and other operating systems may expose none. An unsupported name throws an `Error` with `code === "ERR_UNSUPPORTED_SIGNAL"`. A live subscription keeps its owner active until closed. While a `SIGTERM` subscription is active, it suppresses the process's default termination, including when owned only by a Worker. `SIGINT` still cancels the NOE CLI, so asynchronous cleanup is not guaranteed after an interrupt. Closing a Worker reclaims its subscriptions; use messages to coordinate whole-process shutdown.

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
