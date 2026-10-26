# Agent-first development workflow

A deliberately thin, portable launcher starts one native NOE workflow parent for settled requirements. The parent plans, delegates work/reviews, judges evidence, commits completed slices and decides whether a final push is safe. There is no mechanical workflow engine, decision classifier, Git controller, custom artifact store, or automatic resume.

## Copy and run

Copy the whole workflow folder into the target repository (the launcher and workflow.md must stay together):

~~~sh
mkdir -p /path/to/target/.noe/lib
cp -R /path/to/noe/.noe/lib/workflow /path/to/target/.noe/lib/workflow
cd /path/to/target
noe run .noe/lib/workflow/run.js --requirements docs/settled-requirements.md
~~~

To start from the sample role config, copy it only when you do not already have a config to preserve:

~~~sh
cp .noe/lib/workflow/config.example.json .noe/workflow.json
~~~

The module imports and reads workflow.md relative to its own copied location. Requirements and config paths resolve from the actual process working directory, so launch from the target repository root. There is no --repo option or fake working-directory setting.

The required --requirements PATH names the settled requirements file. Optional --config PATH supplies role overrides. If --config is omitted, .noe/workflow.json is read when present; if it is absent, built-in model defaults are used. Paths may be absolute; relative paths are relative to the current working directory.

~~~sh
noe run /absolute/path/to/copied/workflow/run.js --requirements requirements.md --config .noe/workflow.json
noe run /absolute/path/to/copied/workflow/run.js --help
~~~

Help parses arguments and prints usage without reading a requirements/config file or starting an agent. It needs no Git repository or provider call. Normal launch needs NOE native agent operations and the configured provider.

## Model settings

Built-in defaults:

| Role | Provider | Model | Effort |
| --- | --- | --- | --- |
| Parent, general, planning | openai-chatgpt | gpt-6.1-sol | high |
| Review | openai-chatgpt | gpt-6.1-sol | xhigh |
| Routine implementation/exploration | openai-chatgpt | gpt-6-luna | max |
| Difficult implementation/exploration | openai-chatgpt | gpt-6.1-sol | high |

An optional config is ordinary role data. It can override provider, model, and/or effort per role; omitted fields inherit the built-in default. Unknown role names or role-setting fields are rejected. No Git branch/release assertion or command registry is required.

The launcher invokes native run_agent with the parent role and explicit allowDelegation:true. The parent uses native agent lifecycle tools to delegate and review; child permission is not automatically inherited. The launcher waits for the native task to finish, prints the parent's natural-language answer, and returns a nonzero process status for native failure, cancellation, another non-completed state, or launcher errors. It does not parse or validate the answer.

## Roles and procedure

The launcher requester/outer agent, workflow parent, and parent workers are distinct. Use [agent-instruction.md](agent-instruction.md) when an existing agent launches the standalone CLI. The dedicated parent follows [workflow.md](workflow.md): read settled requirements and applicable work rules/current Git state; create and independently review a useful plan; implement/test/review/repair and locally commit slices; validate requirements and cross-slice behavior; then verify a safe destination and push once. No release, tags, force push, or history rewriting.

The parent owns all completion judgments and Git operations through normal repository tools, preserves unrelated work, and reports milestone progress and actual commits/push state. Workers do not recursively launch the workflow, further-delegate by default, or commit/push. Native events/outcomes and a small human-readable run note are sufficient evidence; no custom artifact framework is included.

## Files

- run.js: standalone NOE CLI entry.
- workflow.js: importable launcher, argument handling, role defaults and native run_agent call.
- workflow.md: fixed procedure and workflow-parent responsibilities.
- config.example.json: optional role override example.
- agent-instruction.md: outer requester launch guidance and role boundaries.
- test.js: focused offline launcher check with injected run_agent; it makes no provider call.

Run the offline check with NOE from any working directory:

~~~sh
noe run /absolute/path/to/workflow/test.js
~~~

The example entry in examples/agent/development-workflow.js invokes the same launcher. The workflow package is standalone; it imports only NOE native APIs and standard-library modules.
