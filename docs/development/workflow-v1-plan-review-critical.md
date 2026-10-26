# Workflow v1 plan: critical review

Reviewed workflow-v1-requirements.md, workflow-v1-plan.md, AGENTS.md, the documented NOE task/runtime contracts, and the repository's Git/release usage path. One focused pass; no implementation changes or delegation.

## Current conclusion after focused re-review

**No unresolved findings.** The prior P2 finding is resolved by workflow-v1-plan.md:16. Version 1 now explicitly supports only a standalone NOE CLI subprocess launched from the target repository. The exported runWorkflow function is for standalone composition and injected tests, not managed in-process continuation. Completion-only waits, child cleanup, cancellation propagation, and the absence of live steering through the requesting agent are explicit.

This is consistent with R1: a copyable library/CLI and agent-launch template are still provided, with correct repository cwd. The requirements do not require interactive steering or managed in-process execution. The documented NOE wait contract permits completion-only waiting without a caller owner run, and subprocess ownership ties the controller's lifetime to its launching runtime. No cross-run_js state preservation is needed. R9's diagnostic artifacts and no-automatic-resume boundary remain intact. No concrete new requirement problem was introduced by this revision.

This re-review covered the revised launch/lifecycle contract and its requirement implications; no implementation changes or delegation.

## Resolved finding (original review retained for context)

### P2 — Define a continuation contract that survives a fresh run_js runtime

**Status: resolved.** The evidence and affected path below describe the earlier plan revision, not the current standalone-only contract.

**Evidence:** workflow-v1-plan.md:16 promises immediate return on `wait_tasks` steer for managed tool callers, but proposes preserving an **in-memory** continuation through an exported session/advance API. NOE explicitly discards variables and module state between `run_js` calls (internal/agent/facts.md:1; `docs("noe:run_js")`). The `wait_tasks` contract requires finishing the current tool call on steer and using a later call to continue; children remain running.

**Affected path:** An agent invokes the exported workflow, receives steering while a worker/reviewer is running, returns to its model, then invokes the workflow again. A module-scoped session or suspended JavaScript continuation cannot survive that boundary. The plan does not define how the next call recovers the workflow phase, owned task IDs, or already-completed actions. This leaves the explicitly supported managed-call path implementation-blocking; keeping the original tool call alive instead would violate the steer contract.

**Fix:** Choose and document the supported launch contract. If managed in-process calls remain supported, make advance return/persist a JSON-safe checkpoint containing the orchestration state and owned task IDs, and define how a fresh invocation loads it under the same owner task without restarting workers or replaying commits. Verify an intentional steer checkpoint across fresh invocations. Alternatively, explicitly limit the launcher to the standalone CLI path and remove the unsupported in-memory managed-call promise. Intentional checkpoint continuation does not require automatic recovery after an interrupted owner task, which remains out of scope.

No other concrete findings from this pass. In particular, Decision API/OpenAI integration remains deferred, and the checked repository release workflow is tag/manual-dispatch only.
