# NOE — No Ordinary Engine

Just JavaScript. Until you need an agent.

A tiny JavaScript runtime written in pure Go.

This English README is the canonical version. [日本語版](README.ja.md)

NOE is a Go runtime for small scripts and autonomous agent tasks. Run JavaScript directly, or describe a task with `run_agent()` and let an agent read files, execute JavaScript, revise its work, and return an answer. It uses [go-quickjs](https://github.com/go-quickjs/go-quickjs) as its JavaScript engine.

**JavaScript is supported today.** npm and package management are outside the project's scope. Complete Node.js or Deno compatibility is not a project goal. NOE enables the pinned go-quickjs standard library, whose Web and Node-style APIs are useful subsets rather than complete platform compatibility.

[API reference](docs/api.md) · [Migration from the old APIs](docs/api.md#旧apiからの移行) · [REPL guide](docs/repl.md) · [Examples](examples/README.md) · [Development notes](docs/README.md)

The linked guides and development notes are currently in Japanese.

[v1.3.0 release notes](docs/releases/v1.3.0.md) · [Download binaries](https://forge.harakara.site/littleisland/noe/releases/tag/v1.3.0)

## Build and run

Go 1.27.2 or later is required. No C toolchain is needed. Clone the repository, then build from its root:

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
```

```sh
./noe --version
# noe v1.3.0

./noe run examples/hello.js
# Hello from NOE!

./noe eval 'console.log(1 + 1)'
# 2

./noe repl
```

## Try it out

Summarize a JSON array from standard input:

```sh
printf '[100,200,300]\n' | ./noe run examples/summarize-stdin.js
# {"count":3,"total":600,"average":200}
```

Summarize JSON files in a directory:

```sh
./noe run examples/collect-json.js examples/data/numbers
# Prints per-file results and totals: count: 5, total: 800, average: 160
```

Print three times, one second apart, using asynchronous sleep:

```sh
./noe run examples/sleep-demo.js
```

See the [examples](examples/README.md) for file transformation, report generation, and HTTP downloads.

## Autonomous agents

Import `noe:agent` from a script to start an agent. The agent calls `run_js` to read and write files, execute JavaScript and commands, repair failures, and delegate to children. It continues without interactive approval and returns its answer when finished. Credentials and provider connections are handled by Go; generated JavaScript runs with the same OS permissions as NOE.

### Choose a provider and register credentials

| `provider` | Example `model` | Default credential file under `~/.config/noe/credentials/` |
| --- | --- | --- |
| `opencode-go` | `deepseek-v4.1-flash`, `gpt-6-luna` | `opencode-go-api-key` (text) |
| `openrouter` | `deepseek/deepseek-v4.1-flash`, `openai/gpt-6-luna` | `openrouter-api-key` (text) |
| `openai-chatgpt` | `gpt-6.1-sol`, `gpt-6-luna` | `openai-chatgpt.json` (ChatGPT sign-in) |
| `openai-api` | `gpt-6.1-sol`, `gpt-6-luna` | `openai-api-key` (text) |

Put your API key in the corresponding text file. To use another location, set `NOE_OPENCODE_GO_API_KEY_FILE`, `NOE_OPENROUTER_API_KEY_FILE`, or `NOE_OPENAI_API_KEY_FILE`. For ChatGPT credentials, use `NOE_OPENAI_CHATGPT_CREDENTIALS_FILE`. When `XDG_CONFIG_HOME` is set, defaults use `$XDG_CONFIG_HOME/noe/` instead of `~/.config/noe/`. Credentials are read lazily on the first model request.

For ChatGPT sign-in, install the external helper and run it with NOE:

```sh
mkdir -p ~/.config/noe
cp scripts/auth-chatgpt.js ~/.config/noe/auth-chatgpt.js
./noe run ~/.config/noe/auth-chatgpt.js
```

Open the displayed URL in a browser. If you downloaded only the binary, [download the v1.3.0 helper](https://forge.harakara.site/littleisland/noe/raw/tag/v1.3.0/scripts/auth-chatgpt.js) in place of the `cp` step. The [OpenAI guide](docs/openai.md) covers remote-terminal sign-in and credential renewal. OpenAI uses the official OpenAI Go SDK and defaults to Responses; OpenRouter and OpenCode Go use the OpenRouter Go SDK. Chat Completions and Responses are supported; Messages is not implemented.

### Run one task and receive its answer

Save this as `task.js`:

```js
import { run_agent } from "noe:agent";

const snapshot = await run_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "summary",
}, "Read README.md and summarize it in three points.");

if (snapshot.lastResult?.state !== "completed") {
  throw new Error(snapshot.lastResult?.error || snapshot.reason || "Agent failed");
}
console.log(snapshot.lastResult.answer);
```

```sh
./noe run task.js
# Print the answer after completion

./noe run --observe --events=agent.jsonl task.js
# Show progress live and also save detailed JSONL
```

The root agent requires explicit `provider`, `model`, and `effort`. Switch providers by changing the first two fields together; model IDs can differ between providers. `effort: "auto"` leaves the effort to the provider/model. Other accepted values are `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, and `max`; actual support depends on the selected model.

`run_agent` waits until the agent instance stops and cleanup finishes. Normal task failures are returned in `snapshot.lastResult`, so check its state as above. Invalid input and startup acceptance failures reject the Promise. Optional settings include:

| Setting | Meaning |
| --- | --- |
| `name` | Label in observations; defaults to `agent` |
| `system` | Additional instructions fixed during each task; pass file contents here to use external instructions |
| `maxSteps` | Maximum model calls per task; omitted or `0` means no NOE limit |
| `maxTokens` | Output token limit per model request; omitted or `0` leaves it unspecified |
| `cacheKey` | Stable cache/routing key; omitted uses the task's `runId`, and `""` omits the field |

For example, read an instruction file with `await readFile("instructions.md", "utf8")` from `node:fs/promises` and pass the resulting string as `system`. NOE does not automatically discover `AGENTS.md`. Cache keys do not preserve conversation history or guarantee cache hits. OpenRouter sends the key as `session_id`; OpenAI uses `prompt_cache_key`. OpenCode Go sends `x-opencode-session` with the actual run ID, fixed within a task and distinct for parent and child tasks, plus `User-Agent: noe/<version>`.

To limit total execution time, put `--timeout 5m` before the script name. For an arbitrary task with the prepared OpenRouter configuration, use [try-agent.js](try-agent.js):

```sh
./noe run --observe try-agent.js 'Read README.md and write a summary to SUMMARY.md.'
```

### Start work without waiting, observe it, and delegate

| Function | When it returns / purpose |
| --- | --- |
| `run_agent(config, instruction, options?)` | Waits for the instance to stop, then returns its snapshot |
| `spawn_agent(config, instruction?, options?)` | Returns `{agentId}` after acceptance, before the task finishes |
| `observe_agent({agentId})` | Immediately returns current state, `currentRunId`, and `lastResult` |
| `queue_agent({agentId, instruction})` | Accepts another task for a `keepAlive` instance |
| `steer_agent({runId, instruction})` | Adds guidance to the active task before its next model decision |
| `interrupt_agent({runId})` / `interrupt_agent({agentId})` | Accepts cancellation of one task or the whole instance |

Ordinary JavaScript can launch independent root agents concurrently with `Promise.all([run_agent(...), run_agent(...)])`; each root needs its own explicit provider/model/effort. Use `spawn_agent` when the caller needs to observe or steer work while it runs. See the [live-control example](examples/agent-control.js).

In model-generated `run_js`, these six lifecycle functions are already bound. Children inherit the current parent's provider, model, effort, and `maxTokens` when omitted. For example, the parent can execute this tool code:

```js
// Summarize the two documents in parallel
const results = await Promise.all([
  run_agent({ name: "english" }, "Summarize README.md in three points."),
  run_agent({ name: "japanese" }, "Summarize README.ja.md in three points."),
]);
console.log(JSON.stringify(results));
```

This shortened configuration is for children inside `run_js`; ordinary root scripts must specify all three required settings. Child `system`, `name`, `maxSteps`, `cacheKey`, and history are not inherited. Each `run_js` starts a fresh NOE process; use files when data must survive between tool calls. [agent-parallel.js](examples/agent-parallel.js) demonstrates parent-driven delegation.

### Keep an agent available for more tasks

Pass `{keepAlive: true}` as the **third argument** to keep an instance waiting after each task. Omit the initial instruction to start idle without a model request:

```js
import { spawn_agent } from "noe:agent";

const { agentId } = await spawn_agent({
  provider: "opencode-go",
  model: "deepseek-v4.1-flash",
  effort: "auto",
  name: "worker",
}, undefined, { keepAlive: true });
console.log(agentId);
```

The NOE process keeps running even after this script body finishes. In another terminal, find its IDs and send work:

```sh
./noe agents
./noe queue <agent-id> 'Summarize README.md'
./noe steer <run-id> 'Use a table for the answer'
./noe queue <agent-id> 'Run the tests next'
./noe interrupt --run <run-id>
./noe interrupt --agent <agent-id>
```

An `agentId` identifies the instance; each task has a new `runId` and fresh conversation history. Queued tasks run in order. `steer` adds to the current task's history without stopping its current request or JavaScript. Task interruption preserves a keepAlive instance and its other queued tasks; instance interruption stops all of them. Commands return after acceptance; use observations to confirm completion.

`run_agent(..., {keepAlive: true})` waits for the entire instance to stop, including idle time. In the REPL, a live keepAlive instance also keeps the current input active. Instances remain owned by the current NOE execution or parent run and are collected on owner exit, cancellation, timeout, or Ctrl+C; they do not continue independently after NOE exits. See the [lifecycle and control reference](docs/api.md).

### Observe progress and discover models

`--observe` shows live thinking and summaries when supplied, intermediate messages, the first-line comment of each `run_js`, success/failure, stderr, and final answers. Consecutive thinking fragments are joined; messages and answers use color-free Markdown formatting with aligned tables. Ordinary script/tool stdout is hidden in this view.

```sh
./noe run --observe --events=agent.jsonl task.js
./noe observe --follow agent.jsonl
./noe run --events=- task.js | jq --unbuffered -c .
./noe repl --events=agent.jsonl
```

`--events` works with `run`, `eval`, and `repl`: `--events=-` writes JSONL to stdout, a path appends to that file, and bare `--events` creates `noe-events-<date>-<time>-<id>.jsonl` in the starting working directory. Events retain ordinary JS output, generated JavaScript source, and captured exception details. The legacy `NOE_AGENT_TRACE=1` metadata trace remains available on stderr. See the [event guide](docs/events.md) and [observation entry points](docs/observe.md); parents can also read saved JSONL through `observe_agent({logPath, runId, ...})`.

`list_models({provider?, kind?})` returns built-in and externally configured model metadata without contacting a provider. Endpoints, headers, parameters, model-specific routes, and catalog entries can be overridden in `~/.config/noe/config.json` or `NOE_CONFIG_FILE`. See [provider configuration](docs/providers.md), the [model catalog](docs/model-catalog.md), and [Jev routing example](examples/agent-route-model.js). Directly specifying a model in JavaScript is also supported; the catalog is not an allowlist.

For a one-off agent in the REPL:

```text
> const am = await import("noe:agent")
> const result = await am.run_agent({provider:"opencode-go", model:"deepseek-v4.1-flash", effort:"auto"}, "Summarize README.md")
> console.log(result.lastResult.answer)
```

**Migrating from v1.2.0:** `createAgent()` and `agent.run()` have been removed. Import `run_agent` or `spawn_agent`, explicitly set provider/model/effort for roots, and read answers from `lastResult.answer`. There are no compatibility aliases. See the [API migration guide](docs/api.md#旧apiからの移行).

## Structured decisions

Use `import { decide } from "noe:decision"` to submit one structured decision request to OpenRouter. Pass explicit `provider`, `model`, `state`, and named `questions`; the Promise returns the full response JSON. It works in ordinary scripts and agent-generated `run_js`, sharing the existing credentials and cancellation scope. See the [decision guide](docs/decision.md) and [example](examples/decision-classify.js).

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/decision-classify.js 'Checkout failed.' typesafe/jev-1.14
```

## HTTP server

You can write a small HTTP server in JavaScript. Start the demo:

```sh
./noe run examples/http-server.js 8080
```

Try it from another terminal:

```sh
curl http://127.0.0.1:8080/health
curl -H 'Content-Type: application/json' \
  -d '{"message":"Hello"}' http://127.0.0.1:8080/echo
```

Press Ctrl+C in the server's terminal to stop it. The demo defaults to port 8080. Pass 0 to let the OS choose an available port; the actual URL is printed at startup.

The global `serve(options, handler)` takes a Web `Request` and expects a Web `Response`. Use `new URL(request.url).pathname` to route requests and `Response.json(value)` for JSON replies. The server handle exposes its URL and `close()`; see the [API reference](docs/api.md#httpサーバー).

## CLI

| Command | Behavior |
| --- | --- |
| `noe run <file> [args...]` | Run a file; script arguments are available in `process.argv.slice(2)` |
| `noe eval <code>` | Run code; `process.argv.slice(2)` is empty |
| `noe repl` | Run JavaScript interactively |
| `noe observe --follow <log>` | Read agent events in a human-readable view |
| `noe agents` | List active agents and their current run IDs |
| `noe steer <run-id> <instruction>` | Add instructions to an active task |
| `noe queue <agent-id> <instruction>` | Queue another task for a keepAlive instance |
| `noe interrupt --run <id>` / `--agent <id>` | Cancel a task or stop an instance |
| `noe --help` | Show usage; also shown when no arguments are supplied |
| `noe --version` | Show the version |

`.js` and `.mjs` files run as ES modules with Promise, `async/await`, and top-level await support. Local imports are relative to the importing file and require an explicit extension. File operations and relative imports in `eval` use the current working directory.

### Execution timeout

You can set an optional timeout for `run` and `eval`. There is no time limit by default.

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'await new Promise(resolve => setTimeout(resolve, 60000))'
# Exit code 124, with a timeout diagnostic on stderr
```

Place `--timeout` before the script name or code. Use a positive Go duration such as `500ms`, `5s`, or `1m`. Empty, invalid, zero, negative, and duplicate values are usage errors. Use `--` to end option parsing. `process.argv` contains the executable and source first; user arguments follow them.

Timing starts after the entry source has been read and includes runtime initialization, JS evaluation, asynchronous I/O, sleep, server listening, and subsequent JS execution. Forced interruption of synchronous host I/O, including `process.stdin.read()`, or OS reads during local imports is not guaranteed. Use Ctrl+C to interrupt the REPL. Output and file changes made before interruption remain.

## REPL

The same runtime is reused, so variables and functions remain available across inputs.

```text
> let count = 1
> count += 1
2
> _ + 1
3
> await new Promise(resolve => setTimeout(resolve, 100))
> process.exit()
```

The REPL supports line editing, persistent history, Tab completion, and multiline input. `_` holds the last successful result other than undefined. Ordinary input errors do not end the session.

Press Ctrl+D on an empty input, or call `process.exit()`, to exit. On supported terminals, Ctrl+C cancels input or running work and normally returns to the prompt. If stdlib I/O, servers, workers, or messaging resources remain active when an input is interrupted or fails, NOE stops them and ends the REPL session. Timers alone are cancelled and the session can continue. A server started in the REPL keeps the current evaluation active until it stops; Ctrl+C stops it.

REPL `await` supports expressions, single-variable initialization, and assignment. General multi-statement input containing `await` and static imports are not supported. Load modules with `await import(...)`. See the [REPL guide](docs/repl.md) for controls and supported syntax.

## API

| Purpose | API |
| --- | --- |
| Autonomous tasks | `noe:agent` (`run_agent`, `spawn_agent`, `observe_agent`, `queue_agent`) |
| Structured decisions | `noe:decision` (`decide`) |
| Model catalog | `noe:agent` (`list_models`) |
| ChatGPT credential registration | `noe:auth` (`save_chatgpt_credentials`), used by the sign-in helper |
| Output | `console.log`, `console.warn`, `console.error` |
| Arguments, environment, and exit | `process.argv`, `process.env`, `process.cwd()`, `process.exit()` |
| Standard input | `process.stdin.read()` |
| Text and binary files | `fs/promises` (`readFile`, `writeFile`) |
| Directories and file operations | `fs/promises` (`readdir`, `stat`, `lstat`, `mkdir`, `rename`, `rm`) |
| HTTP | global `fetch()` and `serve()` |
| Sleep | `timers/promises` (`setTimeout`) |

See the [API reference](docs/api.md) for arguments, return values, errors, and supported behavior.

## Built-in modules and capabilities

Scripts use the go-quickjs standard library's Web APIs and Node-style modules, including `fs`, `process`, `os`, `path`, `child_process`, `dns`, `http`, `worker_threads`, and `crypto`. It also provides timers, Web Streams, WebSocket, and workers. NOE additionally provides `noe:agent`, `noe:decision`, and `noe:auth` in the main runtime; importing them from a Worker is unsupported. NOE has no separate `noe` JavaScript global. These APIs are partial implementations; NOE provides the compatibility implemented by the pinned stdlib; full Node.js or Deno compatibility, npm, package installation, and the original QuickJS `std` module are not provided. Bare imports resolve only to installed built-ins; local source imports must use relative `.js` or `.mjs` paths.

The runtime grants scripts full access to the current process environment and local filesystem, command execution, DNS and network requests, HTTP listeners, WebSocket connections, and workers. It is not a sandbox: scripts can act with the operating-system privileges of the user running NOE.

On normal completion, NOE waits for started I/O, timers, and subsequent JS execution. A running server keeps the runtime alive. `process.exit(code)` stops the runtime and sets its exit status.

## Fetch API

The global `fetch()` is the go-quickjs Web API. It provides `Request`, `Response`, and `Headers`, and response bodies can be read as streams. A response body is consumed once, as with the Web API. Serialize JSON and set Content-Type explicitly when sending JSON.

HTTP 4xx and 5xx responses are returned as responses; transport failures reject the Promise. URL imports are not supported. HTTP servers use the global `serve()` API; WebSocket uses the standard library API.

[HTTP API details](docs/api.md#fetch) · [JSON API request examples](examples/README.md#json-apiへのリクエスト)

## Errors and testing

| run/eval exit code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Execution failure, unhandled Promise rejection, or interruption by SIGINT |
| `2` | CLI usage error |
| `124` | Execution timeout |

`process.exit(code)` sets the process exit status and stops execution. Errors include the location and stack supplied by the engine. NOE also diagnoses unfinished top-level await when no work remains that can resolve it.

The REPL continues after recoverable input errors and exits with 0 on EOF. Unrecoverable input-reading or engine errors, and interactive-terminal input errors or interruption while stdlib I/O remains active, exit with 1. SIGINT exits with 130 for non-terminal input or an external signal while waiting for input.

```sh
go test ./...
go test -tags=e2e ./... -count=1
```

The default E2E tests build the CLI with CGO disabled and run it as a separate process. They use temporary files and local HTTP servers, and check REPL interactions through a PTY on Linux. They do not connect to external services. Live OpenRouter tests require the separate `agent_live` build tag and an explicit credential-file path; see the [agent validation record](docs/increment-13-implementation.md).

See the [development notes](docs/README.md) for design, acceptance criteria, validation records, and development commands.

## License

NOE is licensed under the [MIT License](LICENSE).

Third-party code and data retain their respective licenses.
