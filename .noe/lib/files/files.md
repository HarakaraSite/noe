# Listing, reading, and editing files

`files.js` is an external JavaScript library for NOE. Use `ls()` to inspect layout, `read_file()` to read content, and `write_file()` or `edit_file()` to create or edit files. Use [find()](../find/find.md) to find entries by name and [grep()](../search/search.md) to search file content.

```js
import {resolve} from "node:path";
const {ls, read_file, write_file, edit_file} = await import(resolve(".noe/lib/files/files.js"));
export default await ls();
```

## ls(path?, options?)

`path` is relative to cwd and defaults to `"."` (usually the workspace). By default, only immediate entries are returned, sorted by name.

```js
export default await ls("internal", {tree: true, depth: 2});
// For the next page, pass nextOffset as offset with the same path, tree, and depth.
export default await ls("internal", {tree: true, depth: 2, offset: 200, limit: 200});
```

| Option | Default | Meaning |
| --- | --- | --- |
| `tree` | `false` | Also traverse subdirectories |
| `depth` | `2` | Traversal depth in tree mode; immediate children are at depth 1 |
| `offset` | `0` | Number of entries to skip |
| `limit` | `200` | Number of entries to return |

Returns `{path, entries, total, nextOffset?}`. `path` is the absolute root path. Each entry is `{path, type, bytes, depth}`: its path is relative to the root, type is `file`, `directory`, `symlink`, or `other`, and bytes is its lstat size. A directory's bytes are not the total size of its contents.

Tree mode sorts entries by name and places each directory's children directly after it. Hidden entries, Git-ignored entries, and empty directories are included; symbolic links are not followed. `total` counts entries through the requested depth. Pagination still scans to that depth, so narrow the path for large trees. Read errors throw. If files change during traversal, page boundaries can shift.

## read_file(path, options?)

Returns **the entire UTF-8 text by default**, preserving LF/CRLF line endings and whether the file ends with a newline.

```js
export default await read_file("README.md");
export default await read_file("README.md", {offset: 101, limit: 80});
```

`offset` is a one-based line number (default 1); `limit` is the maximum number of lines (omitting it reads to EOF). Returns `{path, text, offset, lines, totalLines, nextOffset?}`. The path is absolute, lines is the number returned, and nextOffset is present only when more lines remain. An empty file has 0 lines; a trailing newline does not add an extra empty line. An offset beyond EOF returns empty text.

The library imposes no byte limit of its own. If the combined run_js result exceeds the default 64 KiB, select line ranges or increase the budget for a necessary larger call with `// @noe: {"outputBytes": 131072}` in its initial comment block. The allowed range is 1024–262144 bytes. This budget also controls stdout/stderr capture and applies to the entire model-facing result, including metadata and JSON escaping. This function reads an existing file; it cannot recover previously omitted stdout.

## write_file(path, text)

Creates or overwrites a UTF-8 text file, recursively creating parent directories. Returns `{path, bytes}` with the absolute path and the number of UTF-8 bytes written. Text must be a string; an empty string creates an empty file.

```js
export default await write_file("notes/result.txt", "Investigation results\n");
```

## edit_file(path, oldText, newText, options?)

Replaces a literal string in an existing file. Supply only the relevant surrounding text instead of regenerating the whole file. This is neither regular-expression replacement nor a unified-diff parser.

```js
export default await edit_file("app.js", "const limit = 10;", "const limit = 20;");
// Replace all occurrences only when that is intended.
export default await edit_file("app.js", "oldName", "newName", {all: true});
```

By default, it writes only when there is **exactly one match**. Zero or multiple matches throw without modifying the file. For multiple matches, include more context in oldText to identify one occurrence, or use `all: true` to replace all. oldText cannot be empty. An empty newText deletes the matched text. Replacement strings such as `$&` are written literally.

Use the file's actual line endings (LF or CRLF) in oldText. Content, line endings, and the presence of a trailing newline outside the replacement are preserved. A missing target file throws. Returns `{path, replacements, bytes}` with the absolute path, number of non-overlapping replacements, and final UTF-8 byte size. It reads the current file before writing but does not coordinate concurrent edits by other operations.

Validation: `noe run .noe/lib/files/test.js`.
