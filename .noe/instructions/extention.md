# Extension library guide

Resolve library paths from NOE's starting working directory. No import map is needed. Read each library's reference for full options, return values, and output limits.

## Files and local search

Normally use these workspace libraries:

| Task | Module | Reference |
| --- | --- | --- |
| List, read, create, or edit files | `.noe/lib/files/files.js` | `.noe/lib/files/files.md` |
| Find files or directories by name | `.noe/lib/find/find.js` | `.noe/lib/find/find.md` |
| Search file content | `.noe/lib/search/search.js` | `.noe/lib/search/search.md` |

```js
// Find source files containing a term.
import {resolve} from "node:path";
const {grep} = await import(resolve(".noe/lib/search/search.js"));
export default await grep({pattern: "outputLimit", paths: ["internal/agent"], mode: "files"});
```

- If the location is unknown, discover it with `ls()`, `find()`, or `grep()` before dependent reads. Narrow paths or conditions when search results are incomplete.
- `read_file()` reads the whole file by default; use one-based `offset` and `limit` for excerpts. The content is in `text`.
- `write_file()` overwrites the whole file. For partial edits, use `edit_file(path, oldText, newText)`, which requires exactly one literal match by default. Use `{all: true}` only when every match should change.
- `find()` matches a glob against the entire basename; use `*term*` for a substring. `grep()` uses regular expressions by default; use `regex: false` for literal text and `mode: "files"` when only paths are needed. Use external rg or grep when the library lacks a required capability.

## Web search

Prefer `.noe/lib/exa/exa.js` for web search. See `.noe/lib/exa/exa.md` for the API.

```js
// Search before fetching selected pages.
import {resolve} from "node:path";
const {search, getContents} = await import(resolve(".noe/lib/exa/exa.js"));
export default await search("Go microVM library");
```

`search(query)` returns titles, URLs, and highlights; use `{mode: "links"}` for titles and URLs only. Select relevant URLs before calling `getContents(url)` for page text.
