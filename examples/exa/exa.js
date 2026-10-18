// Small Exa search client for NOE (go-quickjs runtime, no npm dependencies).
// Public API: search(query, options).
// The API key is read from disk only when search() runs; importing this module
// performs no file reads and no network requests.

import { readFile } from "fs/promises";
import { homedir } from "os";
import { join } from "path";

const EXA_SEARCH_URL = "https://api.exa.ai/search";
const DEFAULT_API_KEY_PARTS = [".config", "noe", "credentials", "exa-api-key"];

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
 * @param {object} [options] - Exa search parameters (numResults, type,
 *   contents, includeDomains, ...), sent as the request JSON body together
 *   with the query. options.apiKeyFile is library-specific and is removed
 *   from the body; when omitted, the key is read from
 *   ~/.config/noe/credentials/exa-api-key (surrounding whitespace trimmed).
 * @returns {Promise<object>} The parsed Exa response JSON, unmodified
 *   (requestId, results, costDollars, ... are all kept).
 * @throws {TypeError} For an invalid query or options.
 * @throws {Error} With name "ExaSearchError" when the key file cannot be read,
 *   the network request fails, or the HTTP status is not ok. HTTP errors also
 *   carry .status, .requestId and .body.
 */
export async function search(query, options = {}) {
  if (typeof query !== "string" || query.trim() === "") {
    throw new TypeError("exa: query must be a non-empty string");
  }
  if (options === null || typeof options !== "object" || Array.isArray(options)) {
    throw new TypeError("exa: options must be an object");
  }

  const { apiKeyFile, ...exaOptions } = options;
  // The first argument always wins, so options.query can never replace it.
  const body = { ...exaOptions, query };

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
    response = await fetch(EXA_SEARCH_URL, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
        "x-api-key": apiKey,
      },
      body: JSON.stringify(body),
    });
  } catch (cause) {
    const detail = cause && cause.message ? cause.message : String(cause);
    throw searchError(`exa: search request failed: ${detail}`, { cause });
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
      `exa: search failed with HTTP ${response.status}${detail ? ` (${detail})` : ""}`,
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
