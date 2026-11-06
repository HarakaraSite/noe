# Modules and import maps

Import JavaScript, verified ESM libraries, and JSON/text/bytes data by explicit
path, URL, or mapped name. HTTP/HTTPS imports are available through the CLI;
Go hosts enable them explicitly. See [URL imports](url-import.md).
The data-import and prefix/scope additions described here are not released yet.

```sh
./noe run --import-map examples/import-map/import-map.json examples/import-map/main.js
./noe eval --import-map examples/import-map/import-map.json 'import {sum} from "app/math"; console.log(sum([1,2,3]))'
```

`--import-map path` and `--import-map=path` must precede the script or eval
source. Arguments after the script name or `--` remain application arguments.
There is no automatic map discovery or environment-variable setting.
The map filename is relative to the startup cwd; relative targets inside it
are relative to **the map's directory**. Dependencies' relative imports use
**the importing module's location**.

## Exact names, prefixes, and scopes

For a map at `project/config/import-map.json`:

```json
{
  "imports": {
    "app/math": "../lib/math.mjs",
    "app/": "../lib/",
    "settings": "../data/default.json"
  },
  "scopes": {
    "../lib/special/": {
      "settings": "../data/special.json"
    }
  }
}
```

`app/math` matches the exact rule. `app/tools/format.mjs` uses `app/` and
loads `project/lib/tools/format.mjs`. Prefix keys and targets must end in `/` (native Windows path targets also
accept a trailing `\`); exact rules win, then the longest matching prefix. A matching prefix cannot
backtrack above its target directory. Targets are resolved once, without
following other aliases.

The scope changes `settings` for modules inside `project/lib/special/`.
Scopes ending in `/` match that directory and its descendants; other scopes
match only the exact referrer. Try the most specific matching scope, then its
matching outer scopes, then top-level `imports`. A scope's relative targets
are still relative to the map's directory. HTTP/HTTPS scopes match canonical
referrer URLs, including a redirect's final URL. Local file URLs and native
absolute paths are also supported. Virtual entries such as eval/run_js use
the synthetic `<virtual-entry>.mjs` path under their execution cwd for scope
matching, while retaining their original diagnostic names.

Map keys are bare names, including package-like names such as `@app/tools/`.
Path/URL keys, `integrity`, null blockers, map merging, and npm/package.json
resolution are unsupported. Parsing remains strict: duplicate JSON keys,
normalized duplicate scopes, invalid target types, and unknown fields are
errors. Built-in `node:`/`noe:` names and exact standard-library bare names
are reserved; a prefix cannot replace a reserved built-in either. Unused
files and URLs are not read during map validation.

This is a supported subset of the [HTML import-map model](https://html.spec.whatwg.org/multipage/webappapis.html#import-maps),
with native filesystem paths and strict configuration errors.

## JSON, text, and bytes

Data uses explicit import attributes with either static or dynamic
imports. Each data module has one `default` export.

```js
import settings from "settings" with { type: "json" };
import template from "./templates/email" with { type: "text" };
import payload from "./payload.bin" with { type: "bytes" };

const { default: other } = await import("./other.json", {
  with: { type: "json" }
});
```

JSON uses the engine's JavaScript JSON semantics, including mutable object
values. Text exports a JavaScript string. Bytes exports a `Uint8Array` over
an immutable ArrayBuffer; copy it before modifying it. Local source bytes and HTTP response-body bytes are passed intact, including
a BOM or bytes that are not valid UTF-8. The engine
interprets the source according to its requested type.

Local filenames no longer require `.js` or `.mjs`. A plain import still means
JavaScript; a `.json` suffix alone does not select JSON. There is no extension
completion, directory-index lookup, CommonJS loader, or TypeScript/JSX
conversion. Native Windows drive/UNC paths retain their existing behavior;
drive-relative paths are unsupported. Local `file:` URLs allow an empty or
`localhost` authority, without queries/fragments. Literal `#` and `%` remain
part of native filenames and must be encoded in file URLs.

HTTP uses the same raw-source model. Content-Type, charset, and filename do
not select or restrict the import type; even missing or malformed MIME
metadata does not reject an otherwise valid HTTP response. Standard HTTP
transport decompression still applies, so bytes are the bytes returned by
`http.Response.Body`, rather than the compressed wire representation.
No charset conversion, UTF-8 validation, or BOM removal is performed by the
loader. Text retains a leading BOM; BOM-prefixed JSON is rejected by the
current engine, just as with local files. A plain import always asks for
JavaScript, including responses labelled as JSON, HTML, or binary data.
See the [HTTP compatibility details](url-import.md#http-source-interpretation)
and [implementation record](development/stdlib-module-loading.md).

## Workers and agents

The immutable map, including prefixes and scopes, is captured at startup and
passed to Workers, agent task bootstrap, each run_js, and child agents.
Editing or deleting the original map does not change an active execution.
Tell an agent the mapped names and library APIs it should use; the complete
map is not sent to its provider.

Worker dependency imports use the map. Standard Worker entries remain local;
HTTP URLs and aliases targeting them are rejected before an entry request.
Global Worker keeps its existing local alias route; node Worker keeps its
existing string-path validation. Worker entries execute JavaScript, while
imports inside them can use data attributes. `noe:agent`, `noe:decision`, and
`noe:auth` remain unavailable inside Workers. Arbitrary spawned programs do
not automatically inherit CLI options.

## Source lifetime and compatibility

Each runtime reads a resource on its first successful load and retains that
raw source. Aliases resolving to the same canonical ID and import type share
a namespace and value; the same resource imported with a different type has
a different namespace. JavaScript side effects execute once per module.
Failed file reads can be retried. Root, Worker, and agent runtimes have fresh
source caches; restart the runtime to observe later file changes.

An Execute in-memory entry is retained. Worker entry self-import through a
new alias after deleting the entry can still fail; imported Worker dependencies
are retained normally. Local IDs are lexical absolute paths, without unifying
symlinks, hardlinks, or filesystem case aliases. Remote IDs use the final
canonical URL under the [URL identity rules](url-import.md).

Ordinary valid JavaScript imports need no source migration. HTTP loading
now accepts all MIME metadata and preserves response-body bytes; see the
[HTTP behavior changes](url-import.md#http-source-interpretation). Code relying on an
extension-validation failure now reaches filesystem loading and engine
parsing. Go hosts can use `LoaderOptions.Scopes`; external unkeyed
`LoaderOptions` literals must use field names after the new field is added.
The [Go embedding guide](go-embedding.md) describes capability selection.

[API](api.md) · [Phase 7 implementation](development/stdlib-module-loading.md) · [Original implementation record](development/local-import-map-implementation.md)
