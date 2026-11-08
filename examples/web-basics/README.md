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

### Abort interfaces and text streams

The v2.22.0 cancellation/text-stream corrections are demonstrated by:

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/abort-text-streams.js
```

Rebuild the current source first. This checks standard AbortError/TimeoutError
DOMExceptions, TextDecoderStream option getters, and a surrogate pair split over
encoder chunks with an empty chunk between them. The
[contract](../../docs/development/stdlib-abort-text-streams.md) records the illegal
AbortSignal constructor, readonly controller signal, timeout conversion and
legacy no-Loop limitation. No filesystem/network/provider grant is required.

### Compression and route inputs

The v2.22.0 compression and URLPattern corrections are demonstrated by:

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/compression-routes.js
```

Rebuild the current source first. URLPattern resolves object inputs against
baseURL and captures a route parameter; a JSON body then passes through the
existing gzip compression/decompression backends. No network or filesystem grant
is needed. Compression chunks must be BufferSources and stream endpoints are
readonly. See the [contract and limitations](../../docs/development/stdlib-compression-urlpattern.md).

### URL normalization and query encoding

The v2.23.0 URL/query corrections are demonstrated by:

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/url-query.js
```

Rebuild the current source first. The example retains one searchParams reference
across search updates, decodes malformed UTF-8, normalizes a Unicode URL path and
uses the resulting parameters as a Fetch form body. It sends no request and needs
no filesystem/network/provider grant. See the
[contract and boundaries](../../docs/development/stdlib-url-encoding.md).

### Request URL and method inputs

The v2.23.0 Request input corrections are demonstrated by:

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/request-inputs.js
```

Rebuild the source first. Request normalizes a string URL, retains the case of
the extension method patch, exposes readonly URL/method attributes and clones a
body for local consumption. No network request or host grant is used. See the
[contract and boundaries](../../docs/development/stdlib-request-inputs.md).

## Headers and Response

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/headers-response.js
```

Shows iterable header inputs, separate Set-Cookie values, JSON response cloning
and redirect URL normalization. Invalid header names/values, invalid status codes
and non-null bodies for 204/205/304 now throw. Response metadata is readonly;
error, redirect and fetched headers are immutable. See the
[contract and migration](../../docs/development/stdlib-headers-response.md).

## Fetch body consumption

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/body-consumption.js
```

Shows reader locks versus bodyUsed, rejected duplicate reads, independent bytes
results after cloning and finite Request input/transfer ownership. See the
[contract and verification](../../docs/development/stdlib-body-consumption.md).

## Stream tee cancellation

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/tee-cancellation.js
```

Shows discarding one Response clone while consuming the other, and waiting for
shared cleanup after cancelling both tee branches. See the
[contract and verification](../../docs/development/stdlib-stream-tee.md).

## Body clone byte isolation

```sh
/tmp/noe-web-basics run --no-inst examples/web-basics/body-clone-bytes.js
```

Shows raw body-chunk mutation and buffer transfer while a Request/Response clone
retains its original bytes. See the
[contract and verification](../../docs/development/stdlib-body-clone-bytes.md).
