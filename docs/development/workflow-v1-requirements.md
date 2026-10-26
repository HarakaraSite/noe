> Historical record of the paused machine-gated implementation. Its technical contracts are superseded by [the agent-first redesign](workflow-agent-first-design.md) and [current implementation record](workflow-agent-first-implementation.md); do not use it to launch or constrain the current workflow.

# Reusable development workflow: version 1 requirements

## Objective
After requirements have been settled, a user can ask an agent to implement a requirements file. The agent starts a reusable NOE JavaScript workflow which proceeds without per-stage approval through planning, review, implementation, validation, per-slice local commits, and one final push. The workflow must also work in other Git repositories without NOE source-tree dependencies.

## Required behavior
- R1: Provide a self-contained, copyable JavaScript library/CLI under .noe/lib/workflow, an example entry point, repository configuration, and an agent-launch instruction template. Run NOE from the target repository so workspace instructions and child working directories are correct.
- R2: Read the supplied requirements file and repository work rules. Create a plan with requirement IDs/acceptance behavior, preferably vertical slices, ordered dependencies, implementation paths, focused verification commands, and final requirement-level integration verification. A single slice is allowed when splitting adds no value.
- R3: Run independent normal and critical plan reviews on the same plan revision. Revise and run both again until no valid unresolved findings remain. No arbitrary revision-count cap. Critical review challenges assumptions and missing usage paths, not a quota of objections.
- R4: For every slice, implement, execute focused tests/checks, independently review code, fix failures/findings, and rerun affected checks/review until complete. Test evidence must concern required product behavior rather than test counts. Only then create a local slice commit.
- R5: After all slices, run comprehensive requirement-level validation, including cross-slice usage paths. Run product-wide regression checks when justified by impact, and optional minimal real-provider checks when needed. Record why broader checks are or are not needed. Fix integration defects, rerun relevant verification and code review, and create additional correction commits.
- R6: Push the completed branch once after final validation. No force push, tags, release, or automatic rewriting of existing commits. A push failure reports committed-but-not-pushed state and retains local commits.
- R7: Defaults: planning/general work openai-chatgpt/gpt-6.1-sol/high; all reviews openai-chatgpt/gpt-6.1-sol/xhigh; routine implementation/exploration openai-chatgpt/gpt-6-luna/max; difficult implementation/exploration openai-chatgpt/gpt-6.1-sol/high. Select difficulty per slice with a reason, allow escalation when concrete difficulty emerges, and allow configuration overrides.
- R8: Version 1 excludes all Decision API calls, including OpenRouter/Jev. Do not replace the requested future OpenAI decision gate with Jev. Use validated structured agent results for review/control flow and actual command/Git results for mechanical success.
- R9: Persist the plan, stage/task records, findings, test outputs, commit IDs and final outcome. Failure must not be reported as completion. Record enough context for a later agent to diagnose an interrupted run; automatic resume is NOT required.
- R10: Preserve pre-existing unrelated changes. Stage explicit workflow-owned paths, never git add -A over the repository. Do not commit logs, credentials or unrelated files. The workflow owns Git mutations; worker/reviewer prompts forbid committing, pushing, release operations, weakening requirements, and hiding test failures. This is a trusted-agent workflow, not an OS sandbox.
- R11: Git branch/remote/push destination and release-trigger considerations are explicit repository settings. Refuse detached HEAD, unknown push destination and known unsafe/unconfirmed release-trigger destinations before implementation. Do not assume every repository's main branch is safe. Existing upstream may be used after repository configuration confirms branch-push safety.
- R12: Absent credentials/services or contradictory requirements may block work; normal test failures and valid review findings lead to repair loops. Failed or malformed agent responses must fail closed, not count as approval. No automatic paid provider calls beyond work needed for this task. Optional live checks must be explicit in the plan.

## Version 2, explicitly deferred
Investigate the user's intended OpenAI decision API against authoritative specifications, define its contract, add NOE support if needed, and add decision gates to a revised workflow. Define those requirements after version 1 is complete, then use version 1 to carry out the revision. Do not research or implement that integration as part of version 1.

## Completion evidence
- Offline workflow execution against temporary real Git repositories and local bare remotes, with deterministic injected agent responses, verifies review/repair loops, slice commits, final integration fixes, final push, and failure reporting.
- Focused negative checks verify malformed approvals, required test failure, preservation of unrelated files, configuration and Git destination errors.
- Syntax/lint and applicable JavaScript type checks pass for changed JavaScript.
- A minimal real-provider smoke check may verify NOE task/result wiring; no full paid implementation workflow or version-2 work is needed for first-version completion.
