> Design-stage snapshot: the user subsequently authorized implementation. The earlier design-only stop applied to that stage. See [the implementation record](workflow-agent-first-implementation.md) for the current result.

# Agent-first development workflow: proposed redesign

## Status and scope
Design only. Stop after this document; do not implement, test, commit or push.
This proposal replaces the technical architecture in workflow-v1-plan.md, not the user's agreed product behavior. The previous implementation remains paused and is not an approved foundation. Do not automatically run its launcher or treat its strict protocols/Git controller as requirements for the redesign.

## Principles
1. Maximize agent judgment. Planning, interpretation, review, completion decisions, test selection and Git operations are agent work unless there is concrete reason to do otherwise.
2. Consider mechanical processing for deterministic operations that actually recur. Do not prebuild a general framework for hypothetical cases. Use NOE and ordinary Git/filesystem tools before writing wrappers.
3. A fixed procedure is not the same as a machine-enforced state machine. Fix the required stages and responsibilities in the parent's instructions; let the parent interpret evidence and progress through them.
4. Keep implementation proportional: a thin launcher, a workflow instruction document, and role/model settings. No custom decision service, plan/review JSON protocol, Git integrity engine, ownership-amendment protocol or artifact subsystem in the initial version.

## User-visible behavior retained
- A user requests implementation of a settled requirements file.
- The workflow works in other repositories, starting from their actual working directory and reading their requirements and work rules.
- Plan, preferably in useful slices; independently review the plan normally and critically; repair/re-review until valid findings are resolved.
- For each slice: implement, test, independently review, repair/recheck, then make a local commit.
- After all slices: verify the complete requirements and cross-slice behavior; run product-wide regression checks and minimal live-provider checks when their value is justified by the change.
- Fix integration defects, review/recheck affected behavior, and add correction commits when needed.
- Push once after overall validation. No per-slice push, release, tags, force push or automatic history rewrite.
- Preserve unrelated existing work. Report genuine blockers and actual failed delivery honestly.
- No Decision API, including Jev. OpenAI decision integration is a separately defined later revision, to be exercised using the first workflow.

## Structure

User request
  -> thin JavaScript launcher
      -> workflow parent agent (Sol/high)
          -> optional exploration child (Luna/max; Sol/high for difficult work)
          -> implementation child for a slice (same routing)
          -> independent normal plan-review child (Sol/xhigh)
          -> independent critical plan-review child (Sol/xhigh)
          -> independent code-review child (Sol/xhigh)
          -> optional validation child when there is substantial independent work

The launcher's job is to supply the requirements path, role settings and workflow instructions, start the parent with explicit permission to delegate, wait for its result, and surface failure/cancellation. It does not interpret review prose or decide whether a slice is done. Use native NOE lifecycle/results/events.

When an existing user-facing agent starts the launcher, it is only the requester/launcher: it does not become a second workflow coordinator or independently repeat all reviews. The dedicated workflow parent owns the work. A direct CLI launch starts the same parent. No in-process resumable workflow API is promised.

The parent may create children but children do not recursively launch the workflow or delegate further by default. Parent and children are trusted agents sharing the repository, not OS-isolated actors.

## Responsibilities

### Workflow parent
- Read the unchanged requirements, applicable root/nested work rules and current repository state.
- Investigate or commission exploration only when needed; do not spawn a child merely to fill a role.
- Create and maintain the plan, acceptance paths, useful slice boundaries and validation approach.
- Choose implementation difficulty/model with a short reason; escalate with findings/evidence when needed.
- Assign each child a concrete scope, authoritative files, expected evidence and completion conditions.
- Evaluate child outputs against requirements, actual changes, tests and independent reviews.
- Resolve findings, decide whether to proceed, and record the reason. A child's “done” is evidence, not unconditional approval.
- Inspect/stage the intended changes and run normal Git commit/push commands itself. A custom Git controller is unnecessary. A separate commit child is not mandatory.
- Keep a compact progress record, provide milestone reports and report final commits/push state or the precise blocker.

### Implementation/exploration children
- Work within the assigned slice/problem; use their own judgment for implementation details and focused checks.
- Report changed areas, verified behavior, commands/results, remaining problems and any concrete scope/escalation need.
- Do not silently change requirements, include unrelated work, commit, push or release.
- Retain the original implementation child for related fixes where its context is useful; do not launch a fresh child for every correction.

### Review children
- Be independent of the implementer and read-only by instruction.
- Inspect requirements, actual source/diff, plan and relevant validation evidence rather than only the implementer's summary.
- Normal plan review checks completeness, feasibility and verification.
- Critical plan review challenges assumptions, actual omitted usage paths and integration risks, without an objection quota.
- Report actionable findings with evidence and necessary corrections; explicitly say when no unresolved valid findings remain.
- Reuse the same reviewer for related re-review. Natural language and Markdown are sufficient; do not require an exact JSON shape.

## Parent's operating procedure
1. Establish context: requirements are settled; inspect existing work, branch/upstream and release triggers. The parent judges whether intended edits/commits/push are safely distinguishable. Do not blanket-reject every dirty worktree or require a release-safety boolean in configuration. If a real conflict/unknown destination cannot be resolved within the task's authority, report a blocker rather than guessing.
2. Plan: include required observable behavior, scope/slices, dependencies and verification. Use existing requirement IDs if useful; do not require users to add them or manufacture an inventory parser.
3. Review the same stable plan revision with independent normal and critical reviewers. They may run concurrently while no one edits that revision. Parent assesses findings against the original requirements. Repair valid issues and ask both to re-review; when declining a finding, provide evidence/rationale for recheck instead of silently ignoring it. No arbitrary round-count cap.
4. Execute slices in dependency order. Assign one modifying implementation child at a time in the shared worktree. The child implements and runs focused tests. Parent obtains independent code review, routes repairs to the owner and verifies relevant checks/re-review. Review children may run concurrently only against a stable candidate.
5. Parent judges slice completion from behavior, tests and review. Inspect the actual diff and stage the intended paths/changes, preserving pre-existing work and excluding logs/secrets/unrelated files. Make a local slice commit and record its ID. Do not infer completeness merely from local passing tests; consider whether the committed changes contain the implementation and its dependencies.
6. Validate the whole requirement set and cross-slice paths. Parent or an appropriately scoped child runs integration checks. Broader regression/live checks require an impact/usefulness rationale, not a historical test inventory. For defects, return work to the owner, recheck/review affected behavior and make correction commits. No empty final commit is needed.
7. Inspect final changes, evidence and local commits; confirm the actual safe push destination and perform one ordinary push. A push failure leaves local commits intact and is reported as incomplete delivery, not reset/retried with force.
8. Summarize achieved behavior, evidence, commits, push state and remaining limitations. Stop.

## Completion judgments (“gates”)
The parent agent is the gate. There is no separate gate model or JS classifier in this version.
- Plan readiness: required behavior is covered, the approach is feasible and no valid review findings remain.
- Slice readiness: the intended behavior is implemented, meaningful checks ran, concrete failures are resolved and independent code review is satisfied.
- Overall readiness: requirements and cross-slice behavior are verified, necessary regression is addressed and delivery contents are coherent.
- Delivery completion: the intended commits exist and the normal push actually succeeded.

These are judgment criteria in instructions, not schemas to validate mechanically. Raw command exit states, tool errors and actual Git outputs remain factual evidence. Expected negative-test output or a documented unrelated baseline failure must be interpreted in context; an unresolved required-behavior failure cannot be called success.

## JavaScript and records: deliberately small
Initially retain only:
- Startup argument/path/model settings and launching/waiting/reporting through supported NOE APIs.
- Native agent delegation/task lifecycle and optional NOE event capture.
- A workflow instruction document and ordinary role/model data.

The parent stores the plan, review conclusions, validation evidence/paths, current stage and commit/push state in a small human-readable run note at an appropriate non-delivery location. Reuse normal files/tools and NOE logs. No complete object serialization, replay engine, custom artifact store or automatic resume is required.

Potential later helpers must pass this test: a deterministic operation recurred in real usage, its input/output contract is clear, and automating it demonstrably reduces effort/errors. Examples may include report formatting or repeated command-log collection. This is not permission to prebuild them now.

## Models
All defaults use provider openai-chatgpt:
- Parent, planning and general judgment: gpt-6.1-sol / high.
- Every review: gpt-6.1-sol / xhigh.
- Routine implementation/exploration: gpt-6-luna / max.
- Difficult implementation/exploration: gpt-6.1-sol / high.

Role overrides are ordinary configuration, not a new routing service. The parent chooses difficulty and uses the existing NOE spawn/queue/wait/stop APIs. In managed parent tool calls, use steer-aware wait_tasks and yield on guidance according to NOE's documented contract. Persistent orchestration context lives in the parent agent conversation and run note, not in JS globals across fresh run_js calls.

## Portability and interruption
Keep repository-specific test/build commands and Git context in repository rules or discover them through the parent; do not require every repository to configure a generic engine. Launch from the actual repository root so child cwd and workspace instructions are correct. Copyable workflow instructions, launcher and role settings should suffice without dependencies on the NOE source checkout.

Native task failure/cancellation is not approval. User stop requests stop the parent and owned work. Existing local slice commits and notes support later diagnosis/manual continuation, not guaranteed automatic recovery or machine-loss protection. The parent reports unresolved authorization, credentials or requirement conflicts instead of inventing requirements or bypassing permissions.

## What happens to the previous implementation
Do not preserve it merely because effort has been spent. If implementation is later authorized, retain only pieces that serve this minimal design, replace old launch instructions/configuration so they do not invoke the paused engine, and remove superseded modules/tests in a clearly scoped change. Do not reset/amend the existing commit or delete unrelated work as a cleanup shortcut. None of that cleanup is performed during this design-only task.

## Validation approach after implementation is authorized
Verify the launch/parent-child wiring and one small representative real usage path: plan/review repair, implementation/testing/code review, a local slice commit, requirement-level validation and final push to a disposable local bare remote. Use focused deterministic checks only for actual launcher/helper behavior; do not recreate a simulated decision/state-machine test framework. The later OpenAI decision revision can then serve as the first substantive dogfood task after its requirements are settled. No validation is run in this design-only task.

## Design-stage handoff
This is a proposed design, not implementation completion or an approved implementation plan. Stop here. The next authorized task can define a small implementation plan from this design; there are no newly launched agents, implementation changes, tests, commits or pushes in this redesign task.
