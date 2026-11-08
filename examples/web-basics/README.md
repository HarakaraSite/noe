# Encoding, User Timing, and CustomEvent

[Examples](../README.md) · [Contract](../../docs/development/stdlib-web-basics.md)

This is an unreleased source addition after v2.20.0. Build the current source:

```sh
CGO_ENABLED=0 go build -o /tmp/noe-web-basics ./cmd/noe
/tmp/noe-web-basics run --no-inst examples/web-basics/web-apis.js
```

The example writes complete UTF-8 characters into a caller-supplied view,
passes that view by reference through CustomEvent.detail, and records/retrieves
a User Timing measurement. No filesystem, network, command or provider grant
is required by these operations.

encodeInto reports source UTF-16 code units consumed and destination bytes
written. A short destination receives no partial character. CustomEvent detail
retains the supplied reference; Performance mark/measure detail is instead
structured-cloned when the entry is created. Entry objects expose readonly
attributes, and each runtime/Worker has its own timeline.

Use mark/measure, getEntries/getEntriesByName/getEntriesByType and clearMarks /
clearMeasures. A standalone new PerformanceMark does not enter the timeline.
PerformanceObserver, resource/navigation timing and extra TextDecoder encodings
are not part of this change. See the contract for supported errors and limits.
