# Encoding, User Timing, and CustomEvent

[Examples](../README.md) · [Contract](../../docs/development/stdlib-web-basics.md)

These APIs are available in v2.21.0 and later. Build the current source:

```sh
CGO_ENABLED=0 go build -o /tmp/noe-web-basics ./cmd/noe
/tmp/noe-web-basics run --no-inst examples/web-basics/web-apis.js
```

The example writes complete UTF-8 characters into a caller-supplied view,
passes that view by reference through CustomEvent.detail, and records/retrieves
a User Timing measurement. No filesystem, network, command or provider grant
is required by these operations.

It also decodes a BOM split across calls and binds a deduplicated event listener
to an AbortSignal. Aborting removes that listener before later dispatches.
See the [decoder/listener contract](../../docs/development/stdlib-decoder-events.md).

encodeInto reports source UTF-16 code units consumed and destination bytes
written. A short destination receives no partial character. CustomEvent detail
retains the supplied reference; Performance mark/measure detail is instead
structured-cloned when the entry is created. Entry objects expose readonly
attributes, and each runtime/Worker has its own timeline.

Use mark/measure, getEntries/getEntriesByName/getEntriesByType and clearMarks /
clearMeasures. A standalone new PerformanceMark does not enter the timeline.
PerformanceObserver, resource/navigation timing and extra TextDecoder encodings
are not part of this change. See the contract for supported errors and limits.
