# NOE — No Ordinary Engine

Just JavaScript. Until you need an agent.

A tiny JavaScript runtime written in pure Go, powered by [go-quickjs](https://github.com/go-quickjs/go-quickjs). Use it for small scripts, file processing, HTTP servers, and autonomous agent tasks. An agent can read files, execute JavaScript and commands, revise its work, and return an answer without interactive approval.

This English README is the canonical version. [日本語版](README.ja.md)

[v1.11.0 release notes](docs/releases/v1.11.0.md) · [Download binaries](https://forge.harakara.site/littleisland/noe/releases/tag/v1.11.0)

## Build and run

Download a binary above, or build from source with Go 1.27.2 or later. No C toolchain is needed:

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
./noe --version
# noe v1.11.0
```

The examples below assume the repository root is your working directory.

## Run JavaScript

```sh
./noe eval 'console.log(1 + 1)'
# 2
./noe run examples/basics/hello.js
# Hello from NOE!
```

Use `eval` for short checks and `run` for scripts. Scripts support ES modules, `async/await`, files, Fetch, HTTP serving, and child processes. See the [examples](examples/README.md) for JSON processing, reports, downloads, and more.

Read the English API reference bundled with your executable using `./noe docs`, `./noe docs node:fs`, or `./noe docs noe:sockets connect`. Scripts and agents can read the same reference through `import { docs } from "noe:docs"`.

## Run an agent

For this example, save your OpenCode Go API key in `~/.config/noe/credentials/opencode-go-api-key` (or `$XDG_CONFIG_HOME/noe/credentials/opencode-go-api-key` when set). OpenRouter, ChatGPT sign-in, and OpenAI API keys are also supported; follow the [agent setup guide](docs/agents.md) for credentials and provider selection.

Save this as `task.js`:

```js
import { run_agent } from "noe:agent";

const result = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
}, "Read README.md and summarize it in three points.");

if (result.lastResult?.state !== "completed") {
  throw new Error(result.lastResult?.error || result.reason || "Agent failed");
}
console.log(result.lastResult.answer);
```

```sh
./noe run --observe task.js
```

Roots require explicit `provider`, `model`, and `effort`. `--observe` displays progress and the final answer. The [agent guide](docs/agents.md) covers saved logs, task files, and next steps.

## Guides

| Task | Guide |
| --- | --- |
| Set up credentials, run an agent, and observe progress | [Agent guide](docs/agents.md) |
| Find API arguments, CLI options, lifecycle controls, and exit codes | [API reference](docs/api.md) |
| Reuse local JS and verified ESM libraries | [Import maps](docs/import-map.md) |
| Configure providers and choose models | [Provider configuration](docs/providers.md) · [Model catalog](docs/model-catalog.md) |
| Add instructions, hooks, or context policies | [Instructions](docs/instructions.md) · [Hooks and context](docs/hooks.md) |
| Inspect running work and process logs | [Observation](docs/observe.md) · [JSONL events](docs/events.md) |
| Inspect/generate images or request structured decisions | [Images](docs/agent-images.md) · [Decisions](docs/decision.md) |
| Use live child-process I/O or HTTP | [Processes and streams](docs/process-io.md) · [HTTP examples](examples/http/README.md) |
| Use positional file I/O, TCP/Unix sockets, or OS signals | [Files, sockets, and signals](docs/low-level-io.md) |
| Explore examples or develop NOE | [Examples](examples/README.md) · [Documentation](docs/README.md) · [Development guide](docs/development/README.md) |

The linked guides are currently in Japanese. For older scripts, see the [API migration guide](docs/api.md#旧apiからの移行).

## Capabilities and compatibility

**NOE is not a sandbox. Scripts and agents run with the OS privileges of the user who starts NOE**, including filesystem, environment, command, and network access.

NOE uses the pinned go-quickjs engine and a NOE-maintained standard library, based on go-quickjs v0.27.0, for supported Web and Node-style APIs. Full Node.js/Deno compatibility, npm/package management, HTTP imports, and the original QuickJS `std` module are outside its scope. See the [API reference](docs/api.md#利用できる機能と権限) for supported behavior.

## License

[MIT](LICENSE). Third-party code and data retain their respective licenses.
