> Historical record of the paused machine-gated implementation. Its technical contracts are superseded by [the agent-first redesign](workflow-agent-first-design.md) and [current implementation record](workflow-agent-first-implementation.md); do not use it to launch or constrain the current workflow.

# Reusable development workflow v1 implementation plan

## Basis and boundaries
Implement R1-R12 in workflow-v1-requirements.md. This is an external JavaScript workflow, not a change to NOE's Go runtime. All explanatory files/prompts are English (user-facing completion can be Japanese). No Decision API, auto-resume, release, provider API investigation, or new permission sandbox.

## Architecture
Keep the portable package under .noe/lib/workflow with a CLI run.js and exported runWorkflow entry point. No imports from other workspace extension libraries. Separate pure protocol validation/state orchestration from injected NOE agent/command/Git runtime operations so deterministic end-to-end tests exercise actual workflow logic and real Git.

The CLI runs with the target Git repository root as cwd. Requirements/config paths are resolved there; module imports remain relative to the portable package. Do not simulate a repository switch solely by putting a path in a prompt. Use JSON configuration for role overrides and an explicit push destination plus an assertion that branch pushes do not trigger releases. Provide --help and non-mutating configuration validation if useful, not a broad option surface.

Persist run artifacts outside staged implementation paths (default Git metadata directory, supporting worktrees through git rev-parse --git-path). Include JSONL stages and final report, complete test stdout/stderr, plan revisions and reviews. A live run is a fresh execution; no automatic resume.

## Contracts
Agent calls receive role-specific instructions plus exact requirements, repository rules, plan, relevant current diff, prior findings and test evidence. Planner returns a JSON plan with stable requirement IDs, acceptance paths, ordered slices with explicit owned paths, difficulty/reason, focused command arrays, and final command arrays/regression rationale. Numbered requirements use declared/caller-supplied IDs; ordinary unnumbered Markdown uses a DOCUMENT coverage group with separate source-anchored acceptance entries for every behavior. Both plan reviewers audit the full original source in either mode, so users need not rewrite settled requirements to add IDs. Missing default root AGENTS.md is allowed; missing explicitly configured rule files fail. Reviews return strict JSON with findings; only an empty validated list permits progress. Findings include requirement/path evidence and a concrete corrective action. Planner/revision/implementation results are validated and never interpreted as unconditional approval. Actual failed commands cannot be waived by an agent's claimed success.

Use keepAlive workers for related repair tasks; retain reviewer conversations where re-review is related, while keeping normal/critical reviewers independent. Reviewer work is read-only by instruction. Version 1 supports the standalone CLI launch path only: the requesting agent executes a NOE subprocess from the target repository, rather than importing the orchestration into a managed run_js call. The exported runWorkflow function exists for standalone composition and injected tests, not for steer-aware managed continuation. The standalone controller intentionally uses completion-only waits (which may be grouped for independent reviewers), with no polling or interactive steer handoff. No in-memory session/advance API or cross-run_js continuation is promised. Stop owned children on failure/exit; do not swallow cancellation. Document that external steering to the requesting agent is not a live reconfiguration channel for the standalone workflow.

Per-slice commits stage explicit paths belonging to that slice, exclude baseline untracked files and artifacts, and detect unexpected tracked changes before committing. At preflight require a clean index/tracked worktree so unrelated existing tracked edits cannot be accidentally attributed; allow existing unrelated untracked files and never stage them. Do not add arbitrary path/type restrictions beyond concrete Git integrity needs. Git commits and final push run only in the orchestrator. Use explicit branch push refspecs and disallow forced push/tags.

## Implementation slices
1. Portable protocol/configuration, execution runtime and planning/review loop. Verify configuration defaults, plan validation, independent repeated reviews, failed/malformed task handling, artifacts and cwd portability.
2. Slice execution/repair/review, scoped Git commits, requirement-level integration/repair and final push. Verify real local Git histories, no premature push, failure reporting and unrelated-file preservation.
3. CLI/launcher, configuration example, agent instruction template and usage docs. Verify entry point/help from another repository and run the comprehensive offline suite. Integration fixes receive additional commits if needed.

For bootstrap, one implementation owner may build the tightly coupled library and tests; parent coordinates reviews, slice commit boundaries, documentation integration and final validation.

## Validation
Tie each check to R1-R12. Use temporary Git repositories and local bare remotes; deterministic fake agents simulate plan findings, code findings, failing tests, integration defects and blocked results, while real commands verify commit/push behavior. Check reusable entry point from a different cwd. Lint/type-check changed JS; no full Go suite is needed because no Go/runtime behavior changes. A focused live provider smoke can verify role config and structured answer parsing only if offline coverage leaves a concrete wiring question.

## Review and completion
Perform independent normal and critical plan reviews with Sol/xhigh until valid findings are resolved. Implement and test, then independent code review; repair related findings and rerun affected checks. Parent runs final combined workflow suite once on the settled candidate. Commit only named new/changed workflow/docs files, preserve existing event logs/__pycache__, and push normal branch changes to existing origin after confirming this repository releases only on v* tags or manual dispatch.
