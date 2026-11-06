# Process and standard I/O API direction and implementation

2026-11-06. This slice applies the approved
[standard library API policy](stdlib-api-policy.md). After reviewing the
concrete contract, the user approved one `noe:process` module and removal of
`node:child_process`, `child_process`, `node:process`, and `process` imports
without compatibility adapters. Global `process` remains available when
selected. This migration is local and unreleased.

## Public contract

```js
import api, {process, execFile, spawn, stdin, stdout} from "noe:process";

console.log(api.process === process, process === globalThis.process);
const {stdout: branch} = await execFile("git", ["branch", "--show-current"]);
console.log(branch.trim());
```

The default export is a namespace containing the selected named exports.
Metadata and runtime controls belong to the named `process` object, also
available as `api.process`. They are not flattened onto the namespace.
Command functions remain module exports rather than becoming properties of
global `process`.

| Capability | Named exports |
| --- | --- |
| `Profile.Modules.ProcessInfo` | `process`: the same metadata/control object as global `process` |
| `Profile.Modules.Stdio` | `stdin`, `stdout`, `stderr`: the existing host byte Web Streams |
| `Profile.Modules.Commands` | `exec`, `execFile`, `execFileSync`, `execSync`, `spawn`, `spawnSync` |

Any one capability selects the module. With none selected it is absent.
Selecting streams or commands does not expose metadata, create global
`process`, or select each other. With ProcessInfo and Stdio selected,
`process.stdin`, `process.stdout`, and `process.stderr` are the named stream
instances. The global retains its existing arguments, environment snapshot,
cwd, platform, arch, version, pid, clocks, exit/exitCode, microtask helper,
and event methods. This slice does not redesign those remaining utilities.

Command arguments, options, return shapes, and failure conventions are
retained:

- `execFile(file, args?, options?)` invokes a program directly and returns a
  Promise of `{stdout, stderr}`. `exec(command, options?)` uses the platform
  shell. Both reject on nonzero exit; outputs are strings by default or
  Uint8Array with `encoding: "buffer"`.
- `execFileSync` and `execSync` return stdout as text and throw an Error with
  captured output on failure. `spawnSync` returns the existing status/output
  object; inspect its `error` before treating status zero as success.
- `spawn(command, args?, options?)` resolves after startup to the existing
  owned ChildProcess. Its streams carry Uint8Array, `exited` resolves on
  nonzero exit, `kill` requests termination, and `close`/asyncDispose perform
  forced cleanup. Output EOF and process completion remain separate.
- Command cwd/environment replacement, executable lookup, authorization,
  output limits, timeout, and process-group/Job cleanup use the existing
  shared configuration and backends. Setting metadata `process.env` does
  not mutate the host environment or the Profile command defaults.

## Shared implementation and ownership

`internal/stdlib/process_module.go` assembles the selected exports and creates
one default namespace with the same JavaScript values as its named exports.
The core installer publishes metadata and captured commands; the NOE
installer completes that namespace with the existing native `spawn` and
standard streams before user code imports it. No global registry or second
native process implementation is introduced.

`internal/stdlib/process.go` still creates the selected global metadata
object. `internal/stdlib/exec.go` retains the captured-command runner.
`internal/noestdlib` retains the native host and borrowed-I/O adapters.
No public Go configuration field or dependency changes.

Workers retain capability selection, their own metadata/exit behavior, empty
stdin, shared configured output destinations, and execution-owned children.
Host standard streams remain demand-driven and borrowed; closing their Web
Stream endpoints does not close the caller's underlying reader/writer.
Runtime cancellation and Worker termination reclaim owned children.

## Migration

| Before | After |
| --- | --- |
| Commands imported from `node:child_process` or `child_process` | Same named functions or default namespace from `noe:process` |
| Named streams from `node:process` or `process` | Same named streams from `noe:process` |
| `import process from "node:process"` | `import {process} from "noe:process"` |
| `import old from "process"; old.env` | `import api from "noe:process"; api.process.env` |
| Global `process.env`, `process.cwd()`, `process.exitCode` | Retained when ProcessInfo is selected |

Bare `process` and `child_process` names are no longer reserved built-ins and
may be assigned by an import map. `node:` and `noe:` prefixes remain reserved
independently, including removed module names.

NOE's own examples, hidden libraries/instructions, authentication helpers,
Go consumers, declarations, and current references migrate together. The
official MCP spike's Node server keeps its Node imports; its NOE transport
uses `noe:process`. Historical release and phase records keep their original
contracts. noebot is outside this slice and remains its owner's independent
work; no noebot checkout or runtime has been investigated, edited, or tested.

## Verification

Local validation passed. Evidence is retained under
`/var/tmp/noe-process-policy-6fHylF`, including command logs/JSON, public
consumer artifacts and their manifest, and the final source manifest.

- Full `internal/stdlib`, `internal/nativeio`, and public `stdlib` unit
  suites; affected `internal/modulemap`, `internal/noestdlib`,
  `internal/jsexec`, `internal/runtime`, `internal/cli`, and API catalog
  suites. Public profile coverage checks all eight ProcessInfo/Stdio/Commands
  selections, exact export sets, namespace/global identities, and removed
  imports. Existing command authorization, cwd/environment, cancellation,
  borrowed-I/O, Worker, disposal, and external-helper checks passed.
- Selected real-CLI E2E checks for standard-library imports, run/eval,
  explicit exit, captured-command timeout, open stdin, binary streaming
  beyond the capture limit, observation output, blocked-I/O cancellation,
  Worker termination, and fixture-agent owned-process-tree cleanup.
- Five affected offline `.noe/lib` suites; log-analysis example; agent-server
  task/result and interrupt/cleanup scenarios; authentication-helper help
  startup; bundled docs from a standalone temporary candidate.
- Public stdlib consumers built and ran from a checkout replacement and an
  independent module artifact with CGO disabled. The artifact consumer built
  for Linux, Darwin, and Windows on amd64/arm64.
- TypeScript positive/negative fixtures, changed JavaScript/TypeScript lint,
  affected-package Go vet, and `git diff --check`.
- Bounded independent static review of core composition found no blocker.
  The first integration run found a documentation inventory-format mismatch;
  the export inventory was corrected and the full affected runtime suite
  passed. It was not a runtime API failure.

No native process/I/O backend, public Go option, or dependency changed. The
completed filesystem slice's unchanged backend results remain reusable.
The optional official MCP SDK spike was migrated but not rerun; its NOE
transport change is an import replacement. No release, push, installed-binary
replacement, process restart, or noebot validation was performed.
