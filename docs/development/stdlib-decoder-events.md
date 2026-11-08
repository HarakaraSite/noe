# UTF-8 decoding and event listeners

This continuation corrects the existing UTF-8 decoder and single-target event
listener contracts. It follows the [Encoding Standard](https://encoding.spec.whatwg.org/)
and [DOM Standard](https://dom.spec.whatwg.org/#interface-eventtarget).

### UTF-8 decoding

TextDecoder keeps incomplete UTF-8 sequences across streaming calls, emits
replacement characters at the standard error boundaries, and handles a BOM even
when its bytes span calls. Fatal errors, end-of-input, input views, labels,
options, and readonly metadata follow the UTF-8 TextDecoder interface. Other
encoding families remain outside NOE's supported surface.

All six UTF-8 labels in the Encoding Standard are accepted, including the
unicode11utf8, unicode20utf8 and x-unicode20utf8 aliases. Label matching trims
ASCII whitespace and ignores ASCII case. Inputs use their exact byte view,
including fixed shared buffers; detached inputs contribute no bytes. Resizable
and growable backing is rejected by the current IDL contract. Options convert
before decoder state changes and before input bytes are copied. Fatal streaming
errors retain the unconsumed input queue. A non-streaming call finishes the
active stream; the following call starts with fresh decoder and BOM state.

### Event listeners

Listeners are identified by event type, callback identity, and capture. Duplicate
registrations keep the original options. Capture and non-capture listeners run
at the single target; NOE does not introduce a DOM tree. Dispatch observes
removal, removes once listeners before calling them, and respects passive
cancellation and signal-bound listener removal. Real abort algorithms remove
listeners before the signal's abort event; synthetic abort events do not abort
signals or remove their bound listeners.

Existing event handler ordering and asynchronous exception reporting remain in
use for network interfaces, messaging, and workers. The public types and consumer
examples must expose the same listener options.

### Compatibility and limits

The listener map and decoder state become private. AbortSignal's existing
readonly aborted/reason attributes read private state so user data fields cannot
substitute for a real abort when registering listeners. Once and signal cleanup
use internal removal instead of calling an overridden public method. Removal and
abort detach listener algorithms; dependent signals with active algorithms remain
retained until cleanup. No new Go Profile field, dependency or host grant is added.

The [DOM dispatch procedure](https://dom.spec.whatwg.org/#concept-event-dispatch)
invokes capture and non-capture phases separately, including at a single target.
Each phase snapshots listeners. A listener added by capture can participate in
the later non-capture phase; removal remains visible in an existing snapshot.
The [abort procedure](https://dom.spec.whatwg.org/#abortsignal-abort)
runs abort algorithms before firing the abort event. Merely dispatching an event
cannot run those algorithms. Installed Deno 2.9.7 differs on both of these cases;
it remains a secondary comparison target rather than the contract authority.

This slice does not add a browser DOM tree, additional encoding families, or
change unrelated AbortSignal construction, default reason or timeout contracts.
TextDecoderStream continues to use the same decoder; its broader interface
shape is not expanded here. Existing legacy opaque clone behavior is retained.

### Verification record

Completed locally on 2026-11-08. Evidence:
`/var/tmp/noe-decoder-events-uagwfg5_`. All seven affected packages pass, with
focused race coverage for decoding, event/abort/GC, messaging, Workers and real
SSE/WebSocket integration. Positive/negative TypeScript fixtures, reference
catalog, JS lint and Go vet pass. The actual CLI example and primary listener
fixture pass. Legacy WebAPIs installation without a Loop also passes.

Checkout and standalone module-artifact Go consumers build and run with the
updated Worker helper and unchanged published dependencies. The previous local
binary reproduces the original UTF-8/BOM and listener defects; the candidate
matches the primary comparison expectations. Deno 2.9.7 agrees on the core
replacement/BOM/dedup/removal/passive cases and differs on the two DOM cases
documented above. Initial oracle mismatches remain in the evidence directory.

The parent reviewed the decoder implementation. A bounded independent listener
review found a public aborted-property check still used by controller abort and
timeout; both now consult private state. The reviewer independently checked the
fix and reported no remaining actionable findings in that scope. Its review
excluded TextDecoder internals and the broader AbortSignal contracts listed
above. Source/consumer continuity, command results and development binary hashes
are retained with the evidence. This request includes a local commit only;
publishing and installation remain separate, and both installed paths still
contain the verified v2.20.0 asset.
