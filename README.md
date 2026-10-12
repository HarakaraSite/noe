# NOE — No Opulence Engine

A tiny JavaScript/TypeScript runtime written in pure Go.

No bloat. Just scripts.

This English README is the canonical version. [日本語版](README.ja.md)

NOE is a Go runtime for small scripts. It adds practical features such as file I/O, HTTP, and a REPL as needed. It uses [go-quickjs](https://github.com/go-quickjs/go-quickjs) as its JavaScript engine.

**JavaScript is supported today. TypeScript support is planned for later.** npm support and Node/Deno compatibility are outside the project's scope and will not be implemented. Web APIs are limited to the subsets needed by NOE.

[API reference](docs/api.md) · [REPL guide](docs/repl.md) · [Examples](examples/README.md) · [Development notes](docs/README.md)

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
# noe v1.0.0

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

`noe.serve(options, handler)` returns a server handle. Handlers return a response object or a Promise of one, so they can use file reads and `noe.sleep()`. See the [API reference](docs/api.md#httpサーバー) for the contract.

## CLI

| Command | Behavior |
| --- | --- |
| `noe run <file> [args...]` | Run a file; script arguments are available in `noe.args` |
| `noe eval <code>` | Run code; `noe.args` is an empty array |
| `noe repl` | Run JavaScript interactively |
| `noe --help` | Show usage; also shown when no arguments are supplied |
| `noe --version` | Show the version |

`.js` and `.mjs` files run as ES modules with Promise, `async/await`, and top-level await support. Local imports are relative to the importing file and require an explicit extension. File operations and relative imports in `eval` use the current working directory.

### Execution timeout

You can set an optional timeout for `run` and `eval`. There is no time limit by default.

```sh
./noe run --timeout 5s examples/hello.js
./noe eval --timeout=500ms 'await noe.sleep(60000)'
# Exit code 124, with a timeout diagnostic on stderr
```

Place `--timeout` before the script name or code. Use a positive Go duration such as `500ms`, `5s`, or `1m`. Empty, invalid, zero, negative, and duplicate values are usage errors. Use `--` to end option parsing. Arguments after the script name are passed unchanged to `noe.args`.

Timing starts after the entry source has been read and includes runtime initialization, JS evaluation, asynchronous I/O, sleep, server listening, and subsequent JS execution. Forced interruption of synchronous OS syscalls or OS reads during local imports is not guaranteed. Use Ctrl+C to interrupt the REPL. Output and file changes made before interruption remain.

## REPL

The same runtime is reused, so variables and functions remain available across inputs.

```text
> let count = 1
> count += 1
2
> _ + 1
3
> await noe.sleep(100)
> noe.exit()
```

The REPL supports line editing, persistent history, Tab completion, and multiline input. `_` holds the last successful result other than undefined. Ordinary input errors do not end the session.

Press Ctrl+D on an empty input, or call `noe.exit()`, to exit. On supported terminals, Ctrl+C cancels input or running work and returns to the prompt. A server started in the REPL keeps the current evaluation active until it stops; Ctrl+C stops it.

REPL `await` supports expressions, single-variable initialization, and assignment. General multi-statement input containing `await` and static imports are not supported. Load modules with `await import(...)`. See the [REPL guide](docs/repl.md) for controls and supported syntax.

## API

| Purpose | API |
| --- | --- |
| Output | `console.log`, `console.warn`, `console.error` |
| Arguments, environment, and exit | `noe.args`, `noe.env.get`, `noe.cwd`, `noe.exit` |
| Standard input | `noe.readStdin` (run/eval only) |
| Text and binary files | `noe.readTextFile`, `noe.writeTextFile`, `noe.readFile`, `noe.writeFile` |
| Directories, file information, and file operations | `noe.readDir`, `noe.stat`, `noe.mkdir`, `noe.rename`, `noe.remove` |
| HTTP | `fetch`, `noe.serve` |
| Sleep | `noe.sleep` |

See the [API reference](docs/api.md) for arguments, return values, errors, and supported behavior.

On normal completion, NOE waits for started I/O, sleep, and subsequent JS execution. A running server keeps the runtime alive. `noe.exit()` prioritizes stopping and does not run subsequent JS, `catch`, or `finally` blocks.

## HTTP subset

The global `fetch(url, options)` supports GET, POST, PUT, PATCH, and DELETE. Responses provide `status`, `ok`, and repeatable `text()`, `json()`, and `bytes()` reads. Serialize JSON and set Content-Type explicitly. HTTP 4xx and 5xx statuses return responses; transport failures reject the Promise.

HTTP bodies are fully buffered in memory. Streaming, `Request`, `Response`, and `Headers` classes, URL imports, WebSocket, `setTimeout`, and `setInterval` are not provided.

[HTTP API details](docs/api.md#httpサブセット) · [JSON API request examples](examples/README.md#json-apiへのリクエスト)

## Errors and testing

| run/eval exit code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Execution failure, unhandled Promise rejection, or interruption by SIGINT |
| `2` | CLI usage error |
| `124` | Execution timeout |

`noe.exit(code)` returns the specified integer code from 0 to 255. Errors include the location and stack supplied by the engine. NOE also diagnoses unfinished top-level await when no work remains that can resolve it.

The REPL continues after recoverable input errors and exits with 0 on EOF. Unrecoverable input-reading or engine errors exit with 1. SIGINT exits with 130 for non-terminal input or an external signal while waiting for input.

```sh
go test ./...
go test -tags=e2e ./... -count=1
```

E2E tests build the CLI with CGO disabled and run it as a separate process. They use temporary files and local HTTP servers, and check REPL interactions through a PTY on Linux. They do not connect to external services.

See the [development notes](docs/README.md) for design, acceptance criteria, validation records, and development commands.
