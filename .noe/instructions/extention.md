# Extension library guide

## Listing, reading, and editing files

For listing, reading, creating, and partially editing files, normally use `ls()`, `read_file()`, `write_file()`, and `edit_file()` from `.noe/lib/files/files.js`.

If a filename or location is unknown, identify the target with `ls()`, `find()`, or a content search before reading it. Perform reads that depend on search results after the search completes.

```js
import {resolve} from "node:path";
const {ls, read_file, write_file, edit_file} = await import(resolve(".noe/lib/files/files.js"));
export default await ls();
```

`ls()` lists up to 200 immediate entries under cwd (usually the workspace) by default. Set `limit` to change the page size. Use `ls("internal", {tree: true, depth: 2})` for a tree listing, and pass `nextOffset` as `offset` to retrieve the next page. Hidden and ignored entries are included; symbolic links are not followed.

`read_file("README.md")` returns the entire file; `read_file("README.md", {offset: 101, limit: 80})` returns a range using one-based line numbers. The content is in `text`. For necessary larger content, increase run_js `outputBytes` (default 65536, maximum 262144 bytes).

`write_file("path/to/new.txt", "content")` creates parent directories and writes the entire file, overwriting an existing file. For partial edits, use `edit_file("app.js", "old text", "new text")`. It replaces exactly one match and throws without writing for zero or multiple matches. Pass `{all: true}` as the fourth argument only when all matches should change. Matching is literal, not a regular expression; include enough surrounding context and the correct line endings. Results summarize paths, bytes, and replacement counts; there is no need to return the entire file again. See `.noe/lib/files/files.md` for details. Use `grep()` below to narrow content searches.

## Finding files and directories by name

To find files or directories by name, normally use `find(path?, options?)` from `.noe/lib/find/find.js`. Use `ls()` to inspect structure and `grep()` below to search content.

It prefers `fd` and falls back to GNU-compatible `find` only when the executable is unavailable.

```js
import {resolve} from "node:path";
const {find} = await import(resolve(".noe/lib/find/find.js"));
export default await find("examples", {name: "*import*", type: "directory"});
```

`name` is a glob matching the entire basename (`*`, `?`, `[]`). Use `"*import*"` for a substring match. `type` is `all` (default), `file`, `directory`, or `symlink`; `maxDepth` limits depth. Results contain root-relative path strings in `results`, plus `backend`, `complete`, and `limitedBy`. Default limits are 300 entries and 32,000 bytes for the entire returned JSON. Both backends include hidden and ignored entries, exclude `.git`, and do not follow symbolic links. If incomplete, narrow the root or conditions. See `.noe/lib/find/find.md` for details.

## Searching local file content

For local content searches, normally use `grep(options)` from `.noe/lib/search/search.js`. Use external rg or grep commands when the library lacks a required capability.

Searches use regular expressions by default; `foo|bar` searches alternatives. If rg is unavailable, the library falls back to GNU-compatible grep. Results include backend, complete, and limitedBy. If output is limited, narrow paths or globs.

```js
import {resolve} from "node:path";
const {grep} = await import(resolve(".noe/lib/search/search.js"));
export default await grep({pattern: "outputLimit", paths: ["internal/agent"], mode: "files"});
```

`mode` is `files` (matching filenames), `lines` (default, matching lines), or `count` (matching line counts per file). Filter with `glob: ["*.go", "!*_test.go"]`, use `regex: false` for literal matching, and `ignoreCase: true` for case-insensitive matching. Use `regex: false` to search punctuation such as dots or parentheses literally. Default limits are 50 entries and 8,000 bytes for the entire returned JSON; set `maxResults` or `maxBytes` when needed. This guide is sufficient for basic use. See `.noe/lib/search/search.md` for details and differences in the grep fallback.

## Web search

Prefer this workspace's Exa library when web search is needed.

- Library: `.noe/lib/exa/exa.js`
- Usage and API reference: `.noe/lib/exa/exa.md`

These paths are relative to NOE's starting working directory. No import map is needed.

```js
import {resolve} from "node:path";
const {search, getContents} = await import(resolve(".noe/lib/exa/exa.js"));
export default await search("Go microVM library");
```

`search(query)` returns titles, URLs, and highlights for 5 results by default. It asks Exa for 1,000 highlight characters per page, but actual results can exceed that. `numResults` allows up to 20 results. For titles and URLs only, use `search(query, {mode: "links", numResults: 50})` for up to 50 results. These limits apply per query.

For page content, select a URL and call `getContents(url)` for that one page. It returns up to 20,000 characters of Markdown without HTML tags; use `maxCharacters` to request less. The old search format that directly specifies `contents`, body text, or subpages is an error. This guide is sufficient for basic use. See `.noe/lib/exa/exa.md` for filters, return values, and errors.
