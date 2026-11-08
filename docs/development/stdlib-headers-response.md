# Headers and Response contracts

Approved on 2026-11-08 after v2.23.0 publication and installation. This slice
covers local implementation, validation and commit. It does not publish or
install another release. The [API policy](stdlib-api-policy.md),
[Fetch Headers](https://fetch.spec.whatwg.org/#headers-class),
[Fetch Response](https://fetch.spec.whatwg.org/#response-class) and
[Web IDL conversions and iterators](https://webidl.spec.whatwg.org/)
are the design references.

### Scope and migration

Headers accepts iterable pairs and records with ByteString conversion. Header
names with surrounding whitespace now reject instead of being repaired. Values
trim HTTP whitespace rather than all Unicode whitespace; embedded CR/LF/NUL,
Symbols and characters above U+00FF reject. Conversion completes before HTTP
validation. Mutators validate before changing private state. The `_list` property
is removed; internal HTTP transport takes a private snapshot. Use public iteration
or getSetCookie instead of inspecting implementation properties.

Iteration remains sorted, combines ordinary fields and preserves separate
Set-Cookie entries. Iterators and forEach observe mutations. getSetCookie returns
a copy of the individual values; get retains the combined string behavior.
The existing toJSON utility still produces a record and cannot preserve duplicate
cookie entries. Request/Response keep a stable readonly Headers reference.

Response constructor validation now rejects out-of-range status, invalid
ByteString reason phrases, used/locked streams and non-null bodies with a
null-body status. Status uses unsigned-short conversion before the 200..599
range check. Constructed responses have type default, empty url and false
redirected. Metadata uses branded readonly private state. ResponseInit.url is
removed from types and ignored at runtime. Set native metadata through the host
transport rather than mutating Response properties.

Response.json rejects values that JSON cannot serialize and preserves explicit
content types. Response.error produces a bodyless status-0/type-error value.
Response.redirect serializes an absolute URL through the shared URL API and
restricts status to redirect codes. Error/redirect/fetched headers are immutable;
clone preserves that guard and all native metadata. Normal constructed/JSON
headers remain mutable, including cookies, for host HTTP handlers.

Native incoming/outgoing headers preserve bytes through isomorphic ByteString
conversion rather than treating wire bytes as UTF-8 text. Native HEAD/null-body
responses expose null body and release their unused native reader. Reading a null
body leaves bodyUsed false. Response construction snapshots byte sources. Existing
streams, upload limits, network admission and cleanup ownership remain shared.

The noe:http WebSocket helper uses a Go-private response factory for status 101;
the standard public constructor rejects 101. Factories permit either installation
order and are removed on runtime close. Native replies take private status/header
snapshots even when a public getter is shadowed; returning Response.error enters
the server's error handler rather than sending status 0. No factory or
initialization token is published to JavaScript.

This is a bounded correction of the existing APIs. Browser forbidden-name/CORS
header guards, cache/redirect policy, dependent Request signals, full Body/stream
branding and all BodyInit/BufferSource edge cases remain outside this slice.
NOE supplies no ambient document base URL. Full URL/IDNA/parser conformance is
unchanged. noebot and running services are excluded.

### Verification

Evidence: `/var/tmp/noe-headers-response-j6pkokoa`. All seven affected packages
pass. The final native null-body correction also passes focused response/limit /
upload checks and race tests. Types, catalog, example lint, formatting and vet
pass. Nine CLI contract/reference/example/SDK and comparison checks pass. Checkout
and standalone module-artifact consumers cover main and Workers; exact runtime /
declaration source continuity is recorded in the evidence directory.

The prepared official MCP SDK loopback example completes initialization, tool
listing and three parallel echo calls, including Japanese text. Bundle, lockfile
and source fingerprints are unchanged. No external provider or login is used.

The installed v2.23.0 baseline accepts invalid names/newline/Unicode values,
combines cookies during iteration, accepts status 0/700 and body-bearing 204,
and returns type basic from constructors/error. The development candidate corrects
these cases while preserving native HTTP and the prepared MCP SDK fixture.

Deno 2.9.7 is an interoperability reference, not the normative oracle. It accepts
public Response status 101, snapshots Headers.forEach rather than observing an
addition, and differs from current Web IDL dictionary getter order, including
JSON serialization before init conversion. Those differences are recorded
separately; primary assertions retain the standard behavior.

One bounded independent review found a P2: without a Loop, a payload on HTTP 205
was read and limited before being discarded. Native HEAD/null-status responses
now cancel and close before buffered reads or stream creation. A real HTTP 205
payload with MaxBodyBytes 1 resolves successfully with null body. One fix-only
re-review confirms resolution, normal body limits, and fifteen independent owner /
cleanup cases, with zero reads and one close for null bodies in both modes.
The initial review also independently confirmed both installer orders, factory
rebinding/removal, private 101 upgrades and all 256 byte values. Independent
overlay probes were in memory; the parent retains the reported results.

Both installed paths remain the verified v2.23.0 asset, SHA256
`7b7ff1dffbe40b35339e3f999b7a247442f4e91384104d211b6e226ed7cc4cd9`.
No push, publication, installation or service change is included. Reuse these
checks for unchanged source and dependencies when a release is requested.
