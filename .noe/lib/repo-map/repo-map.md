# Repository map

`repo-map.js` provides `repoMap(options?)`, returning the directory and file layout with file counts under each directory. It reads no file content and assumes no particular language, framework, or repository structure. It runs in NOE and is independent of the `search.js` content-search library.

```js
import {resolve} from "node:path";
const {repoMap} = await import(resolve(".noe/lib/repo-map/repo-map.js"));
export default await repoMap();
```

Start with a shallow view, then expand the directories needed.

```js
export default await repoMap({path: "src", depth: 3});
```

| Argument | Default | Meaning |
| --- | --- | --- |
| `path` | `"."` | Root directory to inspect, relative to cwd |
| `depth` | `2` | Display depth; root is 0 and immediate children are 1; 0 returns only root totals |
| `maxEntries` | `120` | Maximum total displayed files and directories, excluding root |
| `maxBytes` | `8000` | UTF-8 byte limit for the entire returned JSON, including metadata |
| `maxFiles` | `50000` | Maximum number of files to count; exceeding it stops traversal |
| `backend` | `"auto"` | Normally automatic; can be fixed to `"git"` or `"fs"` |

Returns `{root, backend, files, directories, scanComplete, complete, limitedBy, tree, warnings}`. For example, `tree` can contain:

```text
. (19 files)
├─ src/ (12 files; collapsed)
├─ tests/ (6 files; collapsed)
└─ README.md
```

- `files` and each directory's file count describe all files counted beneath that location by the backend, not the number of displayed lines. `directories` counts scanned directories excluding root.
- Branches folded at `depth` are marked `collapsed`. When everything through the requested depth fits, `complete: true` is returned.
- Display candidates are selected from shallow levels first so a large branch does not hide other top-level directories. Directories appear first, with entries sorted by name within each category.
- Omission at `maxEntries` or `maxBytes` returns `complete: false` and lists the applicable limits in the `limitedBy` array. A branch's `+N entries` counts its scanned immediate children that are not displayed. The result is complete JSON.
- Stopping traversal at `maxFiles` also sets `scanComplete: false`. File counts are then lower bounds, displayed in the tree as `N+ files`. If several limits are reached, all appear in `limitedBy`.
- A maxBytes value too small even for root metadata is an error. When returning several maps, keep their combined size within run_js's output budget.
- Newlines and similar characters in filenames are escaped rather than inserted into tree lines. Symbolic links are marked `@` and not followed. Git submodules are shown as `gitlink` boundaries; their contents are not counted.

## File selection and fallback

Inside a Git repository, `git ls-files` supplies tracked and non-ignored untracked files. Hidden files are included. Tracked files deleted from the working tree are excluded. Nothing outside the requested `path` is displayed. Empty directories not managed by Git are not shown.

If Git is unavailable or the target is outside a Git repository, the library falls back to filesystem traversal. This ignores ignore files and excludes only `.git` directories. The difference is explicit in `backend: "fs"` and `warnings`. Filesystem traversal includes empty directories. Other command or read errors throw.

Usage: inspect the map, expand a selected `path`, then search content with `grep()` from `search.js`.

Validation: `noe run .noe/lib/repo-map/test.js`. Normal traversal works without Git, but validation requires Git.
