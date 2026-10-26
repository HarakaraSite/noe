# Agent-first workflow implementation

## Basis
Implement workflow-agent-first-design.md, authorized by the user after the design-only stop. Preserve the behavioral stages and model choices, not the old machine-gated engine. No Decision API or release.

## One implementation slice
This is one small vertical slice because launcher, parent instructions and launch guidance must agree.
- Replace .noe/lib/workflow with a thin CLI/launch module, workflow-parent instructions, role defaults/config example, README, outer-agent instruction template and a focused test.
- Remove the superseded protocol/runtime/Git/state-machine modules and their simulation suites; keep Git history intact.
- Launcher reads requirements/settings/instructions, starts one Sol/high parent with explicit delegation permission, awaits native NOE completion and surfaces the answer/native failure. It never parses plan/review decisions or owns Git state.
- Parent follows the fixed procedure and judges evidence; it creates independent Sol/xhigh reviewers, routes routine implementation/exploration to Luna/max or difficult work to Sol/high, retains related workers, commits each finished slice and pushes once at the end.
- No mandatory role-filling children, exact result schemas, requirement numbering, per-repository command registry, clean-worktree blanket rule, custom artifacts or resume engine.
- Update the example, installed outer-agent guidance and existing repository config so they cannot invoke the paused engine.

## Ownership
Implementation owner modifies .noe/lib/workflow/** and examples/agent/development-workflow.js only. Parent updates installed instructions/configuration, index links and historical-doc supersession notices, reviews final diff, commits/pushes only task changes. Do not touch unrelated event logs or __pycache__.

## Checks and completion
Owner runs launcher-focused offline checks (argument/default/config propagation, delegation permission, native failed/cancelled outcomes, no provider call for help, launch from another cwd) plus lint/types. An independent code reviewer checks actual code and parent instructions. Parent then runs the settled focused suite once and one small live-provider fixture through the real launcher, using a temporary Git repository and local bare remote: independent reviews, child implementation/tests, local slice commit, requirement-level check and final local push. No broad Go suite or synthetic Git controller test framework. Complete when that usage path works, docs match, and intended changes are committed and normally pushed to this repository's existing safe upstream. Current release CI is tag/manual-only; recheck before push.

## Status
Implemented the small replacement. Independent normal and critical Sol/xhigh plan reviews and the independent code review all returned no findings.

The portable package now contains a thin native-agent launcher, workflow-parent instructions, role settings/example, README/outer-agent template and a launcher-focused test. The superseded protocol/runtime/Git/state-machine modules and their simulation suites were removed rather than adapted. Installed launch guidance and model-only settings now use the new parent-led workflow.

Final focused checks passed: offline launcher tests, Biome lint, NOE-declaration TypeScript checking and git diff --check. No Go/runtime changes or broad Go suite were needed.

One live usage fixture was executed from another repository through the actual portable launcher, using default OpenAI ChatGPT roles and a disposable local bare remote. Native events show one Sol parent, independent normal/critical Sol reviewers, a Luna implementation child and an independent Sol code reviewer. The parent made one source-only slice commit (7c7400d3b04651089593b745ab246e0d861d098f), performed final requirement checks and one normal final push. The bare remote tip matched the commit. A fresh clone of the delivered branch passed the actual greeting test; only greeting.js changed. No Decision API or release was used.

Fixture/evidence directory: /tmp/noe-agent-first-live-1793017628759 (events.jsonl, stdout.txt, stderr.txt, repo, remote.git and delivered clone). This was a single small success-path usage check, not a claim of exhaustive validation of large multi-slice projects or interrupted-run recovery. Native failure/cancellation propagation is checked offline.

Repository delivery: pending the final scoped commit and ordinary upstream push; actual commit/push outcome is reported in the task completion response.
