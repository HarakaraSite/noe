# Exa — search and fetch content from selected URLs

An external JavaScript library using only NOE's standard APIs; no npm or SDK is needed.
Search to narrow candidates, then fetch content only from the pages needed.

```js
import {resolve} from "node:path";
const {search, getContents} = await import(resolve(".noe/lib/exa/exa.js"));
export default await search("Go microVM library");
```

Paths are relative to the starting cwd (usually the workspace). No import map is needed.
In `run_js`, construct an absolute import path as shown above.

## Returned content and limits

| Call | Input | Returned content | Default count | Maximum count |
| --- | --- | --- | ---: | ---: |
| `search(query)` | Search string | Titles, URLs, and highlights | 5 | 20 |
| `search(query, {mode: "links"})` | Search string | Titles and URLs only | 5 | 50 |
| `getContents(url)` | One selected URL | Title, URL, and page text | 1 | 1 |

The library asks Exa for 1,000 highlight characters per page and up to 20,000 characters of page text.
The highlight character setting is not a strict return limit; observed excerpts can exceed it.
The library does not apply additional truncation.
Page text is **Markdown without HTML tags**, requested with `includeHtmlTags: false`. Exa's `text` is returned directly,
without custom Markdown conversion or flattening. Treat it as a string rather than relying on well-formed Markdown.
The character limit means the entire page is not guaranteed to be returned.

Limits apply to each library call. Multiple queries add their result counts and content sizes together.
The entire returned JSON is also subject to `run_js`'s shared output budget (default 64 KiB, maximum 256 KiB).
Character counts and UTF-8 byte counts are different.

## search(query, options?)

Makes one `POST https://api.exa.ai/search` request. The default mode is `highlights`.

```js
export default await search("Go microVM library", {numResults: 10});
```

```js
export default await search("Go microVM library", {mode: "links", numResults: 50});
```

- `query`: required nonempty string. The first argument takes precedence over `options.query`.
- `mode`: `"highlights"` (default) or `"links"`.
- `numResults`: default 5; an integer from 1–20 for highlights or 1–50 for links.
- Search filters forwarded to Exa: `type`, `category`, `includeDomains`, `excludeDomains`,
  `startPublishedDate`, `endPublishedDate`, `startCrawlDate`, `endCrawlDate`,
  `includeText`, `excludeText`, and `userLocation`.
- Highlight freshness options: `maxAgeHours` and `livecrawlTimeout`.
  They are sent inside `contents`. They cannot be supplied in links mode, which requests no page content.
- `apiKeyFile`: optional key-file path; see below.

Returns `{mode, results, requestId?, resolvedSearchType?, searchTime?, costDollars?, statuses?}`.
Each result is `{title, url, highlights}`, or `{title, url}` in links mode.
Exa may return fewer results than requested.

**The old format that directly specifies `contents` is rejected.** Select a mode instead of mixing
page text, summaries, or subpages into search. Excessive counts, invalid modes, and unsupported options
fail before the API key is read or any HTTP request is made.

Replace the old `search(query, {contents: {highlights: true}})` with `search(query)`.
Replace `search(query, {contents: {text: true}})` by searching for candidate URLs, selecting one, and fetching its content separately.

## getContents(url, options?)

Sends exactly one URL to `POST https://api.exa.ai/contents`.
Arrays and crawling additional pages are not accepted. Select the required URL from search results.

```js
export default await getContents("https://example.com/report", {maxCharacters: 10000});
```

- `url`: one HTTP(S) URL string.
- `maxCharacters`: default 20,000; an integer from 1–20,000. Smaller ranges are allowed.
- `maxAgeHours` and `livecrawlTimeout`: Exa freshness and crawl-wait settings; Exa defaults apply when omitted.
- `apiKeyFile`: optional key-file path.

The return shape matches search, with `mode: "text"` and at most one
`{title, url, text}` entry in results. If `statuses` is present, also check each URL's success or failure.
Additional options for HTML tags, summaries, highlights, or subpages are not accepted.

## Authentication and errors

The default key file is `~/.config/noe/credentials/exa-api-key`.
Override it with `apiKeyFile: "/path/to/key"`; a leading `~/` is expanded.
The file content is trimmed and sent in the `x-api-key` header.
Do not store the key in source files or the repository.

Importing the library neither reads the key nor makes a request. There is no library-level automatic retry
or cache; Exa's own cache is separate.

Invalid arguments throw `TypeError` or `RangeError`. Key-read, transport, JSON-parse, and HTTP failures throw
`ExaSearchError`. HTTP errors retain `status`, `requestId`, and `body`, plus `cause` when there is an underlying failure.
API key values are excluded from exception messages.

## Validation and official references

`noe run .noe/lib/exa/test.js` checks requests, limits, return shapes, and errors.
HTTP is substituted, so no real API key, network request, or API charge is needed.

- [Exa Search](https://exa.ai/docs/reference/search)
- [Exa Contents](https://exa.ai/docs/reference/get-contents)
- [Page text is Markdown; highlights are relevant excerpts](https://exa.ai/docs/contents/quickstart)
