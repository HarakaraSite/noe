# Agent-first development workflow

A deliberately thin, portable launcher starts one native NOE workflow parent for settled requirements. The parent plans, delegates work/reviews, judges evidence, commits completed slices and decides whether a final push is safe. It uses NOE's native Jev Decisions API at six readiness boundaries; Jev supplements, but does not replace, the parent's judgment or independent reviews. There is no mechanical workflow engine, decision classifier, Git controller, custom artifact store, or automatic resume.

## Copy and run

Copy the whole workflow folder into the target repository (the launcher and workflow.md must stay together):

~~~sh
mkdir -p /path/to/target/.noe/lib
cp -R /path/to/noe/.noe/lib/workflow /path/to/target/.noe/lib/workflow
cd /path/to/target
noe run .noe/lib/workflow/run.js --requirements docs/settled-requirements.md
~~~

To start from the sample config, copy it only when you do not already have a config to preserve:

~~~sh
cp .noe/lib/workflow/config.example.json .noe/workflow.json
~~~

The module imports and reads workflow.md relative to its own copied location. Requirements and config paths resolve from the actual process working directory, so launch from the target repository root. There is no --repo option or fake working-directory setting.

The required --requirements PATH names the settled requirements file. Optional --config PATH supplies settings. If --config is omitted, .noe/workflow.json is read when present; if it is absent, built-in role and decision defaults are used. Existing role-only configuration files remain valid and inherit the Jev decision defaults. Paths may be absolute; relative paths are relative to the current working directory.

~~~sh
noe run /absolute/path/to/copied/workflow/run.js --requirements requirements.md --config .noe/workflow.json
noe run /absolute/path/to/copied/workflow/run.js --help
~~~

Help parses arguments and prints usage without reading a requirements/config file or starting an agent or decision request. It needs no Git repository or provider credentials. By default, normal execution makes OpenRouter decision calls in addition to ChatGPT agent calls, so both configured OpenRouter access and configured agent-provider access are required. If you override either provider, configure credentials for the selected providers.

## Model settings

Built-in agent-role defaults:

| Role | Provider | Model | Effort |
| --- | --- | --- | --- |
| Parent, general, planning | openai-chatgpt | gpt-6.1-sol | high |
| Review | openai-chatgpt | gpt-6.1-sol | xhigh |
| Routine implementation/exploration | openai-chatgpt | gpt-6-luna | max |
| Difficult implementation/exploration | openai-chatgpt | gpt-6.1-sol | high |

Built-in decision defaults (separate from roles and agent effort):

| Decision provider | Decision model |
| --- | --- |
| openrouter | typesafe/jev-1.14 |

An optional config can override agent provider, model, and/or effort per role, and decision provider and/or model independently. Omitted fields inherit defaults. For example:

~~~json
{
  "decision": {
    "provider": "openrouter",
    "model": "typesafe/jev-1.14"
  },
  "roles": {
    "routine": {
      "effort": "high"
    }
  }
}
~~~

The only supported decision settings are nonempty `provider` and `model` strings; agent `effort` does not apply to `noe:decision`. Unknown decision settings are rejected. Unknown role names or role-setting fields are also rejected. The top-level `decision` section is optional, so existing role-only `.noe/workflow.json` files need no changes.

The launcher invokes native `run_agent` with the parent role and explicit `allowDelegation:true`; it passes the effective decision provider/model separately in the parent's instructions. The launcher itself makes no decision call. At each required boundary the parent calls `noe:decision.decide` from its normal `run_js`, using the supplied settings and evidence for the next stage. Native NOE handles decision ownership, cancellation, usage, and events. There is no fallback to a different provider/model.

The launcher waits for the native parent task to finish, prints the parent's natural-language answer, and returns a nonzero process status for native failure, cancellation, another non-completed state, or launcher errors. It does not parse or validate the answer.

## Roles and procedure

The launcher requester/outer agent, workflow parent, and parent workers are distinct. Use [agent-instruction.md](agent-instruction.md) when an existing agent launches the standalone CLI. The dedicated parent follows [workflow.md](workflow.md): read settled requirements and applicable work rules/current Git state; draft a plan; assess readiness before independent normal and critical plan reviews; resolve findings; assess before implementation; and pause after implementation for Jev assessment before planned focused verification. It assesses verification before independent code review, review resolution before a local slice commit, and overall validation before final delivery/push.

Each assessment uses current requirements and actual candidate/evidence. The parent reads choice and available probabilities/confidence, compares them with evidence, resolves missing evidence or defects, and records each gate and its conclusion in the ordinary small run note. Refusals, unusable answers, errors, and disagreement are explicit; none is silently treated as approval. The parent remains responsible for completion and delivery. Preserve independent reviews, implementation/test/repair loops, per-slice local commits, overall validation and one ordinary safe final push. No release, tags, force push, or history rewriting.

The parent owns all completion judgments and Git operations through normal repository tools, preserves unrelated work, and reports milestone progress and actual commits/push state. Workers do not recursively launch the workflow, further-delegate by default, or commit/push. Native decision events and a small human-readable run note are sufficient evidence; no custom artifact framework is included.

## Files

- run.js: standalone NOE CLI entry.
- workflow.js: importable launcher, argument handling, defaults/configuration and native `run_agent` call.
- workflow.md: fixed procedure, Jev readiness assessments and workflow-parent responsibilities.
- config.example.json: optional decision and role settings example.
- agent-instruction.md: outer requester launch guidance and role boundaries.
- test.js: focused offline launcher check with injected `run_agent`; it makes no provider call.

Run the offline check with NOE from any working directory:

~~~sh
noe run /absolute/path/to/workflow/test.js
~~~

The example entry in examples/agent/development-workflow.js invokes the same launcher. The workflow package is standalone; it imports only NOE native APIs and standard-library modules.
