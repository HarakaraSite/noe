# Agent instructions

NOE embeds execution and API facts, such as run_js runtime isolation, result serialization, resource ownership, and available documentation. The version-matched [API reference](reference.md) is bundled with the executable.

Behavioral choices belong to users: autonomy and approval preferences, language and comment style, tool and model selection, delegation, investigation procedures, validation scope, and reporting. The CLI reads these choices from external instruction files. It injects no built-in behavioral policy and has no hidden fallback when those files are absent or removed.

## Composition and locations

An agent instance composes its system message once when its first task starts:

1. Built-in runtime and API facts, including the selected profile's capabilities.
2. Markdown from the selected external directories, in order.
3. The explicitly supplied `config.system` string.

By default the selected directories are common, then workspace:

| Scope | Directory |
| --- | --- |
| Common | `$XDG_CONFIG_HOME/noe/instructions/`, or `~/.config/noe/instructions/` when the variable is empty or unset |
| Workspace | `<NOE starting working directory>/.noe/instructions/` |

The workspace is the host's configured working directory, not the script directory or a temporary agent-script location. There is no Git-root or ancestor-directory discovery. Children use the same selected locations.

## Selecting directories for a CLI execution

Both `noe run` and `noe eval` accept repeatable `--inst DIR` or `--inst=DIR` options. Together they **replace** the default list, in the supplied order; common and workspace are used only if explicitly listed. Repeating a directory reads it again. `--no-inst` selects an empty external list and is mutually exclusive with `--inst`. It does not remove built-in facts or explicit `config.system`.

```sh
# An application-owned list; no app registry or identity is involved.
noe run --inst /srv/my-app/instructions --inst ./team-rules app/main.js
noe eval --inst=./team-rules 'console.log("ready")'
noe run --no-inst app/main.js
```

The previous names `--instruction-dir` and `--no-instructions` remain accepted as compatibility aliases for `--inst` and `--no-inst`, respectively. The directory alias supports both separated and `=` forms; old and new names can be mixed under the same ordering and exclusivity rules.

Options precede the first operand; `--` ends option parsing. Arguments after a run script are passed to the script, even if they look like instruction options. Relative directories are fixed against the actual starting cwd, not the entry script's directory. NOE does not interpolate environment variables or expand tildes in option values; any shell expansion occurs before NOE receives them. Selection changes instruction locations only, not the execution cwd or filesystem behavior.

Files with the lowercase extension `.md` are read from each directory's immediate children in filename order. Names such as `10-tools.md` and `20-project.md` can make that order explicit. Subdirectories are not traversed. Symbolic links to regular files are supported.

Contents are appended. Files with the same name in different selected directories are all read; one does not replace another. Likewise, `config.system` is appended rather than replacing loaded files. To remove an instruction from the composed text, edit or remove the source file before creating a new agent instance.

Missing directories add no instructions. An unreadable directory or instruction file fails the task before a model request, with the affected path reported.

Go applications control their own `HostConfig.Instructions` and optional `HostConfig.Policy`. Neither a NOE behavioral policy nor filesystem instruction discovery is automatically added to a custom Go host. See [Go agent embedding](go-agent-embedding.md).

## Editable workspace preferences

The source repository contains ordinary external instructions:

- [behavior.md](../.noe/instructions/behavior.md): autonomy and interaction preferences.
- [tool-use.md](../.noe/instructions/tool-use.md): comment style, API discovery, concurrent work, and model-backed decisions.
- [delegation.md](../.noe/instructions/delegation.md): task splitting, waiting, reuse, and handoff.
- [development.md](../.noe/instructions/development.md): investigation, validation, review, and completion.
- [extention.md](../.noe/instructions/extention.md): local extension-library discovery and usage.
- [js-tools.md](../.noe/instructions/js-tools.md): this workspace's lint and type-checking workflow.

Edit, replace, or remove these files to choose the workspace's behavior. To use selected preferences across workspaces, place copies in the common directory. NOE does not install, recreate, or restore these files automatically. Keeping them external is a user-control boundary, not a trial before embedding them into the binary.

## Library guidance

An instruction file can advertise a library without making it part of NOE's built-in policy:

```md
For web search, use the local Exa helper described in
/path/to/workspace/.noe/lib/exa/exa.md.
Import its JavaScript module by resolving its absolute path from cwd.
```

An import map is separate and can be selected with `--import-map`. Adding Markdown does not install a library or discover an import map.

## Adding explicit system text

`system` is a string. The caller can read and concatenate selected files in a chosen order:

```js
import {readFile} from "node:fs/promises";
import {run_agent} from "noe:agent";

const files = ["./AGENTS.md", "./rules.md"];
const system = (
  await Promise.all(files.map(path => readFile(path, "utf8")))
).join("\n\n");

const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  system,
}, "Summarize README.md in three points.");
console.log(result.lastResult);
```

AGENTS.md is not discovered automatically. A file-path array cannot be passed directly as system.

## Snapshot lifetime

Instruction locations are fixed for the host execution, but file contents are read once per instance, before its first actual task's hooks or model request. Ordinary JavaScript and idle agent startup do not read them. Later queued tasks reuse the same composed system message, even if the files have changed.

A new instance, including a new child, reads the current files at its own first task. It does not inherit the parent's loaded instruction text or conversation. Explicit config.system is fixed for its instance and reaches a new child only when explicitly supplied.

The composed system message remains immutable. Changing external files takes effect for new instances without rebuilding the executable; updating the binary is required to change built-in runtime facts. Existing instances need to be replaced to use changed files.

## Runtime status and conversation history

NOE adds a user message beginning with `NOE_CONTEXT_STATUS` before each model request. It contains request and usage information, estimates, trimming notices, and runtime information supplied through `api.context.setRuntimeInfo()`. This is runtime status, not a behavioral instruction.

Each status message is retained with its assistant response and tool results. The next request, including a queued task in the same instance, preserves prior status messages and appends the new one. Transport conversion preserves message order. The status generator is `contextMessage` in [context.go](../internal/agent/context.go), and the task loop inserts it in [task.js](../internal/agent/js/task.js).

Automatic context management may remove older completed execution blocks when the budget is reached. It preserves the system message, original task, applied steering, started queue-task instructions in conversation order, and the most recent completed block. Tool calls and their results are removed together. The next queued task inherits the effective conversation and trimming state. Removed blocks do not return; separate instances have independent conversations.

Save useful information to a known file when needed for later work; this is optional. See the [hooks and context guide](hooks.md) for configuration and budget semantics. Earlier context-status and cache investigations remain historical evidence in the [efficiency observation record](development/agent-efficiency-observation.md).
