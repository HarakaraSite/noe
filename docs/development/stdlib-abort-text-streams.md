# Abort interfaces and text streams

Approved on 2026-11-08 after v2.21.0. This slice completes existing cancellation
and text-stream contracts under the [API policy](stdlib-api-policy.md). It covers
local implementation, verification and a local commit. Publication, installation,
service restarts and noebot changes require their separate release scope.

### AbortController and AbortSignal

Follow the [DOM abort interfaces](https://dom.spec.whatwg.org/#aborting-ongoing-activities)
and [Web IDL conversions](https://webidl.spec.whatwg.org/). Controllers hold a
private signal exposed through a branded readonly SameObject getter. Signals
have an illegal public constructor and are created by controllers and static
factories. Default abort reasons are AbortError DOMExceptions; timeout reasons
are TimeoutError DOMExceptions. Explicit reasons preserve identity, including
falsy values. Repeated abort preserves the original reason.

timeout requires EnforceRange unsigned long long conversion before allocating
or arming anything. Web IDL limits this JavaScript conversion to the inclusive
range 0 through 2^53-1 after truncation, despite the IDL type's 64-bit name.
Supported delays must not wrap through the ordinary 32-bit
setTimeout delay or overflow Go's time.Duration. Use the existing runtime Loop
and execution-scope lifecycle. Timers must not fire after runtime/task teardown.
No browser page suspension model is introduced for this host runtime.
The legacy WebAPIs installer without a Loop still supports synchronous abort
operations; timeout explicitly rejects TypeError because that installer has no
timer scheduler. Public clearTimeout identifiers cannot cancel internal abort
deadlines. Ordinary setTimeout conversion and delay limits remain unchanged.

onabort uses shared event-handler bookkeeping, preserving object-valued handlers,
registration order,
replacement and removal. Existing internal abort algorithms, synthetic-event
distinction, AbortSignal.any state/reason ordering, retention and cleanup remain
in effect. Fetch and Streams must observe standard default cancellation reasons.

### TextEncoderStream and TextDecoderStream

Follow the [Encoding Standard](https://encoding.spec.whatwg.org/#interface-textdecoderstream)
over the existing TransformStream backend and UTF-8 codec. Expose branded,
readonly encoding/readable/writable getters, plus fatal/ignoreBOM for the decoder.
These interfaces are GenericTransformStreams, not TransformStream subclasses.

TextEncoderStream converts chunks to DOMString and retains a trailing high
surrogate across calls, combining a later low surrogate or replacing it on
incompatible input or final flush. TextDecoderStream requires AllowSharedBufferSource
chunks, retains decoder/BOM state, and flushes or errors at EOF according to fatal.
Preserve backpressure, cancellation and error propagation through the existing
stream backend. Keep UTF-8-only labels and current fixed-buffer IDL rules.

Types, embedded reference entries, examples, public consumers and the session
handoff must agree. No new encoding family, dependency, host capability, Go Profile
field or separate stream engine is added.

### Compatibility and verification record

Code using new AbortSignal() must switch to a controller or a static factory.
Assignments to controller.signal or text-stream endpoints/options no longer
mutate internal state. Default reasons now use the standard DOMException names.
Invalid timeout values reject before scheduling; the accepted JavaScript range
is 0..2^53-1 after truncation. onabort uses LegacyTreatNonObjectAsNull conversion:
non-object values clear it, objects retain their identity and listener slot,
and non-callable objects do nothing on invocation. The shared handler helper
also serves EventSource and WebSocket. Legacy opaque abort-interface cloning is
retained; text-stream wrappers explicitly reject DataCloneError.

Completed locally on 2026-11-08. Evidence is retained at
`/var/tmp/noe-abort-text-streams-o4yke3sm`. All seven affected stdlib/profile /
runtime/CLI packages pass. Focused race covers abort/default reasons, dependent
signals/GC, timeout stages/ownership, timer cancellation/wakeups, pipeTo,
text-stream behavior and Workers. Types, embedded reference catalog, lint/vet,
actual CLI primary/reference/example checks, and checkout/standalone-module
Go consumers pass. The consumer helper checks standard abort/timeout reasons
and split UTF-16 text streams in a real Worker. Legacy Streams installation
before codecs and synchronous WebAPIs installation without a Loop also pass.

The previous installed v2.21.0 binary reproduces the original defects. A common
fixture now matches Deno 2.9.7 for private signal behavior, handler ordering,
default reason types, invalid timeout inputs, stream metadata and split UTF-16.
Full primary assertions remain separate: Deno accepts an unbranded onabort
accessor receiver and exposes a different optional-argument abort method arity.
Initial oracle mismatches are retained rather than weakening product tests.

A bounded independent abort/timer review found two P2 issues in the initial
implementation: the 64-bit IDL conversion must use JavaScript's 2^53-1 limit,
and object-valued onabort assignments must preserve an inert handler slot.
Both were fixed with regressions. One fix-only re-review confirmed both and
reported no material residual within the fixes. Text-stream internals were
reviewed by the parent and were outside that independent review's scope.

The development candidate, runtime comparison, exact source/consumer continuity,
and per-command logs remain in the evidence directory. Publication and installation
are not included in this request; both existing executable paths remain v2.21.0.
Reuse these results for unchanged source/dependencies when release is requested.
