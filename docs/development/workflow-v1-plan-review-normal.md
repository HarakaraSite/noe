# Workflow v1 plan review — normal

Reviewed `workflow-v1-requirements.md`, `workflow-v1-plan.md`, and repository `AGENTS.md`. Scope: requirement completeness, viable NOE API use, and implementation/validation contracts. No implementation changes or delegation.

## Finding N1 — Define a viable state handoff for managed callers (medium, resolved)

**Original evidence (superseded by the revision):** `workflow-v1-plan.md:16` required managed callers to return immediately on `wait_tasks` steer while preserving an “in-memory workflow continuation” through an exported session/advance API.

The installed NOE references `docs("noe:run_js")` and `docs("noe:run_js", "agents")` establish that every subsequent tool call has a fresh runtime/module cache: local variables and module state do not persist, but child agents remain owned by the parent task across calls. `docs("noe:agent", "wait_tasks")` requires returning to the model on steer without stopping those children.

**Impact:** On the advertised managed-library usage path, the first steer destroys the current JavaScript continuation. Merely exporting a live session object/function does not retain the current stage, outstanding command IDs, and completed side effects for a later call. The plan leaves the handoff contract unresolved.

**Fix:** Choose an explicit contract before implementing this path. Either make the initial entry/launcher explicitly standalone-only, or define a JSON-serializable session/checkpoint returned or persisted before yielding and accepted by a fresh `advance` call in the same owning task. Retain pending agent/command IDs and completed stage/commit state, and distinguish steer from failure cleanup. This is same-task continuation, not the deferred automatic resume of interrupted runs. For managed support, verify one steer followed by advancement in a fresh runtime without relaunching pending work or duplicating Git mutations.

No other findings from the initial focused pass. Release-trigger evidence checked: `.forgejo/workflows/release.yml:4-8` uses tag pushes (`v*`) and manual dispatch, consistent with the plan's repository-specific assertion.

## Focused re-review — current conclusion

**N1 is resolved. No current findings.**

Re-read the revised plan and requirements. `workflow-v1-plan.md:16` now explicitly limits orchestration to a standalone NOE subprocess launched from the target repository, restricts the exported `runWorkflow` to standalone composition/injected tests, and disclaims managed in-process continuation. It specifies completion-only waits, owned-child cleanup on failure/exit, cancellation propagation, and no live reconfiguration through steering of the requesting agent.

This selects the standalone alternative in N1 rather than relying on state surviving a fresh `run_js` runtime. Completion-only waits are appropriate for the standalone controller, consistent with the verified NOE agent contracts. The launch must remain awaited within its owning command/tool runtime, as required by NOE subprocess ownership; the revised plan does not promise detached execution.

The revision introduces no concrete requirement problem: R1 requires a reusable library/CLI and agent-launch template, not steer-aware managed continuation. The standalone library/CLI contract preserves that requirement and does not change R2-R12's review, repair, validation, commit/push, persistence, or version-1 exclusion requirements.

Re-review scope was the resolution and consequences of this revision only. No implementation changes or delegation.
