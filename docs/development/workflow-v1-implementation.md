# Reusable development workflow v1 implementation record

## Scope
Bootstrap the external portable JavaScript workflow described in [requirements](workflow-v1-requirements.md) and [plan](workflow-v1-plan.md). No Go runtime/API changes, Decision API calls, release, or version-2 provider investigation.

## Plan review
Independent normal and critical Sol/xhigh reviewers found a managed-call continuation ambiguity. The plan was changed to standalone CLI execution from the target Git root, with intentional completion-only waits and no automatic resume. Both reviewers confirmed resolution with no remaining findings. See the two plan-review documents.

## Slice 1: portable protocols, runtime and planning
Implemented self-contained modules under .noe/lib/workflow. Strict JSON results separate a plan from completed implementation; a blocked/failed/malformed response cannot approve. Independent retained normal and critical reviewers evaluate the same revision and both repeat after repairs. Artifacts preserve source, prompts/results and plan/review history under worktree-safe Git metadata.

Actual input-path fixes:
- Plain Japanese/English Markdown without explicit IDs is supported through a DOCUMENT coverage group, source-anchored acceptance entries and full-source coverage reviews.
- Default absent root AGENTS.md is allowed; explicitly missing rule files fail, and prompts require discovering applicable nested work rules.
- Ordinary body references such as record R42 do not become phantom requirements.

Implementation-owner validation: deterministic offline checks cover real temporary Git preparation and injected agent loops, malformed/stale approvals, coverage repairs, cleanup, destination safety and the above requirements-input paths. Biome lint and NOE-declaration-based tsc pass. No live provider calls were needed for this slice. Independent code review found the R42 issue; its focused re-review is recorded separately.

## Remaining bootstrap stages
Slice 2 adds per-slice implementation/test/review/commit plus final integration repair and push. Slice 3 finishes CLI, examples, configuration and launch documentation. Final requirement-level validation is performed on the settled combined candidate; this document will record its evidence and any limitations.
