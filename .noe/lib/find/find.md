# Finding files and directories by name

`.noe/lib/find/find.js` provides `find(path?, options?)` for NOE.
It prefers `fd` on PATH and falls back to GNU-compatible `find` only when the executable is unavailable.
Execution errors and invalid search conditions do not trigger fallback. There is no npm dependency or shell execution, and importing the library starts no command.

```js
import {resolve} from "node:path";
const {find} = await import(resolve(".noe/lib/find/find.js"));
export default await find("examples", {name: "*import*", type: "directory"});
// results: ["import-map", "url-import"] (order is unspecified)
```

Use `ls()` to inspect structure, `find()` to locate entries by name,
and `grep()` to search file content. `find()` does not read file content.

## Arguments

`path` is the search root relative to cwd and defaults to `"."`. It must be a directory.
A file or symbolic link as the root is an error.

| Option | Default | Meaning |
| --- | --- | --- |
| `name` | `"*"` | Glob matching the entire file or directory basename, not a regex or plain substring |
| `type` | `"all"` | `all`, `file`, `directory`, or `symlink`; file means regular files only |
| `ignoreCase` | `false` | Match without case sensitivity |
| `maxDepth` | Unlimited | Traversal depth; immediate children are at depth 1; positive integer |
| `maxResults` | `300` | Maximum number of returned entries; positive integer |
| `maxBytes` | `32000` | UTF-8 byte limit for the entire JSON-encoded return object; positive integer |
| `backend` | `"auto"` | Normally automatic; can be fixed to `"fd"` or `"find"` for validation |

`name` accepts basename globs (`*`, `?`, `[]`).
Use `"*import*"` for a substring match, `"*.js"` for JavaScript, or `"README.md"` for an exact name.
Path globs, `**`, brace expansion, backslashes, NUL, and newlines are rejected.
Use `path` and `maxDepth` to narrow the hierarchy.
Details such as character classes and non-ASCII case matching depend on the backend and locale.
Prefer simple globs shared by both backends.

```js
export default await find(".noe/lib", {name: "*.js", type: "file", maxDepth: 2});
```

## Results and traversal scope

```json
{"backend":"fd","path":"/workspace/examples","results":["import-map","url-import"],"complete":true,"limitedBy":null,"warnings":[]}
```

- `path` is the absolute root path; `results` contains **path strings relative to the root**. The root itself is excluded. Order is unspecified.
- **Both backends include hidden and ignored entries.** Unlike fd's own defaults, the library supplies `--hidden --no-ignore`.
- Entries named `.git` are excluded and not traversed. Other ignore files are not applied. Narrow the root for large trees.
- Symbolic links can be found but are not followed. `all` also includes entry types other than regular files, directories, and symbolic links.
- No matches returns `results: [], complete: true`.
- Finding a match **beyond a limit** returns `complete: false` and `limitedBy: "maxResults" | "maxBytes"`, then stops and reaps the child process. If the search finishes with exactly the maximum number of entries, `complete: true` is returned.
- Paths are never cut partway through. Entries that do not fit are omitted. No total count or continuation offset is returned. If incomplete, narrow the root or conditions.
- The byte budget reserves room for completion metadata. A maxBytes value too small for the metadata is an error. When returning several results or file contents together, also account for run_js's output budget.
- stdout is processed as a NUL-delimited stream, supporting filenames with spaces or newlines. It is decoded as UTF-8.
- Abnormal exit codes, launch failures, and stderr diagnostics throw. fd receives `--show-errors`. Traversal errors that would occur after a limit stops the search cannot be checked.
- Fallback is identified by `backend: "find"`. Because both backends share the same traversal policy, fallback alone adds no warning.

The library uses GNU find-compatible options such as `-mindepth`, `-maxdepth`, `-iname`, and `-print0`;
it does not target every minimal find implementation. It does not automatically look for an executable named `fdfind`.

Validation: `./noe run .noe/lib/find/test.js` (requires fd and GNU-compatible find).
