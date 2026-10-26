# Searching local file content

`.noe/lib/search/search.js` provides `grep(options)` for NOE. It has no additional npm dependency, uses `rg` on PATH, and falls back to GNU-compatible `grep` only when the executable is unavailable. Search-condition errors and execution errors do not trigger fallback. Importing the library starts no command.

```js
import {resolve} from "node:path";
const {grep} = await import(resolve(".noe/lib/search/search.js"));
export default await grep({
  pattern: "outputLimit",
  paths: ["internal/agent"],
  glob: ["*.go", "!*_test.go"],
  mode: "lines"
});
```

| Argument | Default | Meaning |
| --- | --- | --- |
| `pattern` | Required | Search string; newlines and NUL are rejected; an empty string matches every line |
| `paths` | `["."]` | Array of files or directories, relative to cwd |
| `mode` | `"lines"` | `lines`: matching lines; `files`: matching filenames; `count`: matching line counts per file |
| `regex` | `true` | Regular expressions by default; `foo|bar` searches alternatives; use `false` for literal matching |
| `ignoreCase` | `false` | Match without case sensitivity |
| `glob` | `[]` | File filters, such as `["*.js", "!*.test.js"]`; rg glob syntax |
| `maxResults` | `50` | Maximum number of returned lines or files; positive integer |
| `maxBytes` | `8000` | UTF-8 byte limit for the **entire JSON-encoded return object**; positive integer; too little room for metadata is an error |
| `backend` | `"auto"` | Normally automatic; can be fixed to `"rg"` or `"grep"` for validation |

Example result:

```json
{"backend":"rg","mode":"lines","results":[{"path":"src/main.js","line":12,"text":"const answer = 42;"}],"complete":true,"limitedBy":null,"warnings":[]}
```

- Each `lines` entry is `{path,line,text}`. Line numbers are one-based. Only the trailing LF is removed. Several matches on one line still count as one result.
- Each `files` entry is a path string. These are **files with matching content**, not a search of filenames themselves. Use [find()](../find/find.md) to search by name.
- Each `count` entry is `{path,count}`. Count means matching **lines**, not individual matches. Files with no matches are excluded.
- No matches returns `results: [], complete: true`. Exit code 1 is not an exception.
- `pattern: "foo|bar"` is regex OR by default. Use `regex: false` for literal punctuation such as dots or parentheses (for example, `{pattern: "a.b", regex: false}`). Invalid regexes throw; the library does not silently switch to literal matching.
- Results beyond a limit return `complete: false` and `limitedBy: "maxResults" | "maxBytes"`, then stop the search. The total count is unknown. If the search finishes with exactly the maximum number of entries, `complete: true` is returned.
- A long matching line returns its path, line number, and the leading text that fits, with `textTruncated: true` and `complete: false`. If even the path and other fields cannot fit, that entry is omitted. The library never returns an incomplete JSON string.
- stdout is processed as a stream, and the child process is stopped and reaped at a limit. When combining searches, keep their total within run_js's output budget. Traversal errors that would occur after a limit stops the search cannot be checked.
- Normal exit codes of 2 or higher and launch failures throw. Diagnostic text is kept short. Result order is unspecified. Output is decoded as UTF-8.
- In `lines` mode, an rg binary-match notice is recorded in `warnings` instead of being mixed into matching lines.

## Differences in the grep fallback

Validated with GNU grep 3.11 in this environment. Options such as `--null` and `--include` mean this does not target every minimal grep implementation.

- `backend: "grep"` and `warnings` identify fallback. `.git` directories are excluded, but `.gitignore` and other ignore files are not read; hidden files are searched.
- The default regex search uses grep's extended POSIX regular expressions, which are not fully compatible with rg's regexes. Use shared syntax across backends, or `regex: false` for literal matching.
- `glob` supports basename `*`, `?`, `[]`, and a leading `!` for exclusion. Path globs, `**`, and brace expansion are explicitly rejected. Exact precedence when includes and excludes conflict also depends on the backend.
- Both backends normally skip binary files and do not follow symbolic links during directory traversal. Explicitly supplied files and similar cases follow each command's behavior.

To reduce output, first locate files with `mode: "files"`, then narrow paths and globs and read matching lines with `mode: "lines"`. Use `mode: "count"` when only counts are needed.

Validation: `noe run .noe/lib/search/test.js` (requires rg and GNU-compatible grep).
