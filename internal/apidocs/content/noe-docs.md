# `noe:docs`

### API information

Public exports: `docs`.

Reference: `noe:docs`. Owner: noestdlib.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source distribution. Signatures describe supported APIs, not which bindings this runtime has installed. Detailed member headings below are documentation entries, not an export inventory.

Availability: Installed by stdlib.Profile in main runtimes and Workers, independently of selected host capabilities.

Public-definition source: internal/noestdlib/noestdlib.go. Reading this reference evaluates no target module and makes no provider request.

`noe:docs` exposes this binary's embedded Markdown reference to JavaScript. It performs no external I/O, provider request, or persistent work. The same content is used by the `noe docs` CLI command.

### Availability

The module is available in the main runtime and Workers. Topic aliases resolve to the same reference where an alias is listed. Returned member IDs identify sections that can be requested independently. A topic overview includes the shared summary and member list; a member lookup includes the shared summary and only the selected detail.

## docs

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
