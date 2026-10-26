# JavaScript static checks and formatting

Biome 2.5.15 is installed for the user at `~/.local/bin/biome` and is available as `biome`. It requires neither Node.js nor npm. This workspace's configuration is in the root `biome.json`.

When creating or changing saved JavaScript, check the affected files:

```sh
biome lint task.js
```

The configuration focuses on syntax errors, undefined variables, reassignment to const, unreachable code, and duplicate keys, arguments, or declarations. NOE globals such as `serve` are registered.

To check a snippet such as a long run_js call before execution, save it as `run-js-snippet.js` in a temporary directory and run `biome lint /that-temporary-directory/run-js-snippet.js` from the workspace working directory. Only this filename also recognizes run_js prebindings such as run_agent and wait_task. In ordinary task.js files, import agent APIs from `noe:agent`. Short file reads and check invocations do not themselves need recursive checking.

From run_js, use `node:child_process` with `execFile("biome", ["lint", path])`. On a nonzero exit, inspect the exception's `stdout` and `stderr` and fix the reported problems.

Use `biome format --write task.js` on the affected files only when formatting is wanted. Use `lint` for checks, and do not expand the change solely to fix formatting differences.

TypeScript 5.9.3 is also installed for the user at `~/.local/bin/tsc`, matching the version Zed uses. `tsc` is a CLI that runs on Node.js; NOE's JavaScript does not need to be rewritten in TypeScript.

NOE's type declarations are in `.zed/noe.d.ts`, and the shared type-checking configuration is `.zed/jsconfig.json`. From the workspace root, this checks API names, arguments, and return types in JavaScript under `.zed/`. It neither executes JavaScript nor generates files.

```sh
tsc --noEmit --project .zed/jsconfig.json
```

This configuration covers only `.zed/`. To check JavaScript elsewhere, create a temporary configuration extending `.zed/jsconfig.json`, set `files` to the absolute paths of the target JavaScript and `.zed/noe.d.ts`, and set `include: []`. Run `tsc --noEmit --project /that-temporary-config.json`. The original include paths are inherited as relative paths, so explicitly override include when limiting the temporary configuration's scope. These declarations do not include run_js prebindings as globals.

From run_js, use `execFile("tsc", ["--noEmit", "--project", ".zed/jsconfig.json"])`. On a nonzero exit, inspect both `stdout` and `stderr` in the exception. Biome lint and tsc type checking are separate checks.

The declarations cover NOE's 11 public modules and the main standard APIs from fs/path/process/child_process; they do not declare every stdlib module. A missing declaration alone does not establish that NOE lacks an API: check its contract with `noe docs` or `noe:docs` and verify the required behavior as needed. Web APIs use TypeScript's standard DOM types, so completion suggestions do not guarantee that every browser API runs in NOE.
