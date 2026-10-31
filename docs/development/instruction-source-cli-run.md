# Instruction-directory CLI implementation run

## Authority and baseline

- Requirements: [unchanged task input](instruction-source-cli-requirements.md).
  Re-read against the launcher's supplied text before planning; no differences.
  SHA-256: `e03dc8a913fb3c82eb0674dd4537bd26dd6442434179bc27ed29d79f3fd6f806`.
- Root `AGENTS.md` applies; no nested work rules in affected paths.
- Baseline: `main` at `ecdc038`, tracking `origin/main` at `ce6122c`.
  No tracked/index edits. Four pre-existing commits, in order:
  `7993a68`, `c8d2689`, `c8a11c7`, `ecdc038`; preserve unchanged.
- Untracked requirements are task input. The two untracked discussion memos
  are unrelated user work and must remain unmodified and unstaged.
- Ordinary destination: `https://forge.harakara.site/littleisland/noe`,
  `refs/heads/main`. No push override configured. Only checked-in workflow is
  `.forgejo/workflows/release.yml`: version-tag push or manual dispatch.
  No release/tag/version/dependency/install work is authorized.

## Stable plan — revision 1

### Required behavior and source paths

Both `noe run` and `noe eval` must accept a repeatable ordered replacement
selection via `--instruction-dir PATH` / `--instruction-dir=PATH`, or an
explicit empty selection via `--no-instructions`. Neither means unchanged
common-plus-starting-cwd discovery. Explicit selection must exclude any
unlisted default source; repetitions and directory ordering are significant.
Selection changes only external instruction locations, not execution cwd,
application identity, explicit system text, built-in facts or custom Go policy.

Existing parser `internal/cli/execution_options.go` stops at the first
operand or `--`; preserve that boundary and script args. Reject missing/empty
directory values, conflicting options in either order, and repeated
`--no-instructions` with usage exit 2 before executing source/model requests.
Follow the existing separate-value option convention for a following `--...`
token; equals form can express such a literal path.

Carry an unspecified-versus-explicit-empty value through CLI command options,
`runtime.Options`, and `internal/runtime/managed.go` to the existing
`HostConfig.Instructions` seam. A nil-versus-non-nil slice is sufficient if
no copy/default operation collapses it. The host fixes selected relative
locations against its actual starting cwd, not entry location/Git ancestors;
copy caller-owned lists and do not sort/deduplicate or expand env/tilde.
Retain the existing no-option discovery behavior, including common fallback.

Reuse `internal/noeprofile/instructions.go`'s reader and existing per-instance
first-task composition. Resolve/fix locations without reading directory/file
contents eagerly. Selected sources must remain lazy, including ordinary JS
and idle agents. Queued tasks freeze text/errors per instance; fresh children
and instances load current contents at their own first actual task using the
host's selected locations, without implicitly inheriting parent system text.
Do not change public agent config or custom Go-host discovery/policy.

### Slice and ownership

One cohesive feature slice: CLI parser/help + managed/source plumbing +
focused tests + current `docs/instructions.md`. CLI and host wiring have no
useful independently deliverable intermediate behavior. One modifying
implementation worker (difficult role, Terra/high: nil/empty and lazy snapshot
integration cross multiple layers), retained for related repairs. No worker
Git operations, further delegation, live paid providers or unrelated edits.
Parent owns this note, task-record input, assessment, commits and push.
Read-only independent plan reviewers (normal and critical, Sol/xhigh) inspect
this same revision before implementation; valid revisions go back to both.
Independent code reviewer (Sol/xhigh) checks the settled candidate and evidence.

### Focused verification and acceptance

Use existing deterministic local HTTP/provider patterns in
`internal/cli/agent_test.go` for real `cli.Run` run/eval entry paths; no paid
feature-provider requests. Inspect actual provider system messages, not only
parser results:
- Default run/eval reads common then starting-cwd workspace.
- Explicit run/eval reads only the ordered selected list (both value forms,
  repeated directories, same filenames concatenated), with facts first and
  explicit system last; explicit empty excludes both defaults.
- Relative selection is based on actual cwd when the entry file lives
  elsewhere; no change to process.cwd/filesystem behavior. Missing sources add
  nothing, selected non-directory/broken file errors stay path-bearing and lazy.
- Test option forms, mixed ordering with existing options, usage failures,
  terminator, and unconsumed script args after the first operand.
- Exercise selected-source snapshot lifetime through existing runtime fixture
  patterns: queues retain first text, fresh children/instances reload selected
  contents, parent explicit text is not implicitly inherited. Preserve lazy
  ordinary-JS/idle startup and initialization failure behavior.
Reuse existing source-reader coverage for filename order, lowercase .md,
symlink-to-regular-file/no recursion, missing directories, cancellation and
unreadable sources; extend only concrete selection gaps.

Worker runs focused new/related tests continuously with repairs, gofmt on
changed Go, and `git diff --check`; saved JS, if any, gets Biome lint and
relevant type checks (embedded JS fixtures are exercised through Go tests).
Candidate review follows parent inspection of actual code/results; no redundant
full suite before review. Parent chooses one overall `go test ./...` after
review fixes settle, justified by internal runtime option/host-construction
changes shared by CLI and Go-facing integration. Run affected-package
`go vet` and diff/format checks; no unrelated stress/release checklist.
Investigate failures only far enough to distinguish related regressions from
baseline issues, then scope any additional verification by actual impact.

Document help and current instruction guide: defaults, replacement/empty
semantics, mutual exclusion, option boundaries, run/eval examples, application-
owned absolute/relative locations without a registry, cwd and lazy lifetime.
Do not modify separated instruction Markdown files or discussion memos.

### Completion and delivery

Parent inspects evidence and final diff, resolves all related review findings,
and commits only intended paths plus unchanged requirements/task record.
Record slice hash below; no per-slice push. Recheck ancestry, remote state,
push configuration and workflow triggers after integration validation. One
ordinary explicit `git push origin main:main` if safe; no force/tags/rewrite.
That push also delivers the four pre-existing local commits, not authored by
this feature. Report exact syntax, evidence, commits, actual push state and
concrete limitations. A failed/unsafe push leaves delivery incomplete.

## Findings, evidence and delivery

Plan revision 1 received independent normal and critical Sol/xhigh reviews.
Both inspected the full requirements and relevant source and found no concrete
findings. Parent concurs based on the existing parser boundary and host
first-task composition seam. No plan revision was necessary. Implementation is
authorized as the single cohesive slice above, with continuous focused checks
and repairs by the retained difficult-role Terra/high worker.


### Candidate inspection and focused evidence

Parent inspected the tracked diff plus new CLI/source test files and the
focused command output. Nil/empty is preserved by the parser and runtime
options; selected sources are copied/resolved before lazy content reads;
the default branch and common fallback retain their old code. Host composition
and custom Go hosts are unchanged. Local HTTP tests observe exact system
composition for default/replacement/empty run and eval, with entry elsewhere
and process cwd/filesystem assertions. Existing runtime lifetime fixtures now
exercise selected locations for child reload and queue/error snapshots.

Worker's focused command passed (three packages; output at
`/tmp/noe-instruction-focused.log`):

```sh
go test ./internal/cli ./internal/runtime ./internal/noeprofile \
  -run 'Test(ParseExecutionOptions|RunExecutionOptionsPreserveOperands|RunExecutionOptionUsageErrors|RunEvalTerminatorAndLegacyOperands|ParseInstructionOptions|CLIInstruction|CLISelectedInstructions|CLIAgentRunAndEval|AgentInstructions|AgentBehaviorInstructions|InstructionSource|SelectedInstructionSource)' \
  -count=1
```

Changed Go files were gofmt-clean; `git diff --check` passed. No standalone
JavaScript was created (embedded fixture JS executes in the Go tests).
Independent code review is next; no full-suite rerun has occurred.


### Independent candidate review disposition

Independent Sol/xhigh review inspected the full requirements, candidate diff,
new test files and focused output and found no concrete findings or related
validation gaps. Parent agrees after direct inspection: the feature is bounded
to launch selection and reuses the unchanged reader/host snapshot mechanism.
The slice is ready for its local commit. This run note stays local/untracked
so it can record the actual final push outcome without a post-push source edit.
The unchanged requirements file will be committed as the task record.


### Slice commit and overall validation

Feature slice committed as `d20d883` (`feat(cli): select external instruction
directories for run and eval`). Staged only the 10 intended paths including
unchanged requirements; both user memos retained their original SHA-256 values.

One overall `go test ./... -count=1` ran on this candidate. Every package
except `internal/cli` passed. The sole failure was
`TestRemovedREPLHelpAndUnknownCommand`: its broad lowercase substring search
for `repl` matches the newly required English help word `Replace`.
The CLI has not restored a REPL command; this is a concrete test-assumption
conflict reached by the new help. Full output:
`/tmp/noe-instruction-overall.log`.
`go vet ./internal/cli ./internal/runtime ./internal/noeprofile` passed
(output file `/tmp/noe-instruction-vet.log` is empty).

Original implementation worker will repair the stale assertion to check
actual CLI command/usage behavior rather than arbitrary English substrings,
then run the entire affected CLI package. No full-suite repetition is needed
for a test-only correction; previously passing other-package results remain
valid. Original independent reviewer will inspect the correction.


### Integration correction and completion judgment

Test-only correction `63aa502` (`test(cli): check removed REPL command without
matching help prose`) narrows the stale assertion to actual command fields and
one unknown-command rejection. Full `go test ./internal/cli -count=1` passed
(`/tmp/noe-instruction-cli-repair.log`); gofmt and diff check passed.
Original independent Sol/xhigh reviewer re-read the correction and logs and
found no remaining findings. Parent concurs: no product code changed after
the overall run, and all packages now have valid passing evidence from the
overall run plus the affected-package repair run. No repeated full suite is
needed. No related implementation/review/validation issue remains.

Final delivery checks and one ordinary push are next. This execution adds only
`d20d883` and `63aa502`; it preserves the four earlier local commits.


### Final push state

Final checks: tracked worktree/index clean, changed Go files gofmt-clean,
`git diff --check ecdc038..HEAD` passed, requirements and both discussion memo
hashes unchanged. No active custom Git hooks, push overrides, or Forgejo
workflow changes. Remote main was confirmed at `ce6122c`; release workflow
remains version-tag/manual-dispatch only.

One ordinary `git push origin main:main` succeeded:
`ce6122c..63aa502 main -> main` at
`https://forge.harakara.site/littleisland/noe`.
It delivered the four preserved commits `7993a68`, `c8d2689`, `c8a11c7`,
`ecdc038`, plus new feature/correction commits `d20d883` and `63aa502`.
No force push, tag, version bump, release or installation occurred.
Both user memos and this local run note remain untracked and were not pushed.
No unresolved related findings remain. Verification used local deterministic
providers only; race/extra build-tag/live-provider/release checks were outside
the necessary scope and were not run.
