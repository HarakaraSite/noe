// Small Exa search client for NOE (go-quickjs runtime, no npm dependencies).
// Public API: search(query, options), getContents(url, options).
// The API key is read from disk only when a request runs; importing this module
// performs no file reads and no network requests.

import { readFile } from "fs/promises";
import { homedir } from "os";
import { join } from "path";

const EXA_API_URL = "https://api.exa.ai";
const DEFAULT_API_KEY_PARTS = [".config", "noe", "credentials", "exa-api-key"];
const HIGHLIGHT_CHARACTERS = 1000;
const TEXT_CHARACTERS = 20000;
const SEARCH_OPTIONS = new Set([
  "query", "type", "category", "includeDomains", "excludeDomains",
  "startPublishedDate", "endPublishedDate", "startCrawlDate", "endCrawlDate",
  "includeText", "excludeText", "userLocation", "maxAgeHours", "livecrawlTimeout",
]);

function checkOptions(options) {
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("exa: options must be an object");
  }
}

function integer(value, maximum, name) {
  if (!Number.isSafeInteger(value) || value < 1 || value > maximum) {
    throw new RangeError(`exa: ${name} must be an integer from 1 to ${maximum}`);
  }
}

function checkKeys(options, allowed) {
  for (const key of Object.keys(options)) {
    if (!allowed.has(key)) throw new TypeError(`exa: unsupported option ${key}; use search() mode "links"/"highlights", or getContents(url) for one page`);
  }
}

function projectResponse(response, mode, maximum) {
  if (!response || !Array.isArray(response.results)) {
    throw searchError("exa: response is missing the results array");
  }
  const result = {mode, results: response.results.slice(0, maximum).map(item => {
    const row = {title: item.title, url: item.url};
    if (mode === "highlights") row.highlights = item.highlights ?? [];
    if (mode === "text") row.text = item.text ?? "";
    return row;
  })};
  for (const key of ["requestId", "resolvedSearchType", "searchTime", "costDollars", "statuses"]) {
    if (response[key] !== undefined) result[key] = response[key];
  }
  return result;
}

// Build the default key path from os/path, because "~" is not expanded by the
// filesystem APIs.
function defaultApiKeyFile() {
  const home = homedir();
  if (home === "") {
    throw searchError(
      "exa: cannot resolve the home directory for the default API key file; pass options.apiKeyFile",
    );
  }
  return join(home, ...DEFAULT_API_KEY_PARTS);
}

// options.apiKeyFile is library-specific. A leading "~/" is expanded with
// homedir() so callers do not have to rely on shell expansion.
function resolveApiKeyFile(apiKeyFile) {
  if (apiKeyFile === undefined) {
    return defaultApiKeyFile();
  }
  if (typeof apiKeyFile !== "string" || apiKeyFile === "") {
    throw new TypeError("exa: options.apiKeyFile must be a non-empty string when provided");
  }
  if (apiKeyFile === "~") {
    return homedir();
  }
  if (apiKeyFile.startsWith("~/")) {
    const home = homedir();
    if (home === "") {
      throw searchError(
        "exa: cannot resolve the home directory for options.apiKeyFile; pass an absolute path",
      );
    }
    return join(home, apiKeyFile.slice(2));
  }
  return apiKeyFile;
}

function searchError(message, { status, requestId, body, cause } = {}) {
  const error = new Error(message);
  error.name = "ExaSearchError";
  if (status !== undefined) {
    error.status = status;
  }
  if (requestId !== undefined) {
    error.requestId = requestId;
  }
  if (body !== undefined) {
    error.body = body;
  }
  if (cause !== undefined) {
    error.cause = cause;
  }
  return error;
}

/**
 * Run an Exa search (POST https://api.exa.ai/search).
 *
 * @param {string} query - Required, must be a non-empty string.
 * @param {object} [options] - mode defaults to "highlights" (up to 20 pages,
 *   requests 1000 highlight characters per page; the service may exceed it).
 *   "links" returns only titles/URLs
 *   (up to 50). numResults defaults to 5. Content extraction is managed by
 *   the mode; raw contents options are not accepted. Common Exa search filters
 *   and options.apiKeyFile are supported; see exa.md.
 * @returns {Promise<object>} Selected result fields and request/cost metadata.
 * @throws {TypeError|RangeError} For an invalid query, mode or options.
 * @throws {Error} With name "ExaSearchError" when the key file cannot be read,
 *   the network request fails, or the HTTP status is not ok. HTTP errors also
 *   carry .status, .requestId and .body.
 */
export async function search(query, options = {}) {
  if (typeof query !== "string" || query.trim() === "") {
    throw new TypeError("exa: query must be a non-empty string");
  }
  checkOptions(options);
  const {apiKeyFile, mode = "highlights", numResults = 5, ...exaOptions} = options;
  if (mode !== "links" && mode !== "highlights") {
    throw new TypeError('exa: mode must be "links" or "highlights"; use getContents(url) for one page of text');
  }
  integer(numResults, mode === "links" ? 50 : 20, `numResults in ${mode} mode`);
  checkKeys(exaOptions, SEARCH_OPTIONS);
  // The first argument always wins, so options.query can never replace it.
  const {maxAgeHours, livecrawlTimeout, ...filters} = exaOptions;
  const body = {...filters, query, numResults};
  if (mode === "highlights") {
    body.contents = {highlights: {maxCharacters: HIGHLIGHT_CHARACTERS}};
    if (maxAgeHours !== undefined) body.contents.maxAgeHours = maxAgeHours;
    if (livecrawlTimeout !== undefined) body.contents.livecrawlTimeout = livecrawlTimeout;
  } else if (maxAgeHours !== undefined || livecrawlTimeout !== undefined) {
    throw new TypeError("exa: content freshness options require highlights mode");
  }
  return projectResponse(await request("search", body, apiKeyFile), mode, numResults);
}

/** Retrieve one URL as tag-free Markdown; no extra Markdown conversion. */
export async function getContents(url, options = {}) {
  if (typeof url !== "string" || url.trim() === "") {
    throw new TypeError("exa: getContents expects one HTTP(S) URL string, not an array");
  }
  const parsed = new URL(url);
  if (parsed.protocol !== "http:" && parsed.protocol !== "https:") {
    throw new TypeError("exa: getContents expects one HTTP(S) URL");
  }
  checkOptions(options);
  const {apiKeyFile, maxCharacters = TEXT_CHARACTERS, ...exaOptions} = options;
  checkKeys(exaOptions, new Set(["maxAgeHours", "livecrawlTimeout"]));
  integer(maxCharacters, TEXT_CHARACTERS, "maxCharacters");
  const body = {...exaOptions, urls: [url], text: {maxCharacters, includeHtmlTags: false}};
  return projectResponse(await request("contents", body, apiKeyFile), "text", 1);
}

async function request(endpoint, body, apiKeyFile) {
  const apiKeyPath = resolveApiKeyFile(apiKeyFile);
  let apiKey;
  try {
    apiKey = (await readFile(apiKeyPath, "utf8")).trim();
  } catch (cause) {
    throw searchError(`exa: cannot read API key file: ${apiKeyPath}`, { cause });
  }
  if (apiKey === "") {
    throw searchError(`exa: API key file is empty: ${apiKeyPath}`);
  }

  let response;
  try {
    response = await fetch(`${EXA_API_URL}/${endpoint}`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "x-api-key": apiKey,
      },
      body: JSON.stringify(body),
    });
  } catch (cause) {
    const detail = cause && cause.message ? cause.message : String(cause);
    throw searchError(`exa: ${endpoint} request failed: ${detail}`, { cause });
  }

  if (!response.ok) {
    let errorBody = null;
    let raw = "";
    try {
      raw = await response.text();
    } catch {
      // Keep the HTTP status as the diagnostic when the body cannot be read.
    }
    if (raw !== "") {
      try {
        errorBody = JSON.parse(raw);
      } catch {
        // The body was not JSON; the raw text is used as a fallback detail.
      }
    }
    const requestId =
      errorBody && typeof errorBody.requestId === "string"
        ? errorBody.requestId
        : response.headers.get("x-request-id") || null;
    const detail =
      (errorBody && (errorBody.error || errorBody.message)) ||
      raw.slice(0, 200) ||
      response.statusText ||
      "";
    throw searchError(
      `exa: ${endpoint} failed with HTTP ${response.status}${detail ? ` (${detail})` : ""}`,
      { status: response.status, requestId, body: errorBody },
    );
  }

  try {
    return await response.json();
  } catch (cause) {
    throw searchError(`exa: failed to parse search response JSON (HTTP ${response.status})`, {
      status: response.status,
      cause,
    });
  }
}
