# Request URL and method inputs

Approved continuation on 2026-11-08 after the local URL/query correction `99f7f4b`.
Local implementation, validation and commit only; no push, release or installation.
The [API policy](stdlib-api-policy.md) and [Fetch Standard](https://fetch.spec.whatwg.org/#request-class)
control this slice.

### Scope and migration

Normalize string/URL inputs through the existing Web URL backend, with USVString
conversion, required input, parsing errors and credential rejection. NOE exposes
no ambient document/API base URL, so relative inputs require explicit URL resolution
by the caller. Preserve Request.url fragments; network serialization still belongs
to the existing HTTP transport.

Convert method to ByteString, validate HTTP tokens and reject forbidden methods
(CONNECT, TRACE and TRACK). Normalize DELETE/GET/HEAD/OPTIONS/POST/PUT only; retain
case for extension methods such as patch. Empty/invalid methods no longer fall
back to GET. url/method are branded readonly attributes backed by private state.
Copy, clone and fetch consume those private values even if an instance property
shadows a getter. Preserve metadata when copying/cloning the existing surface.

Complete URL/method validation before body extraction or ownership transfer. Reuse
existing body, stream, abort, transport, Allow and execution-cleanup mechanisms.
Legacy Network without WebAPIs still installs, but Request construction explicitly
requires the Web URL backend. Do not install new globals or native parser fallbacks.

Incoming noe:http requests use a Go-private factory over the same Request state,
so native incoming methods and raw URLs are preserved rather than being rejected
or normalized by the public constructor. Copies and clones preserve that incoming
metadata. No JavaScript factory/token is published. The per-runtime bridge is
removed on runtime close and allows either installer order.

This is not a full Request/Fetch audit: HeadersInit/BodyInit conversion, remaining
metadata/RequestInit fields, dependent signals, browser CORS/cache/redirect policy,
header guards, Response and stream/body redesign remain outside this slice.
NOE's existing host network policy and body lifecycle remain the environment's
contract. Types, references, examples and main/Worker/external Go consumers must
agree with the corrected URL/method behavior.

### Verification and review

Evidence: `/var/tmp/noe-request-inputs-u_xbfrv4`. All seven affected stdlib/profile /
runtime/CLI packages, focused race, positive/negative types, reference catalog,
example lint, Go formatting/diff checks and vet pass. Public tests exercise exact
URL/method delivery through Allow and an actual loopback HTTP transport, shadowed
getters, invalid input before body acquisition, incoming raw methods/URI and
clone/transfer ownership. Legacy Network can install WebAPIs later.

Actual CLI primary/common/example/reference checks pass. The unchanged prepared
MCP SDK loopback fixture completes initialize/list and three parallel Unicode echo
calls, with original bundle/lock/source fingerprints retained. No external service,
login or model request is used. Checkout and standalone module-artifact consumers
build/run, including main and Workers; 225 runtime/declaration paths match the
artifact manifest and extracted module.

The pre-slice `99f7f4b` development binary reproduces unnormalized Request strings,
uppercased extension methods, writable URL/method and accepted empty methods /
credentials. The candidate matches Deno 2.9.7 on three URL and ten method cases,
copy/clone metadata and readonly attributes. Credential-bearing URL rejection is
a primary-only assertion: that Deno version accepts it contrary to Fetch.

One bounded independent diff review found no required findings. It checked private
metadata, transport case, factory privacy/lifetime and owner-thread calls. Separate
loopback probes confirmed both Network/Servers installer orders, incoming TRACK
with clone/copy stream ownership, clean shutdown and no public factory or additional
fetch authority. Broader Request/Headers/Body/signal gaps were not reassessed.

Both installed paths remain the verified v2.22.0 release asset. No publication,
installation, service or noebot change is included in this local continuation.
Reuse completed checks for unchanged source/dependencies on a later release.
