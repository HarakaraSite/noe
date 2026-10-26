# JavaScript checks and formatting

Biome is installed at `~/.local/bin/biome` and is available as `biome` without Node.js or npm. The workspace configuration is `biome.json`.

Check saved JavaScript when creating or changing it:

```sh
biome lint task.js
```

The configuration checks syntax, undefined variables, const reassignment, unreachable code, and duplicate keys, arguments, or declarations. Supported NOE globals such as `serve` are registered. Agent helpers are module exports: import them from `noe:agent` in both saved scripts and run_js snippets.

For a long snippet that needs a pre-execution check, save it in a temporary directory and run `biome lint /path/to/snippet.js` from the workspace root. Short reads and check invocations do not need recursive checking. From run_js, use `execFile("biome", ["lint", path])` from `node:child_process` and inspect stdout/stderr on failure.

Use `biome format --write task.js` only when formatting is wanted. Use lint for checks, and do not expand scope solely for formatting differences.

TypeScript's `tsc` CLI is available at `~/.local/bin/tsc` and runs on Node.js. It checks JavaScript without requiring a rewrite to TypeScript.

```sh
tsc --noEmit --project .zed/jsconfig.json
```

This configuration checks JavaScript under `.zed/` using `stdlib/types/noestdlib.d.ts` and `agent/types/noeagentlib.d.ts`. It does not execute JavaScript or generate files.

To check a file elsewhere, create a temporary configuration whose extends is the absolute path to `.zed/jsconfig.json`. Set files to the absolute paths of the target JavaScript and both declaration files, and set include to `[]` so inherited include paths do not add unrelated files. Run `tsc --noEmit --project /path/to/config.json`. From run_js, invoke tsc with execFile and inspect both stdout and stderr on failure.

Biome lint and tsc type checking serve different purposes. The declarations describe NOE's public APIs and its supported Web API subset; they do not use the DOM library or imply that every profile enables every API. When a declaration is missing or availability is uncertain, consult `noe:docs` and check the required behavior.
