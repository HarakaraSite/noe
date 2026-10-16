# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

This English README is the canonical version. [日本語版](README.ja.md)

NOE is a Go runtime for small scripts. It adds practical features such as file I/O, HTTP, and a REPL as needed. It uses [go-quickjs](https://github.com/go-quickjs/go-quickjs) as its JavaScript engine.

**JavaScript is supported today. TypeScript support is planned for later.** npm and package management are outside the project's scope. Complete Node.js or Deno compatibility is not a project goal. NOE enables the pinned go-quickjs standard library, whose Web and Node-style APIs are useful subsets rather than complete platform compatibility.

[API reference](docs/api.md) · [Migration from the old APIs](docs/api.md#旧apiからの移行) · [REPL guide](docs/repl.md) · [Examples](examples/README.md) · [Development notes](docs/README.md)

The linked guides and development notes are currently in Japanese.

## Build and run

Go 1.27.2 or later is required. No C toolchain is needed. Clone the repository, then build from its root:

```sh
git clone https://forge.harakara.site/littleisland/noe
cd noe
CGO_ENABLED=0 go build -o noe ./cmd/noe
```

```sh
./noe --version
# noe v1.1.0

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

## OpenRouter agent

NOE includes an optional autonomous agent module. It can read and change files, run commands, and return a final answer. The agent runs with the same operating-system permissions as NOE; it is not a sandbox. File contents used for the task may be included in requests to OpenRouter.

Store an OpenRouter API key in a text file, then point NOE to that file:

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-summary.js README.md
```

The file is read on the first model request. If the variable is unset, NOE looks for `$XDG_CONFIG_HOME/noe/credentials/openrouter-api-key`, or `~/.config/noe/credentials/openrouter-api-key` when `XDG_CONFIG_HOME` is unset. Importing the module or creating an agent does not read the key or contact the provider.

`agent.run()` returns a string; it does not print automatically. The example prints it. For parallel child agents that summarize the sample JSON files, run:

```sh
NOE_OPENROUTER_API_KEY_FILE=/path/to/openrouter-api-key \
  ./noe run examples/agent-parallel.js
```

The root agent requires a model. Child agents inherit the current run's provider, model, and `maxTokens` unless they override those options. Each run has a separate history. See the [examples](examples/README.md) and [API reference](docs/api.md) for model settings, optional limits, errors, REPL use, and trace output.

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
| Output | `console.log`, `console.warn`, `console.error` |
| Arguments, environment, and exit | `process.argv`, `process.env`, `process.cwd()`, `process.exit()` |
| Standard input | `process.stdin.read()` |
| Text and binary files | `fs/promises` (`readFile`, `writeFile`) |
| Directories and file operations | `fs/promises` (`readdir`, `stat`, `lstat`, `mkdir`, `rename`, `rm`) |
| HTTP | global `fetch()` and `serve()` |
| Sleep | `timers/promises` (`setTimeout`) |

See the [API reference](docs/api.md) for arguments, return values, errors, and supported behavior.

## Built-in modules and capabilities

Scripts use the go-quickjs standard library's Web APIs and Node-style modules, including `fs`, `process`, `os`, `path`, `child_process`, `dns`, `http`, `worker_threads`, and `crypto`. It also provides timers, Web Streams, WebSocket, and workers. NOE has no separate `noe` JavaScript global. These APIs are partial implementations; NOE provides the compatibility implemented by the pinned stdlib; full Node.js or Deno compatibility, npm, package installation, and the original QuickJS `std` module are not provided. Bare imports resolve only to installed built-ins; local source imports must use relative `.js` or `.mjs` paths.

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

E2E tests build the CLI with CGO disabled and run it as a separate process. They use temporary files and local HTTP servers, and check REPL interactions through a PTY on Linux. They do not connect to external services.

See the [development notes](docs/README.md) for design, acceptance criteria, validation records, and development commands.

## License

NOE is licensed under the [MIT License](LICENSE).

Third-party code and data retain their respective licenses.
