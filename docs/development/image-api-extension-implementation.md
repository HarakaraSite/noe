# Image API extension implementation

Implementation date: 2026-11-01. Baseline source:
`fedad4de3bd4e3d6c50412bf3621de0082b9d750` (v2.10.1).
The [design](image-api-extension-design.md) defines the contract;
[results](image-api-extension-results.json) record checks and live evidence.
This records implementation and local validation; publication is tracked in
the [release checkpoints](../releases/release-checkpoints.md).

## Implemented behavior

- `edit_image` accepts ordered local image paths, snapshots the execution's
  actual filesystem inputs, sends image bytes, validates the returned image,
  and saves one output atomically. It is also re-exported from `noe:model`.
- `openai-codex-images` is a separate image-only provider with JSON generation
  and editing endpoints, a curated `gpt-image-2` entry, and explicit credential
  ownership. Existing public plan-sharing and API-key routes stay distinct.
- Managed credentials have their own schema and native loopback OAuth/PKCE
  helper. Expiry refresh and one 401 recovery use cross-process locking and
  rejected-generation comparison. External Codex credentials are read-only;
  they do not create lock files, refresh, or fall back to an API key.
- `model.Service` keeps its old required methods. The optional
  `model.ImageEditor` capability survives public service dispatch, scoped
  operations, binding, and the CLI's lazy model service. Custom operation
  selection cannot silently switch to a built-in account.
- Existing image event names now distinguish generation/editing. Structured
  errors include operation/stage; known usage/request identity survives
  cancellation or provider/response/save failure and is accounted once.
  Explicit `read_image` retains its separate vision attachment semantics.

The native helper, public Go/auth options, declarations, bundled references,
provider/image/embedding guides and an editing example are included. Masks,
API-key/OpenRouter editing and multiple output images remain outside this slice.

## Deterministic verification

The whole-tree unit run and vet pass. Focused credential/auth and image
lifecycle race checks cover refresh rotation, concurrent 401s, cancellation,
close, and accounting. TypeScript declarations pass positive/negative compile
checks. External local/artifact agent consumers build and run in portable and
gohost modes; a separate public Go consumer verifies that a legacy Service
implementation still compiles and the optional editor can be selected explicitly.

Fixtures cover ordered image contents, metadata, input snapshot ownership,
root/readonly failures, unsupported dispatch without fallback, structured error
classification, redirect header protection and external credentials remaining
unchanged. Run_js integration delivers actual fixture bytes to later model
requests after both generation and editing. Unknown usage remains absent, and
save observations do not duplicate known provider consumption.

Initial whole-tree checks exposed stale catalog counts and runtime fixtures
that inherited external instructions while estimating only built-in facts.
Catalog expectations include the new image model. The built-in-facts/context
fixtures now explicitly select an empty external instruction list, making
their own assertions independent of the user's instruction files. No user
instruction content was changed for these checks.

## Actual subscription workflow

The authorized live validation used the actual built CLI, existing Codex
credentials in `external-readonly` mode, `openai-codex-images/gpt-image-2`, and
`openai-chatgpt/gpt-6.1-terra` as the vision/conversation model.

The first agent run generated and saved a mint green square and successfully
attached it with `read_image`. It then exposed a missing optional-editor method
in the CLI lazy-service wrapper; that attempted edit stopped locally without
reaching the image backend. The wrapper was corrected and a focused regression
test was added.

A continuation reused the existing original rather than generating again. It
read that original, received the image on the next actual model request, sent
the original PNG bytes through `edit_image`, saved a separate revised image,
and attached it with `read_image`. The next model response described the
original as mint green and the revision as orange. Both local PNGs were also
opened and visually inspected by the primary agent. Shape/layout were visually
similar; exact preservation of other pixels is not claimed.

Actual image-service requests across these runs: **one generation and one edit**.
Both outputs are 1254 by 1254 despite the requested 1024 by 1024 size. The two
run ledgers match the sum of their distinct reported model and image usages;
image save events were excluded from the sum. Checks of live output/events
found no selected credential strings, image data URLs, or base64 payloads.

Artifacts, scripts, original failures, corrected continuation, events, hashes,
request IDs and usage are retained under
`/tmp/noe-image-implementation-evidence/`. The JSON results preserve the
sanitized evidence and both output hashes.

## Accepted limits and review

The user explicitly skipped native NOE fresh-login confirmation. Its pending
browser attempt was cancelled; no managed grant was created or copied from
the active Codex session. Native login and managed refresh have deterministic
fixture/race evidence, not a real-account confirmation. This accepted gap is
separate from the successful external-readonly live image/vision workflow.
Additional formats/input counts, transparency and live quota/cancellation
failures were not exercised by spending more requests.

Independent code review identified one P2 lifecycle issue: public Go login
completion could lose the attempt's cancellation lifetime after the callback
when the caller supplied a separate context. It was corrected by merging both lifetimes and joining active completion on Close;
blocked exchange fixtures cover both Close and Begin-context cancellation, and
focused re-review confirmed resolution. External 401 recovery also compares
available selected user identity within the same account. The two earlier design review supplements (401
generation-aware recovery and cancellation accounting) were confirmed in the
implementation. Final review disposition is recorded in the JSON results.
