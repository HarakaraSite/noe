# Requirements: CLI selection of instruction directories

## Authority and objective

The user selected CLI startup options as the instruction-source selection
entry point and now requests implementation through the standalone workflow.
Implement this bounded feature in the NOE repository.

Let callers select the external instruction directories for one CLI
execution, independently of application identity and the actual working
directory. Keep existing instruction-file organization and per-instance
snapshots. Do not build noebot or a generic instruction-management framework.

The two discussion memos are background, not additional implementation scope:
[instruction discussion](instruction-source-selection-discussion.md) and
[noebot concept](noebot-concept-discussion.md).
The current [instruction contract](../instructions.md) describes the baseline.

## Required CLI behavior

Support instruction selection for both `noe run` and `noe eval`.

1. With neither new option present, preserve existing automatic common plus
   starting-cwd workspace instruction selection exactly.
2. Add repeatable `--instruction-dir PATH` and `--instruction-dir=PATH`.
   The supplied directories form an ordered replacement list, not additions
   to automatic defaults. Common and workspace are included only if listed.
3. Add `--no-instructions` to select an explicitly empty external directory
   list. It does not disable built-in runtime/API facts, explicit
   `config.system`, or a custom Go host's fixed policy.
4. The directory option and `--no-instructions` are mutually exclusive,
   regardless of argument order. Reject missing/empty directory values and
   repeated `--no-instructions` as CLI usage errors, before agent requests.
   Repetition of the directory option is intentional, including the caller's
   chosen directory order; do not silently sort or deduplicate that list.
5. Follow the existing execution-option boundary: options before the first
   operand, the existing `--` terminator, and script arguments afterward.
   Do not consume script arguments that happen to use these option names.
6. Resolve relative directories against the actual starting cwd, not the
   entry-script location or a discovered Git root. Keep the selected
   locations stable for that execution. Do not add ancestor discovery,
   environment-variable interpolation or NOE-specific tilde expansion.
7. Describe the options and their replacement/empty/default semantics in
   CLI help and the current instruction guide, with run/eval and
   application-owned directory examples. No app registry is involved.

## Instruction-loading contract to preserve

Reuse the existing reader and HostConfig.Instructions composition seam.
Plumb the launch selection to the managed host; do not introduce per-agent
source overrides or a new inheritance framework.

- Read immediate lowercase .md files in each selected directory in filename
  order, with existing symlink-to-regular-file behavior and no recursion.
- Concatenate contents. Matching filenames do not override earlier files.
- Missing directories add nothing. Preserve errors for unreadable sources.
- Preserve lazy instruction content loading at each instance's first actual
  task. Ordinary JavaScript that never starts an agent must not acquire new
  instruction-file read failures merely because it selected a directory.
- Each instance keeps its first-task snapshot across queued tasks.
- New children use the host's selected locations, reading current contents
  for their own first task. They do not inherit parent-loaded text or
  parent config.system unless explicitly supplied.
- Preserve built-in facts, explicit system composition and actual
  execution cwd, filesystem, command and network behavior.
- Custom Go hosts retain control of their own instruction source; do not
  silently add CLI discovery there.
- Preserve cancellation and initialization error propagation without
  adding file watchers or background work.

The unspecified versus explicitly empty distinction must survive every
configuration/plumbing boundary. Do not implement an empty list as default
discovery.

## Scope and exclusions

Keep changes to CLI parsing/help, necessary managed-runtime/source-selection
plumbing, related tests, and current usage documentation.

Do not merge or rewrite the deliberately separated Markdown instruction
files. Do not extend provider-oriented config.json, add a new centralized
config file, or introduce an environment-variable selection mechanism.
Do not add live reload, append-only instruction mutation, automatic
propagation to existing children, new memory storage, noebot, scheduling,
provider changes, dependencies, version bumps, tags, releases or installation.

Normal/critical plan reviews, continuous implementation plus focused-test
loops, independent code review, local slice commits, proportionate overall
validation and safe final delivery remain owned by the workflow parent.
Choose useful slices in the plan; there is no fixed slice count for this
single feature. No Jev/decision gates or test-authorization pauses.

## Evidence and acceptance

Verify actual user paths with deterministic local fixtures, not only parser
unit tests:

- Ordinary no-option run/eval retains common/workspace behavior.
- An ordered explicit list includes only selected directories, including
  common/workspace exclusion; verify composition order.
- Explicit-empty selection excludes both discovered directories while
  retaining runtime facts and explicit system text.
- Relative paths use starting cwd even when the entry script is elsewhere.
- Queued tasks retain the snapshot; fresh child/new instances read the
  selected source under existing lifetime rules.
- Loading semantics and failures remain lazy and compatible.
- CLI errors, option order/forms, terminator and script-argument boundaries
  follow the declared contract.

Reuse existing coverage where sufficient; add focused checks for changed
behavior and confirmed regressions. Use a local provider/fixture for actual
agent composition, with no paid feature-provider requests. Run necessary
formatting/type/lint checks for saved code and a diff check. Coordinate one
settled-candidate overall validation if justified, rather than repeated
full suites, unrelated stress tests or a historical release checklist.
The workflow's own agent calls are permitted.

Complete when the CLI paths work, documented contracts match actual
behavior, focused evidence and independent review have no unresolved
related findings, and delivery state is honestly reported. Report exact
new syntax/examples, checks, commits, push state and limitations.

## Git and input preservation

At handoff, main has four pre-existing local commits ahead of origin/main
(7993a68, c8d2689, c8a11c7, ecdc038), previously requested by the user.
Preserve them; do not rewrite, reset or misattribute them to this feature.
The parent must inspect actual ancestry/destination and report what a
normal final push delivers.

The two untracked discussion memos are pre-existing user work: preserve
their contents and do not stage them accidentally. This requirements file
is task-owned input; keep its contents unchanged. It may be committed as
the task record if appropriate. Preserve unrelated work and local logs.
