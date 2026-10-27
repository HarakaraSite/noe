# Requirements: five small NOE core improvements

## Status and objective

The user approved all five candidates and now requests implementation through
the standalone development workflow, with all five requirements passed together
in this file and each requirement implemented as its own slice. Requirements
are settled. Release work remains outside scope.

Reduce confirmed redundant processing and unused internal state in NOE core
without adding features or changing public behavior. These are cleanup and
efficiency improvements, not confirmed correctness bugs. Performance impact
has not yet been measured.

The five items are required scope. The implementation may choose the smallest
suitable internal structure; it must not turn them into a broader redesign.
If measurements do not support a proposed optimization, report the evidence
and unresolved item rather than silently dropping it or claiming improvement.

## Required implementation slices

Run one standalone workflow for the complete requirements file, not five
separate workflow runs. Use exactly these five implementation slices, in order:

| Slice | Requirement | Scope |
| --- | --- | --- |
| 1 | R1 | Command-environment normalization and non-Windows sorting |
| 2 | R2 | Saved-log line copying and maximum-sequence traversal |
| 3 | R3 | Thinking-fragment text accumulation |
| 4 | R4 | Cancellation queue-search shortcut |
| 5 | R5 | Unused module-loader entry state |

Each requirement is one slice. In particular, the two R2 optimizations belong
to slice 2, not to separate implementation slices. Slice 3 builds on the
saved-log analysis candidate from slice 2.

The workflow parent owns the overall plan, readiness assessments, independent
reviews, validation, and Git operations. Complete each slice through the
workflow's verification/review process and local slice commit before proceeding
to the next. After all five slices, perform coordinated overall validation and
the workflow's final delivery/push decision. Do not launch another workflow per
slice or combine all five into one implementation slice.

## Common constraints

- Preserve public Go and JavaScript APIs, CLI behavior, error handling, result
  shapes, and existing JSONL compatibility.
- Preserve resource ownership, cancellation, task acceptance/publication
  ordering, retained results, and instance lifecycle behavior.
- Keep changes local to the identified paths and directly related tests or
  small benchmarks. Preserve pre-existing and unrelated worktree changes,
  including workflow/model configuration changes.
- Do not add runtime dependencies, configuration fields, user-facing options,
  automatic limits, background services, or new features.
- Do not update dependencies or versions, perform release work, or change
  repository-wide formatting.
- Source references below identify the inspected implementation. Confirm the
  actual source and applicable work rules before implementing.

## R1. Remove redundant command-environment processing

### Basis and affected paths

[commandenv.go](../../internal/commandenv/commandenv.go):
`Shell` selects and normalizes an environment, then `prepare` normalizes it
again. `normalizeEnv` also sorts keys before copying them on non-Windows
systems, where that ordering is not needed.

This code is used by Node-style child-process execution and `noe:process`;
the change must preserve both usage paths.

### Required change

- Normalize the selected shell environment once per preparation path rather
  than repeating the same normalization/copy.
- Remove sorting from the non-Windows environment-normalization path.
- Keep deterministic sorting wherever it is part of the existing behavior,
  including final environment-block generation and Windows collision handling.

### Behavior to preserve and acceptance criteria

- A nil override inherits the configured environment; a non-nil override
  replaces it, and an explicitly empty override remains empty.
- Prepared environments remain independent of caller-owned maps; neither
  defaults nor override maps may be mutated or shared as writable output.
- Preserve Windows case-insensitive key handling and the existing deterministic
  winner when differently cased keys collide.
- Preserve shell selection, shell arguments, policy callback arguments and
  invocation count, authorization before directory/executable resolution,
  working-directory resolution, and selected PATH/PATHEXT behavior.
- Never introduce fallback to the host environment or host PATH.

## R2. Remove redundant copies and traversal in saved-log analysis

### Basis and affected paths

- [read.go](../../internal/loganalysis/read.go): `readCompleteLine` copies
  each complete line even when the first buffered read already contains it.
- [analyze.go](../../internal/loganalysis/analyze.go): maximum sequence
  calculation has a full traversal separate from state reconstruction.

The affected usage paths include saved-log analysis through `noe:agent-log`
and CLI observation of saved snapshots.

### Required change

- Parse a complete line directly from the buffered reader's slice when it
  fits in the first read; retain accumulation for buffer-spanning lines.
- Keep borrowed bytes valid until parsing finishes. Retained records must not
  refer to a reader buffer that a subsequent read may overwrite.
- Compute the maximum sequence during the existing reconstruction traversal,
  removing the separate full traversal for that purpose.

### Behavior to preserve and acceptance criteria

- Preserve complete-line parsing, CRLF handling, malformed-record errors with
  line numbers, and the rule that an unterminated tail is not parsed.
- Preserve cancellation checks and reader-error propagation, including while
  accumulating a long line.
- Preserve target-execution selection, ambiguous-run rejection, cursor
  validation, unknown-run results, and state reconstruction from the full
  supplied snapshot.
- The returned cursor remains the maximum sequence for the selected
  **execution**, including records from other runs, not just the selected run
  or projected events.
- Preserve event order, event filtering, output/thinking options, and empty
  event-array representation. Do not replace snapshot reconstruction with a
  new incremental parser or persistent observation state.

## R3. Avoid repeated prefix copying when joining thinking fragments

### Basis and affected paths

[event.go](../../internal/loganalysis/event.go): `appendProjected`
repeatedly concatenates an accumulated string with the next fragment.
Many consecutive fragments repeatedly copy the accumulated prefix.

This affects projected saved-log events when `IncludeThinking` is enabled;
it is not a request to change ordinary model streaming or CLI rendering.

### Required change

- Accumulate each eligible consecutive thinking group without repeatedly
  copying the entire accumulated prefix, using a small local mechanism such
  as `strings.Builder`.
- Materialize the final text at the group boundary or end of processing.
- Do not introduce shared mutable accumulation state between analyses.

### Behavior to preserve and acceptance criteria

- Produce the same combined text and projected event metadata as before.
- Preserve the first event's existing metadata and the current grouping rules:
  matching thinking type and request ID, with consecutive event sequences.
- Do not merge across sequence gaps, different types, different request IDs,
  intervening events, or a query's cursor boundary.
- Preserve separate treatment of `thinking` and `thinking_summary`, the
  disabled-thinking path, and empty fragments.
- Use a small, fixed fragment-heavy before/after benchmark to verify that the
  repeated-prefix-copying problem is actually reduced. Report time and
  allocation results without claiming an end-to-end speedup from that alone.
  Do not introduce arbitrary performance thresholds or prolonged load tests.

## R4. Skip queue searches when cancelling a current command

### Basis and affected paths

[task_lifecycle.go](../../internal/agent/task_lifecycle.go):
`acceptTaskCancellationLocked` identifies selected/active commands but still
scans the pending queue. Selection already removes a command from that queue.

### Required change

- Search the queue only when the command is neither selected nor active.
- Keep the change local; do not redesign command states, queue representation,
  locking, or cancellation publication.

### Behavior to preserve and acceptance criteria

- Selected and running commands use the existing current-task cancellation
  path; queued commands are removed and settled using the existing queue path.
- Preserve errors/rejections for finished or non-cancellable commands and
  commands whose final answer has already been committed.
- Preserve idempotent retries, ownership/admission checks, queue ordering,
  group accounting, and retained cancellation outcomes.
- Preserve acceptance-event ordering, cancellation notification, publication
  gates, and shutdown joins. Do not add observer callbacks or waits while
  holding `Host.mu`.
- The selected/active path no longer performs a linear search of pending
  commands, including when other commands remain queued.

## R5. Remove unused module-loader entry state

### Basis and affected paths

[loader.go](../../internal/modulemap/loader.go): `Loader.entry` is assigned
but not read; in-memory entry source is served from `sources`.

### Required change

Remove the unused `entry` field and its assignment. Do not remove the
meaningful entry ID, presence flag, error state, or in-memory source-cache entry.

### Behavior to preserve and acceptance criteria

- In-memory entry source still takes precedence over an on-disk file for the
  same canonical module ID, including an empty source string.
- Preserve `EntryID`, canonicalization, nil/no-entry behavior, entry-validation
  errors, and runtime-local source/alias caching.
- This is a state-clarity cleanup, not a claim that a second copy of the source
  bytes was previously allocated or that removing the field yields a measured
  performance improvement.

## Validation and completion

### Focused evidence

Use existing tests where they establish the required behavior; add focused
coverage only for a changed behavior or a concrete unverified contract.

- R1: command-environment tests for direct and shell preparation, override/map
  isolation, policy order, and Windows case/PATH/PATHEXT semantics.
- R2: short and buffer-spanning log lines, malformed/partial/CRLF input,
  cancellation, and cursor/state results for interleaved runs and executions.
- R3: fragment grouping and cursor-boundary cases, identical projected output,
  and the bounded before/after benchmark described above.
- R4: selected, running, queued, retried and finished cancellation, committed
  final answers, and the existing acceptance/publication ordering.
- R5: existing module-loader entry/source-cache tests; include empty in-memory
  source if the applicable behavior is not already covered.

Run the affected package tests and directly related callers' tests. Relevant
starting points are `internal/commandenv`, `internal/loganalysis`,
`internal/observation`, `internal/cli`, `internal/agent`, and
`internal/modulemap`; choose exact commands from the actual diff. Use targeted
race checks for changed cancellation paths where applicable. Run Go formatting
and diff checks.

Coordinate broader validation once on the settled candidate if needed.
Unrelated failures must be identified and reported separately, not silently
fixed in this task or made grounds for unlimited reruns. No live provider
requests, paid model comparisons, long stress runs, or release builds are
required for these five changes.

### Completion report

Completion requires all five scoped changes, preserved behavior established by
focused evidence, and no unresolved related correctness findings. Report:

1. Changed areas and how each requirement was satisfied.
2. Actual checks and outcomes, distinguishing unrelated existing failures.
3. R3 benchmark results and any limits on performance conclusions.
4. Any unresolved requirement or related issue with concrete impact.

Do not use test counts, code-size reduction, or unmeasured speed claims as a
substitute for these criteria.
