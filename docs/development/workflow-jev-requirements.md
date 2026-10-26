# Requirements: Jev-assisted completion judgments for the agent-first workflow

## Status and purpose
Prepared for the next workflow implementation task. This request defines requirements only; do not start implementation, the workflow, live checks, commits or push merely because this file exists.

Revise the existing portable agent-first workflow so its parent uses the already-supported Jev Decisions API when assessing completion of the main work stages. Use the current first-version workflow to implement this revision once implementation is requested.

## Principles and unchanged behavior
- Maximize agent judgment. Consider mechanical helpers only for deterministic work that recurs in actual usage.
- Keep the thin JavaScript launcher and parent/child structure. The parent plans, delegates, evaluates evidence, resolves findings and performs normal Git operations.
- Retain independent normal and critical plan reviews; independent code review; implementation/test/repair loops; local commits per completed slice; overall requirement-level validation; impact-justified regression and minimal live checks; one final ordinary push; no release.
- Parent/general/planning remain openai-chatgpt / gpt-6.1-sol / high. All agent reviews remain Sol/xhigh. Routine implementation/exploration remain Luna/max; difficult work remains Sol/high, chosen with a concrete reason. Keep existing role overrides, retained related workers and one modifying child at a time.
- Jev supplements the parent's judgment. It does not replace the parent or independent reviewers, perform repository inspection/tests, commit, or push.

## R1. Use the existing NOE decision capability
Use noe:decision.decide from the workflow parent's normal run_js tool.
- Default decision provider: openrouter.
- Default decision model: typesafe/jev-1.14.
- Use existing OpenRouter credential configuration and native decision ownership/cancellation/usage/events.
- Decision selection is explicit and separate from agent role/effort settings; do not send agent effort to decide.
- Provide ordinary optional decision provider/model configuration, defaults when absent, and pass the effective settings to the parent. Existing role-only configurations and launch commands must continue working.
- No new OpenAI transport, alternate decision API, built-in runtime changes, gateway service, dedicated gate agent or silent provider/authentication fallback.

## R2. Required assessment points
The parent must obtain a Jev assessment before advancing across these boundaries. Assess readiness for the NEXT stage, not completion of work that has not happened.

| Boundary | Question to assess | Necessary evidence |
| --- | --- | --- |
| Plan drafted -> independent plan reviews | Is the plan sufficiently grounded and concrete to review? | Original requirements, relevant repository facts, proposed slices/approach and verification plan |
| Plan reviews resolved -> implementation | Does the reviewed plan cover required behavior, with no unresolved valid findings? | Current plan, both independent reviews, finding dispositions and supporting reasons |
| Slice implementation -> focused verification | Is the intended slice implemented sufficiently to run the planned focused checks? | Relevant requirements, actual current source/diff, implementer report and known unfinished work |
| Focused verification -> code review | Does verification provide sufficient evidence for review, with no unresolved failure of required behavior? | Actual commands/results, tested usage paths, current candidate and explanations of expected negative or unrelated baseline outcomes |
| Slice code review resolved -> local commit | Is this candidate ready to be committed as a completed slice? | Actual candidate diff/dependencies, focused results, independent code review, repairs and finding resolutions |
| Overall validation -> final delivery/push | Are requirements/cross-slice paths verified and the resulting commits ready for final delivery? | Requirement-level checks, relevant focused/regression/live evidence and rationale, integration repair/re-review results, intended commits and delivery context |

A single-slice task still receives overall requirement-level validation. A materially revised plan/candidate must be assessed again at the affected boundary after the relevant repairs/checks/reviews; do not reuse a decision for an obsolete target.

Do not add Jev calls merely to confirm a fact already established by Git, such as the existence of a commit or success of a push. The parent confirms these through actual Git output/state. A failed push is not completed delivery regardless of a prior assessment.

## R3. Decision questions and evidence
Use the existing choice-question capability with clearly described next actions:
- proceed: the evidence supports entering the next stage.
- revise: correct the identified work or collect missing evidence before advancing.
- blocked: there is a concrete prerequisite/authority/environment issue preventing progress.

These are choices for the decision request, not a required JSON schema for planner, implementer or reviewer reports.

The parent constructs each stage-appropriate question and evidence using judgment. Include the relevant original requirements, actual target/source/diff, review findings/resolutions and command outcomes. Merely passing file paths or a child's “done” claim is insufficient because Jev cannot inspect local files or execute tests itself. State any material scope reduction or omitted evidence rather than presenting a misleading complete picture. Do not send credentials or unrelated private material.

Keep agent reports natural-language/Markdown as appropriate. Do not introduce an exact plan/review/result protocol, mandatory requirement numbering, fixed command registry or repository ownership-amendment schema.

## R4. Parent responsibility and handling outcomes
- Jev must actually be called at the designated boundaries; a statement that it “would approve” is not an assessment.
- The parent reads the returned choice and available probabilities/confidence, compares them with the source evidence and independently decides what to do.
- proceed is not permission to ignore valid review findings, omit required checks, claim unexecuted tests passed or treat failed delivery as success.
- revise normally returns work to its owner or gathers the missing evidence. Do not blindly modify code when the real issue is incomplete/incorrect evidence.
- blocked requires the parent to investigate the specific prerequisite and either resolve it within authority or report the genuine blocker.
- If the parent disagrees with Jev, recheck the evidence/question and explain the concrete discrepancy. Reassess when the supplied evidence/question was materially wrong. The parent may overrule an incorrect substantive judgment with an explicit evidence-based reason in the run note; do not quietly discard inconvenient results.
- Refusal, an unusable/missing answer, or an API/credential/transport error is not approval. The parent may make a justified retry/correction or report inability to obtain the required assessment. Do not silently bypass the required call, fabricate a choice or switch provider.
- Do not invent a universal probability/confidence pass threshold, arbitrary review-round cap, repeated polling or repeated identical calls just to seek a preferred answer.

The native decision result can be structured; it does not require a JS state machine. The parent agent remains the workflow gate and owns the reasoning.

## R5. Records, portability and user reporting
Use existing native NOE decision events and the parent's small human-readable run note. For each boundary record:
- Stage/slice and the candidate/evidence being assessed.
- The request/evidence itself or a usable record reference.
- Actual provider/model, returned choice and available probability/confidence information.
- Parent conclusion, next action and any disagreement/override reason.

Also retain ordinary review/test evidence, slice/correction commit IDs and final push state. Do not build a separate artifact store, report schema validator, replay system or automatic resume engine. Preserve unrelated work and keep non-delivery logs/credentials out of commits.

Keep the package copyable to other repositories. Update workflow instructions, launcher/settings/example and installed requester guidance/README as needed so the actual behavior is consistent. Explain that ordinary workflow execution now makes OpenRouter decision calls in addition to ChatGPT agent calls, and requires configured OpenRouter access. Help must remain offline.

## R6. Scope and exclusions
Included: a small revision to the existing workflow's parent instructions and necessary launch/configuration/documentation/checks.
Excluded:
- OpenAI Decisions integration or unification work, and any new probes/benchmarks comparing providers.
- Rewriting the existing noe:decision API or Jev transport merely to serve this workflow.
- Reintroducing the rejected mechanical Git controller, workflow engine, strict agent schemas or comprehensive simulated decision framework.
- Broad Go/runtime refactors, releases, forced pushes, history rewriting or automatic recovery.
- Mandatory additional reviewer/test/commit agents for every stage.

Existing OpenAI research/probes are background information, not implementation tasks or automatically included delivery files.

## Acceptance and proportionate validation
1. An ordinary settled-requirements request launches the parent with effective Jev defaults or configured overrides while preserving current agent role settings.
2. The parent procedure identifies and uses every assessment boundary above without replacing independent reviews or actual test execution.
3. Real decision output is recorded and influences an explained parent action; substantive disagreement and unavailable/refused assessment handling are explicit rather than hidden.
4. The normal per-slice local-commit and overall-validation/final-push behavior remains intact, with no release.
5. Use focused offline checks for actual changed launcher/configuration/prompt behavior and applicable lint/type/diff checks. Do not recreate a generic parent-decision/Git simulator.
6. Run one small representative live task against a disposable Git repository/local bare remote, confirming native Jev calls at the relevant boundaries, independent agent work/reviews, evidence recording, commit and final push. Keep calls proportionate; do not repeat runs to force stochastic findings or every possible model answer. Investigate additional branches only when a concrete failure would change the completion decision.
7. No full Go suite is required solely for this instruction/launcher revision; any necessary broader checks follow actual changed paths.

## Applying the revision using version 1
The current first-version parent uses its launch-time workflow instructions to implement this file. Do not require it to retroactively obey the newly introduced Jev boundaries or restart itself mid-task. Validate the revised workflow on the separate tiny representative task once a candidate exists; do not recursively run the workflow to modify itself indefinitely.

## References
- Current procedure: .noe/lib/workflow/workflow.md
- Current launcher/settings: .noe/lib/workflow/workflow.js and config.example.json
- Existing decision contract: docs/decision.md
- Existing Jev usage example: examples/agent/task-loop-jev/workflow.js
- Agent-first design: docs/development/workflow-agent-first-design.md
