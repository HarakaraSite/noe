# Development, validation, and completion criteria

This policy applies to parent and delegated agents developing, investigating, testing, or reviewing this repository. Follow the user's explicit validation scope and requests for repeated experiments, load tests, or similar work.

## Define scope from product behavior

- Success means the user can complete the required behavior through actual usage paths. Test counts, review counts, and mechanical gate passes do not replace that criterion.
- Start a plan by defining required behavior, its basis in explicit requirements, execution evidence, or verified external contracts, the paths affected by the change, and completion criteria. Do not build the work around a test inventory or hypothetical failures.
- Tie each test to concrete product behavior or a confirmed regression. Do not add a test whose purpose and evidence you cannot explain. Test counts are neither a goal nor a cap.
- Uncertainty alone does not justify adding feature restrictions, safeguards, input limits, fallbacks, or cleanup. Limit these decisions to explicit requirements or concrete failure paths and user impact in the current environment.

## Narrow broad investigations in stages

For broad investigations, narrow the scope in stages: inspect structure, counts, or search results, then read the relevant details. Select returned information according to the next decision it must support. A summary should reduce the amount of information, not merely remove fields while retaining every item. Preserve material exceptions and supporting evidence. Follow up on omitted information when it could change the conclusion.

Save useful information to a known file when needed for later work. Context-budget notices alone do not require archival work.

## When to repeat validation

- During implementation, use tests for the changed behavior and the necessary type, formatting, lint, and diff checks. Do not repeatedly use the full suite for intermediate checks.
- Do not require the full suite before review. Reviewers focus on the target diff and related checks; the parent coordinates full validation.
- When full validation is needed, default to one run by the parent on a candidate whose implementation and review fixes have settled. If related changes or unresolved issues remain, choose the scope of further validation from their impact. A change to one area does not automatically require rerunning every check.
- On failure, first identify how the changed code could reach that failure. Use focused checks to distinguish a regression from an unrelated issue. Record unrelated failures separately with evidence; do not add them to this task's completion criteria.
- A retry, more repetitions, or broader validation needs a concrete question and an explanation of how the result could change the completion decision. Do not run many repetitions solely because one pass feels insufficient. Stop when the necessary evidence is available.

## Review and delegation

- A normal review checks explicit requirements, actual usage paths, verified external contracts, bugs or regressions introduced by the change, and concrete validation gaps.
- When reporting a testing gap, identify the changed product behavior that remains unverified and the execution path through which it affects users. Missing tests, unobserved variants, hypothetical failures, or an incomplete coverage table are insufficient on their own.
- Delegated tasks must include scope, exclusions, completion criteria, and time and effort limits appropriate to the work. Children may return unresolved issues; do not chain unlimited reviews or repetitions. The parent decides whether further investigation is needed and when to stop.

## Completion reports

- Focus on the outcome, evidence that the objective was achieved, and remaining issues needed for practical usage decisions. Follow the requested format and detail; otherwise keep reports concise. Distinguish bugs in this change, existing separate issues, and untested areas; do not give hypothetical concerns the same weight.
- Finish when the required behavior and validation are established. Do not make unrelated investigation a completion requirement. Report any unresolved related issue with its concrete impact.
