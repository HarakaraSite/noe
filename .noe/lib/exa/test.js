// Run with NOE. All HTTP requests use a stub; no real key or paid API is used.
import {mkdtempSync, writeFileSync, rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join} from "node:path";
import {search, getContents} from "./exa.js";

const root = mkdtempSync(join(tmpdir(), "noe-exa-"));
const keyFile = join(root, "fake-key");
writeFileSync(keyFile, "  test-key-not-real\n");
const originalFetch = globalThis.fetch;
const requests = [];
let checks = 0;
function check(ok, label) {
  if (!ok) throw new Error(label);
  checks++;
}
async function rejects(operation, expectedName, label) {
  let error;
  try { await operation(); } catch (caught) { error = caught; }
  check(error?.name === expectedName, label);
  return error;
}
function reply(_url, body) {
  return new Response(JSON.stringify({
    requestId: "stub-request", resolvedSearchType: "auto", searchTime: 10,
    costDollars: {total: 0},
    results: Array.from({length: body.numResults ?? 1}, (_, i) => ({
      title: `Page ${i}`, url: body.urls?.[0] ?? `https://example.com/${i}`,
      highlights: ["Relevant excerpt."], text: "# Title\n\nBody with [link](https://example.com).\n",
      author: "Extra metadata", subpages: [{text: "Unrequested content"}],
    })),
    statuses: [{id: "https://example.com/page", status: "success"}],
    context: "Unrequested combined text",
  }), {status: 200});
}
let respond = reply;
globalThis.fetch = async (url, init) => {
  check(init.method === "POST" && init.headers["x-api-key"] === "test-key-not-real", "POST with trimmed test key");
  const body = JSON.parse(init.body);
  requests.push({url, body});
  return respond(url, body);
};
try {
  const highlights = await search("microVM Go", {apiKeyFile: keyFile});
  let sent = requests.at(-1);
  check(sent.url === "https://api.exa.ai/search" && sent.body.numResults === 5 && sent.body.query === "microVM Go", "default query and five results");
  check(sent.body.contents.highlights.maxCharacters === 1000 && !sent.body.contents.text && !sent.body.mode && !sent.body.apiKeyFile, "only bounded highlights requested");
  check(highlights.mode === "highlights" && highlights.results.length === 5 && highlights.results[0].highlights[0] === "Relevant excerpt." && !("text" in highlights.results[0]) && !("context" in highlights), "highlight result omits full text and combined context");
  check(highlights.requestId === "stub-request" && highlights.costDollars.total === 0 && highlights.searchTime === 10, "request and cost metadata preserved");

  const links = await search("original query", {mode: "links", numResults: 50, apiKeyFile: keyFile, query: "ignored", includeDomains: ["example.com"], type: "auto"});
  sent = requests.at(-1);
  check(sent.body.numResults === 50 && !Object.hasOwn(sent.body, "contents") && sent.body.query === "original query" && sent.body.includeDomains[0] === "example.com", "links request has no content extraction and keeps filters");
  check(links.results.length === 50 && Object.keys(links.results[0]).sort().join(",") === "title,url", "links return only title and URL");
  await search("query", {numResults: 20, apiKeyFile: keyFile, maxAgeHours: 0});
  check(requests.at(-1).body.numResults === 20 && requests.at(-1).body.contents.maxAgeHours === 0, "highlight ceiling and nested freshness");

  const page = await getContents("https://example.com/page", {apiKeyFile: keyFile});
  sent = requests.at(-1);
  check(sent.url === "https://api.exa.ai/contents" && sent.body.urls.length === 1 && sent.body.urls[0] === "https://example.com/page", "one selected URL uses Contents endpoint");
  check(sent.body.text.includeHtmlTags === false && sent.body.text.maxCharacters === 20000 && !sent.body.highlights && !sent.body.subpages, "bounded tag-free Markdown requested without extra views or crawling");
  check(page.mode === "text" && page.results[0].text === "# Title\n\nBody with [link](https://example.com).\n" && !("highlights" in page.results[0]) && page.statuses[0].status === "success", "Markdown is passed through unchanged and URL statuses retained");
  await getContents("https://example.com/page", {maxCharacters: 1234, apiKeyFile: keyFile});
  check(requests.at(-1).body.text.maxCharacters === 1234, "smaller page budget");

  const before = requests.length;
  for (const [options, name] of [
    [{numResults: 21}, "RangeError"], [{mode: "links", numResults: 51}, "RangeError"],
    [{numResults: 0}, "RangeError"], [{numResults: 1.5}, "RangeError"],
    [{mode: "text"}, "TypeError"], [{contents: {text: true}}, "TypeError"],
    [{contents: {highlights: true}}, "TypeError"], [{subpages: 100}, "TypeError"],
    [{stream: true}, "TypeError"], [{outputSchema: {}}, "TypeError"],
    [{summary: true}, "TypeError"],
  ]) {
    await rejects(() => search("query", {...options, apiKeyFile: join(root, "absent-key")}), name, "invalid search rejected before key or HTTP");
  }
  await rejects(() => getContents(["https://example.com/a", "https://example.com/b"]), "TypeError", "multiple URLs rejected before key or HTTP");
  await rejects(() => getContents("file:///tmp/page"), "TypeError", "non-HTTP URL rejected");
  await rejects(() => getContents("https://example.com", {maxCharacters: 20001}), "RangeError", "page length ceiling");
  await rejects(() => getContents("https://example.com", {subpages: 100}), "TypeError", "subpage crawling rejected");
  await rejects(() => getContents("https://example.com", {includeHtmlTags: true}), "TypeError", "tag output cannot override Markdown mode");
  check(requests.length === before, "rejected requests never reached HTTP");

  respond = () => new Response(JSON.stringify({error: "rate limited", requestId: "error-id"}), {status: 429});
  const http = await rejects(() => getContents("https://example.com", {apiKeyFile: keyFile}), "ExaSearchError", "HTTP error remains structured");
  check(http.status === 429 && http.requestId === "error-id" && http.body.error === "rate limited", "HTTP diagnostics preserved");
  respond = () => { throw new Error("network unavailable"); };
  const network = await rejects(() => search("query", {apiKeyFile: keyFile}), "ExaSearchError", "network error wrapped");
  check(network.cause.message === "network unavailable", "network cause preserved");
  respond = () => new Response("not JSON", {status: 200});
  await rejects(() => search("query", {apiKeyFile: keyFile}), "ExaSearchError", "invalid JSON wrapped");
  const priorMissingKey = requests.length;
  await rejects(() => search("query", {apiKeyFile: join(root, "absent-key")}), "ExaSearchError", "missing key keeps existing error type");
  check(requests.length === priorMissingKey, "missing key never reached HTTP");
  console.log(`PASS: ${checks} checks; no real API requests`);
} finally {
  globalThis.fetch = originalFetch;
  rmSync(root, {recursive: true, force: true});
}
