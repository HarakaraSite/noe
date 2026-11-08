# Encoding, User Timing, and CustomEvent

Shipped in [v2.21.0](../releases/v2.21.0.md). The implementation-stage evidence
and scope decisions below are retained as historical records.

Approved on 2026-11-08 after the Deno comparison: correct TextEncoder.encodeInto,
replace empty Performance methods with useful User Timing, and add CustomEvent.
The [API policy](stdlib-api-policy.md) applies. This is local implementation;
publication, installation, noebot and service changes are outside this request.

The subsequent [decoder/listener continuation](stdlib-decoder-events.md) corrects
UTF-8 decoding and extends the shared EventTarget listener semantics. This file
retains the completed earlier slice and its original evidence.

## Contracts and references

- [Encoding Standard](https://encoding.spec.whatwg.org/#dom-textencoder-encodeinto):
  write complete UTF-8 sequences, consume source UTF-16 code units only when
  the complete scalar value fits, and return accurate read/written counts.
  Replace unpaired surrogates, respect destination offsets, accept genuine
  Uint8Array views including fixed shared backing, and follow Web IDL argument
  and receiver conversion. Detached destinations are empty views. Resizable /
  growable backing requires the separate AllowResizable annotation, absent
  from this operation; do not accept it merely because another runtime does.
- [User Timing](https://www.w3.org/TR/user-timing/),
  [Performance Timeline](https://www.w3.org/TR/performance-timeline/) and
  [High Resolution Time](https://www.w3.org/TR/hr-time-3/): implement mark,
  measure, getEntries/getEntriesByName/getEntriesByType, clearMarks/clearMeasures,
  now/timeOrigin and toJSON. Expose Performance, PerformanceEntry,
  PerformanceMark and PerformanceMeasure with branded readonly fields and
  the specified constructor restrictions. Copy detail through structured clone
  on entry creation; standalone marks do not enter the global buffer. Keep
  chronological stable entry ordering, latest-name lookup, overloads and
  specified validation/errors. Failed creation adds no entry. Each runtime /
  Worker has its own buffer and clock origin. No artificial entry quota.
- [DOM CustomEvent](https://dom.spec.whatwg.org/#interface-customevent): extend
  the existing Event interface, retain detail by reference with a getter-only
  branded attribute, honor EventInit flags/defaults and DOMString conversion,
  and support legacy initCustomEvent outside dispatch. Readonly inherited event
  state and dispatch bookkeeping must support these semantics through the
  existing EventTarget and event-handler path, without a separate dispatcher.
- [Web IDL](https://webidl.spec.whatwg.org/) controls conversions. Another
  runtime is an independent oracle, not a replacement for primary procedures.

PerformanceObserver, resource/navigation timing, browser DOM trees and expanded
TextDecoder encodings are not added. Performance Timeline's newer id/navigationId
fields remain outside the selected User Timing surface. Do not claim full
Performance Timeline or DOM conformance. No capability, Go Profile field,
native dependency, timer, goroutine or service is added.

## Compatibility and integration

Methods that previously returned undefined without recording anything now
return real entries and reject invalid arguments. Performance objects/entries
are non-serializable platform objects and reject DataCloneError rather than
silently losing state. CustomEvent is also non-serializable; detail itself is
an ordinary reference and can contain values that cannot be cloned. Supported
Event attributes become readonly and dispatch uses internal state.

Keep globals, ambient declarations, examples, public Go consumers, embedded
reference entries and the session handoff aligned. Verify default profiles,
Workers and the legacy WebAPIs installation. Run focused behavioral tests,
compare representative cases with installed Deno 2.9.7 and the primary text,
then run affected tests/types/docs and external consumer checks. Reuse existing
evidence for unchanged paths; this is not a release preparation task.

## Work record

Implementation and local verification are complete. Evidence directory:
`/var/tmp/noe-web-basics-ivf40e59`. Installed binaries remain v2.20.0; no release,
push or placement is included in this task.

The original encodeInto, CustomEvent and Performance regressions all fail on
v2.20.0. Affected stdlib/profile/runtime/CLI packages pass. Focused race tests
cover new APIs, Event/messaging/abort integration and Workers. Positive/negative
TypeScript fixtures, embedded reference catalog, lint/vet, the actual CLI
example, and legacy WebAPIs installation without a Loop pass.

An independent bounded review found three Performance binding defects: null
measure options selected a string instead of a dictionary, measure members
converted in declaration order instead of lexicographical order, and the two
entry-specific detail getters accepted the other entry class. All were fixed
with a baseline-failing regression. One fix-only re-review confirmed those
ordinary repros and found a remaining null-to-object conversion that could
read inherited dictionary members. The parent corrected omitted/undefined/null
defaults for both mark creation paths and measure, added baseline-failing
regressions, and verified the final source under focused race. Optional method
arities also match their required argument counts. No additional independent
re-review of these final defaults/arity adjustments is claimed.

Checkout and standalone module-artifact Go consumers build and run with the
new main/Worker helper. All 314 runtime/dependency/reference paths match that
artifact except the final Performance default-option/arity adjustment; its
exact small delta and targeted verification are recorded in
consumer-source-continuity.json. Packaging, Go APIs and dependencies are
unchanged. Completed broad checks are reused rather than repeated for these
last binding adjustments.

The final development binary and runtime-summary.json are in the evidence
directory. A common encoding/Event/User Timing fixture matches installed Deno
2.9.7. Primary-specification cases are checked separately: Deno accepts
resizable encodeInto backing, lacks initCustomEvent, uses different cloning /
null dictionary behavior, and reports undefined for default CustomEvent detail.
The DOM default is null and remains asserted in NOE's public tests. The initial
oracle mismatch is preserved; moving it out of the common-equality fixture did
not weaken the product test. Deno's detail-only measure acceptance also differs
from the selected User Timing draft procedure.

This is the specified local slice, not a complete DOM, Performance Timeline,
Encoding, or cross-platform conformance audit. No new algorithm family,
TextDecoder encoding, observer subsystem, permission or dependency is added.
