# `noe:docs`

`noe:docs` exposes the embedded reference and runtime API discovery. `docs` reads immutable Markdown without external I/O. `describe` inspects module exports without evaluating module bodies; a configured loader can read source while linking. Neither function makes a model-provider request. The same Markdown content is used by `noe docs`.

### Availability

The module is available in the main runtime and Workers. Topic aliases resolve to the same reference where an alias is listed. Returned member IDs identify sections that can be requested independently. A topic overview includes the shared summary and member list; a member lookup includes the shared summary and only the selected detail.

## docs

Signature: `docs(topic?: string, member?: string) -> string`

```js
import { docs } from "noe:docs";
```

`docs(topic?, member?) -> string` synchronously returns Markdown from the embedded catalog.

- With no arguments, or an omitted/empty `topic`, it returns the topic index with kinds and summaries.
- With `topic`, it returns that topic's summary and its available detail member IDs.
- With `topic` and `member`, it returns the shared summary and that member's detail.
- `undefined` and an empty string are treated as omitted arguments. No more than two arguments are accepted.

`topic` and `member` must be strings when provided. A type error or excess argument throws `TypeError`. An unknown topic or member throws `Error` with `code === "ERR_DOC_NOT_FOUND"`. Aliases, such as a supported standard-library alias, resolve to the same content as their canonical topic.

```js
import { docs } from "noe:docs";

console.log(docs());
console.log(docs("noe:sockets"));
console.log(docs("noe:sockets", "connect"));
```

## cli

`noe docs [topic [member]]` writes the same Markdown reference to standard output. With no topic it prints the index; with a topic it prints its overview and member IDs; with a member it prints that detail. Invalid CLI arguments or unknown items are reported on standard error with exit code `2`. `noe docs --help` shows command syntax.

## describe

Signature: `describe(topic?: string, member?: string) -> object`

Use `describe()` for the reference catalog and currently registered module names.
The catalog includes modules outside the current Profile; registered names are
not a complete inventory of loader-provided source modules.

`describe("node:fs/promises")` returns `availability`, actual `exports` with
`name`, `documented`, and `signature`, plus the reference's `details` IDs.
Export names come from the engine's module shape, including undocumented
exports. A null signature means no explicit signature metadata has been
authored. Detailed reference headings are not an export inventory.

`describe(topic, member)` also returns a `member` object with `availability`,
`documented`, and `signature`. A missing detail does not make an export
unavailable. Unknown loader modules or unsuccessful inspection have unknown
availability; they are not reported as nonexistent. Inspecting an importable
module links its static dependencies but does not evaluate its body or read
export values. Custom loader code may run to obtain source.

```js
import {describe, docs} from "noe:docs";
export default describe("node:fs/promises", "readFile");
```

Read `docs(topic, member)` for explanations and lifetimes after discovering the
export. `noe:run_js` is a tool reference topic, not an importable module.
