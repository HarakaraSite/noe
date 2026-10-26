# Workflow v1 slice 1 review

Scope: current requirements/plan and `.noe/lib/workflow/{protocol.js,runtime.js,workflow.js,test.js,API.md}`. Planning-only success is intentional; later implementation, commit/push, and CLI phases were not treated as missing functionality. No implementation modifications or delegation.

## S1-N1 — Ordinary R-number references become phantom requirements (medium, resolved)

**Original evidence (superseded):** `protocol.js:74-78` collects every `R[0-9]+` token anywhere in the original source and requires it in the authoritative inventory, even when it is not a declaration. `runWorkflow` invokes this check before starting the planner.

Focused reproduction against the original implementation:

```js
const source = "# Requirements\n- R1: Import a legacy record whose example record ID is R42 without changing that ID.\n";
requirementInventory(source);          // invalid-protocol
requirementInventory(source, ["R1"]);  // invalid-protocol
requirementInventory(source, ["R1", "R42"]); // accepts phantom R42
```

Both failures say: “requirements contain IDs missing from the inventory; provide complete requirementIds”.

**Product impact:** A valid settled requirements document containing an example record ID, legacy requirement reference, or other non-requirement R-number cannot reach planning with its actual inventory. The caller must change the settled source or invent another requirement and acceptance mapping. This unnecessarily restricts the reusable requirements-input path required by R1/R2. The newer whole-document fallback does not address this case because the source contains a real R1 declaration.

**Fix:** Distinguish declaration evidence from ordinary references. Do not force arbitrary body tokens into a caller-owned inventory; retain full-source coverage auditing for prose and mixed formatting. Add a focused check that a numbered requirement containing an R-number example/reference retains only its real declared IDs and reaches planning with that inventory.

## Focused re-review — current conclusion

**S1-N1 is resolved. No current findings within the re-review scope.**

- The body-token inference is removed. Re-executing the original reproduction now returns only `["R1"]` for both automatic and explicit inventories. Omitting an actually declared R2 still fails with `invalid-protocol`.
- Unnumbered documents, including a document containing only an R42 body example, receive the stable `DOCUMENT` group without source edits. The planner and both reviewers receive the unchanged full source and explicit behavior-by-behavior coverage instructions. Mixed-format/prose behavior remains subject to full-source review rather than being silently exempted by the ID index.
- With omitted `rulesFiles`, an absent default root `AGENTS.md` is recorded and does not block planning. An existing root file is read; explicit configured files remain required reads. Every worker/reviewer is instructed to discover applicable nested rules, regardless of root-file presence.
- Inspected the updated API and focused tests for automatic/explicit R42 handling, unnumbered English/Japanese documents, source-coverage repair with repeated independent reviews, persisted original inputs, missing default rules, and explicit missing-rule failures before agent work. The documented behavior matches these implementation paths.

Validation for this re-review was limited to direct inventory checks and inspection of changed paths/tests. No full suite, provider calls, implementation changes, or delegation.

## Initial-pass observations and validation

- No other concrete findings from this focused pass.
- The current whole-document fallback and optional missing default root rules handling were considered; the earlier unnumbered-document limitation is not reported.
- Verified NOE contracts for `wait_task`, `stop_agent`, `execFile`, and third-argument spawn options. Standalone completion-only waits, separate retained reviewer slots, task-state checks, and shutdown-request semantics are consistent with those contracts.
- Only focused protocol checks were executed to establish the finding. No full suite or provider/live checks were run. Existing offline tests were inspected for the slice's planning/review, failure, Git preflight, artifact, and portability behavior.
