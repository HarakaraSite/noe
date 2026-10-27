# Agent-first development workflow

A deliberately thin, portable launcher starts one native NOE workflow parent for settled requirements. The parent plans, delegates work/reviews, judges evidence, commits completed slices and decides whether a final push is safe. There are no mandatory model-based approval gates. Implementation workers implement and run focused checks in one continuous loop; the parent judges evidence before independent review and delivery. Another opinion is useful only for a concrete unresolved judgment, not as a routine stage approval. There is no mechanical workflow engine, decision classifier, Git controller, custom artifact store, or automatic resume.

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

The required --requirements PATH names the settled requirements file. Optional --config PATH supplies settings. If --config is omitted, .noe/workflow.json is read when present; if it is absent, built-in role defaults are used. Configuration supports role overrides only. Paths may be absolute; relative paths are relative to the current working directory.

~~~sh
noe run /absolute/path/to/copied/workflow/run.js --requirements requirements.md --config .noe/workflow.json
noe run /absolute/path/to/copied/workflow/run.js --help
~~~

Help parses arguments and prints usage without reading a requirements/config file or starting an agent. It needs no Git repository or provider credentials. Normal execution requires configured agent-provider access (ChatGPT by default). The workflow adds no separate decision-provider credential requirement.

## Model settings

Built-in agent-role defaults:

| Role | Provider | Model | Effort |
| --- | --- | --- | --- |
| Parent, general, planning | openai-chatgpt | gpt-6.1-sol | high |
| Review | openai-chatgpt | gpt-6.1-sol | xhigh |
| Routine implementation/exploration | openai-chatgpt | gpt-6.1-terra | high |
| Difficult implementation/exploration | openai-chatgpt | gpt-6.1-terra | high |

An optional config can override agent provider, model, and/or effort per role. Omitted fields inherit defaults. For example:

~~~json
{
  "roles": {
    "routine": {
      "model": "gpt-6.1-terra",
      "effort": "high"
    }
  }
}
~~~

The only supported top-level setting is `roles`. Unknown top-level settings, role names and role-setting fields are rejected. Role-setting values must be nonempty strings. The experimental `decision` setting has been removed without compatibility support; remove it from old config files.

The launcher invokes native `run_agent` with the parent role and explicit `allowDelegation:true`. It passes role settings and requirements to that parent, with no decision settings, decision-model defaults or decision calls.

The launcher waits for the native parent task to finish, prints the parent's natural-language answer, and returns a nonzero process status for native failure, cancellation, another non-completed state, or launcher errors. It does not parse or validate the answer.

## Roles and procedure

The launcher requester/outer agent, workflow parent, and parent workers are distinct. Use [agent-instruction.md](agent-instruction.md) when an existing agent launches the standalone CLI. The dedicated parent follows [workflow.md](workflow.md): read settled requirements and applicable work rules/current Git state; draft a plan; obtain independent normal and critical plan reviews; resolve findings; and execute useful slices. Each implementation owner implements, runs focused checks and repairs related failures without an intervening parent test-authorization pause. The parent inspects the actual candidate and verification evidence, obtains independent code review, resolves findings and commits each completed slice locally. Overall validation and final delivery/push checks remain required.

The parent makes progress and completion judgments from current requirements, actual source/diffs, execution results and independent review. It reuses valid evidence rather than repackaging it for repeated stage approvals. Seek another opinion only for a concrete unresolved judgment, not routinely or to decide whether an extra opinion can be skipped. Unavailable or unusable advice is not approval and does not create a mandatory gate: resolve the actual question from evidence or report a concrete blocker. Preserve independent reviews, implementation/test/repair loops, per-slice local commits, overall validation and one ordinary safe final push. No release, tags, force push, or history rewriting.

The parent owns all completion judgments and Git operations through normal repository tools, preserves unrelated work, and reports milestone progress and actual commits/push state. Workers do not recursively launch the workflow, further-delegate by default, or commit/push. Ordinary command results, native events/outcomes and a small human-readable run note hold the evidence; no per-boundary approval records or custom artifact framework are required.

## Files

- run.js: standalone NOE CLI entry.
- workflow.js: importable launcher, argument handling, defaults/configuration and native `run_agent` call.
- workflow.md: fixed procedure, parent-owned judgments and continuous implementation/testing.
- config.example.json: optional role settings example.
- agent-instruction.md: outer requester launch guidance and role boundaries.
- test.js: focused offline launcher check with injected `run_agent`; it makes no provider call.

Run the offline check with NOE from any working directory:

~~~sh
noe run /absolute/path/to/workflow/test.js
~~~

The example entry in examples/agent/development-workflow.js invokes the same launcher. The workflow package is standalone; it imports only NOE native APIs and standard-library modules.
