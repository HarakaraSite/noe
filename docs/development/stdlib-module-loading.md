# Phase 7: data imports and import-map expansion

[Roadmap](stdlib-roadmap.md) · [User guide](../import-map.md) · [Go embedding](../go-embedding.md)

Implemented locally on 2026-11-06 after phase 6 was committed as `baa984d`.
This phase is included in [v2.15.0](../releases/v2.15.0.md).

## Implementation

The pinned go-quickjs v0.32.0 engine already implements static
`with {type: "json" | "text" | "bytes"}` attributes and dynamic
`import(specifier, {with: {type: "json"}})` options. NOE's standard module
loader now returns local sources without checking their filename extension,
and returns HTTP response-body bytes without MIME/UTF-8 validation or
BOM removal.
JSON parsing and typed namespace identity remain engine-owned. No JSON-to-JS
wrapper, Go JSON decoder, dependency update, or engine fork is introduced.
Raw local and HTTP response-body bytes are retained, including BOMs, zero
bytes, and invalid UTF-8, so byte imports preserve the resource exactly.

The import map accepts bare prefix keys ending in `/` and referrer scopes.
Native Windows path targets may also use their trailing `\` separator.
Exact mappings precede the most specific prefix. Matching scopes are tried
from most specific to least specific, followed by global imports. Scope
keys without a trailing separator match an exact referrer; directory scopes
also match descendants. A matched prefix cannot resolve outside its mapped
base, and a matched invalid expansion does not fall back. Targets inside a
scope remain relative to the map's directory, rather than the scope.

Resolution follows the supported part of the
[HTML import-map model](https://html.spec.whatwg.org/multipage/webappapis.html#resolve-a-module-specifier).
NOE additionally supports native paths and rooted virtual filesystem paths,
and reports configuration errors strictly. Bare mapping keys, string targets,
built-in reservations, strict JSON validation, and the existing local/HTTP
canonical-ID model are preserved. Path/URL remapping keys, null blockers,
integrity, merging, npm resolution, and source transformation remain outside
this phase. Normalized duplicate scopes are rejected deterministically.

`Snapshot.Scopes()` returns a deep copy, and snapshot JSON transports both
map levels. The CLI profile retains scopes when creating its public loader
provider. That same snapshot reaches Workers, agent bootstrap, run_js, and
child agents without rereading the original import-map file. HTTP dependencies
can use prefix/scoped mappings for JavaScript and data; a remote referrer's final URL
selects its scopes after redirects. Virtual entry imports use the existing
`<virtual-entry>.mjs` execution path for scope matching.

The public Go `LoaderOptions.Scopes` field provides the same functionality
through `ModuleLoader`; both imports and nested scopes are captured when
the provider is created. `FileLoader` keeps its existing signature and now
supports local prefixes. Selecting a loader grants no filesystem access;
local data uses the execution's existing Root/Cwd/ReadOnly filesystem view.
HTTP remains an independent explicit capability for Go hosts, including
HTTP targets in scopes that have not been used yet.

## Source and value lifetime

Each runtime retains one successful raw read per canonical resource, shared
across requested types. The engine identifies a namespace by resource ID
and type: JSON aliases share their default object; a text or bytes import
of that resource has a separate namespace. JSON values are mutable, while
byte modules expose immutable ArrayBuffers. A fresh execution receives fresh
loader sources and engine values. Failed reads remain retryable.

Plain imports continue to execute JavaScript regardless of suffix. A JSON
filename alone does not select JSON, and arbitrary suffixes do not enable
CommonJS or TypeScript. Worker entries also execute JavaScript; their
imports can request typed data.

## HTTP response-body policy

The user approved completing HTTP data imports entirely within NOE, without
requesting upstream changes. The public engine loader has no requested-type
argument, so NOE treats HTTP as a raw source provider for every import type,
just as it does local files. Response-body bytes are returned unchanged and
cached once per final URL. The engine selects JavaScript, JSON, text, or bytes
and maintains a separate namespace for each URL/type pair.

Content-Type and charset do not select or restrict interpretation. Missing,
malformed, and arbitrary MIME metadata are accepted. A plain import asks for
JavaScript even when a response is labelled as JSON, HTML, or binary data.
There is no BOM removal, UTF-8 validation, or charset transcoding. Text keeps
a leading BOM; byte imports preserve it and any invalid UTF-8. The current
engine rejects BOM-prefixed JSON, matching local JSON import behavior, while
its JavaScript lexer accepts a leading BOM. A JSON parse failure does not
change the retained source used by a later text or byte import.

Standard transport decompression still applies, so byte imports expose the
bytes read from `http.Response.Body`, not the compressed wire representation.
HTTP status validation, request/redirect policy, body limits, borrowed-client
ownership, and cancellation keep their existing roles. Worker entry URLs
remain unsupported. No new public Go field, engine patch, dependency, or
upstream API extension is needed for this HTTP follow-up. The API limitation
and local resolution are recorded in
[UPSTREAM.md](../../internal/stdlib/UPSTREAM.md#engine-loader-api-limitation-typed-module-requests-2026-11-06).
No upstream request is planned or submitted.

## Compatibility

Ordinary valid JavaScript imports need no source migration. Prefix/scoped maps and local
data targets that were previously rejected are now accepted. A plain import
of a non-JS suffix reaches source loading and engine parsing, so its error
may differ from the old extension check. No compatibility adapter is added.

HTTP now accepts responses formerly rejected by the MIME/UTF-8 checks. In
particular, JavaScript can execute regardless of its response MIME. Text
retains a BOM instead of stripping it; JSON previously loaded through a text
MIME with a BOM now fails engine parsing. Invalid UTF-8 reaches the engine
rather than failing at the HTTP loader. These changes were explicitly
approved by the user; no compatibility adapter is added.

Adding `LoaderOptions.Scopes` affects external unkeyed Go literals: named
fields preserve the earlier settings without supplying Scopes. All inspected
in-repository uses already name their fields, so no such migration was needed.
`FileLoader`, loader-provider interfaces, native module precedence, and the
selected filesystem's capability boundary retain their contracts.

## Validation

Initial local-data/prefix/scope validation passed before the HTTP policy
follow-up:

- Unit suites for modulemap, CLI profile, CLI, runtime, execution bindings,
  API catalog, agent core, public stdlib, and public agent. External portable
  consumer unit wrappers were skipped because the artifact checks below
  exercise those consumers directly.
- Race checks for prefix/scope resolution, loader source identity, local data
  imports, Worker use, and HTTP scope/MIME behavior, plus the complete
  modulemap race suite after the final separator fix.
- CLI run/eval and Worker data imports, including an arbitrary Worker source
  suffix; JSON/text/bytes imports over captured maps; agent bootstrap/run_js after
  deleting the original map; concurrent roots and child agents using distinct
  scope snapshots; existing import.meta behavior.
- Rooted ReadOnly filesystem reads, denied symlink escapes, no implicit FS
  grant, nested option-map mutation isolation, raw bytes/BOM preservation,
  JSON alias identity and JavaScript JSON semantics, extensionless targets,
  final-HTTP-referrer scope selection, and the initial HTTP MIME policy
  (replaced by the approved raw-body behavior below).
- Public stdlib consumer through a checkout replacement and a standalone
  module artifact, including data imports and scoped/prefix mappings. Builds
  passed for Linux, Darwin, and Windows on amd64/arm64. Native execution ran
  on Linux. Public agent consumer passed through both replacement/artifact
  dependencies in portable and Go-host modes.
- Affected-package vet, relative documentation links, and whitespace checks.
- Independent review of resolution, transport, and capability boundaries.
  A virtual HTTP prefix ending in a native backslash was initially accepted
  but could not round-trip after URL canonicalization. Initial parsing now
  rejects it; the fix and Windows native relative-prefix support were
  rechecked. No actionable review findings remain.

Windows-specific resolver tests also cross-compiled; they were not executed
on this Linux host. The initial consumer check exhausted the `/tmp` tmpfs
while copying build outputs. Evidence was moved to the root filesystem and
only unfinished builds/runs were resumed; all completed checks were reused.

Logs, consumer source manifests/artifacts, and completion records are under
`/var/tmp/noe-module-loading-5rpc_di3/` (also reachable through the original
`/tmp/noe-module-loading-5rpc_di3/` alias). The initial validation was completed locally before the HTTP follow-up. No paid provider, publication, installed-binary replacement,
or service restart was performed.

## HTTP follow-up validation

The approved raw-body implementation and verification are complete. Checks
passed on Linux:

- Full public stdlib, agent core, public agent, API catalog, runtime, and CLI
  unit suites. External portable consumer unit wrappers were replaced by the
  direct checkout/artifact consumer checks below.
- HTTP loader race tests covering raw bytes, redirects and final IDs, borrowed
  client/body ownership, cancellation, permission callbacks, and body limits.
- Static and dynamic HTTP JSON/text/bytes imports with application/json,
  application/octet-stream, image/png, missing MIME metadata, and a plain
  JavaScript import served as text/html. Low-level checks also accept malformed
  MIME metadata and preserve invalid UTF-8.
- Shared raw source across JSON/text/bytes aliases and a redirect, retaining
  a single resource read. BOM-prefixed JSON fails engine parsing while later
  text/byte imports preserve the original BOM and body.
- CLI run/eval, module Workers, fixture-agent run_js, existing URL imports
  through child agents, scope snapshot transport, and import.meta behavior.
  Worker entry rejection still performs no HTTP request.
- External public stdlib consumer through a checkout replacement and a
  standalone module artifact, including HTTP JSON/text/bytes imports without
  enabling FS or fetch. Public agent consumers also passed both dependency
  routes in portable and Go-host modes.
- Affected-package vet, Go formatting, relative documentation links, and
  whitespace checks. Earlier native-path and six-target cross-build evidence
  remains above; this follow-up adds no platform-specific implementation.

Logs, exact-source manifests, consumer artifacts, and check results are under
`/var/tmp/noe-http-data-m4spw6b5/`. This supersedes the earlier HTTP MIME rejection
checks and deferred-HTTP notes. The implementation is included in v2.15.0. No paid provider, upstream request, engine change, installed-binary
replacement, or service restart was performed.
