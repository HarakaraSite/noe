# Tool usage preferences

These are editable preferences for using NOE's APIs. The embedded runtime facts and API reference describe the actual execution contracts.

## Code and results

Start each run_js call with a short English `//` comment describing the operation on the first line, before imports or executable statements, without a leading blank line.

Prefer `export default value` or `export default await operation()` for structured results. Use console output for text. Return the information needed for the next decision; for broad investigations, follow the staged narrowing guidance in development.md. For oversized searches, return filenames, counts, or relevant excerpts instead of all matches. When the required content itself is larger, use the documented outputBytes pragma.

After an execution or serialization failure, inspect the result and completed side effects before retrying. A failed result does not imply that earlier operations were undone.

## API discovery

When a signature, return shape, supported operation, or resource lifetime is unfamiliar, consult `noe:docs` before using it. Prefer the local version-matched reference over upstream latest documentation. A missing detailed member is not proof that an API is unsupported; the topic overview lists exports and available details.

```js
import {docs} from "noe:docs";
console.log(docs("node:child_process", "execFile"));
```

Use docs() for the topic index, docs(topic) for an overview, and docs(topic, member) for a known detail. The run_js recipes are results, files, commands, imports, http, agents, and failures. Select a known topic or recipe directly rather than listing the index again.

After an API error, inspect the actual return value or export shape and the relevant reference before another speculative rewrite.

## Concurrent work

When useful for the task and service limits, group independent asynchronous reads or requests whose results are needed together with Promise.all. Complete discovery before reads that depend on its results. Keep dependent writes ordered within a call or wait for one call to finish before starting the next dependent operation.

## Model-backed decisions

Consider `noe:decision` for classification, route or child selection, or evaluating artifacts against shared criteria, especially when results drive JavaScript branches or many inputs use the same rubric. Choose it when useful; deterministic checks can use JavaScript directly. Unless the task specifies otherwise, selecting and calling a decision model does not require the user to name that API or request delegation.

Use list_models from `noe:agent` when model candidates or selection metadata are needed. Missing metadata is unknown. Follow the user's explicit provider, model, and effort choices.
