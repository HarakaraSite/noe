package stdlib

// responseJS shares the Fetch closure's Body, Headers and URL implementation.
// Internal responses use an unexported token rather than weakening the public
// constructor for network errors, native metadata or WebSocket upgrades.
const responseJS = `
  const responseSlots = new WeakMap(), responseToken = {};
  const responseStringify = JSON.stringify;
  const responseTrunc = Math.trunc;
  function responseOf(value) {
    const state = requestApply(requestGet, responseSlots, [value]);
    if (state === undefined) throw new TypeError("The receiver is not a Response");
    return state;
  }
  function responseShort(value) {
    const number = +value;
    if (!Number.isFinite(number) || number === 0) return 0;
    return ((responseTrunc(number) % 65536) + 65536) % 65536;
  }
  function responseByteString(value) {
    const text = requestDOMString(value);
    for (let i = 0; i < text.length; i++) if (text.charCodeAt(i) > 255) throw new TypeError("Response statusText must be a ByteString");
    return text;
  }
  function responseInit(value) {
    const result = {headers: undefined, status: 200, statusText: ""};
    if (value === undefined || value === null) return result;
    if (typeof value !== "object" && typeof value !== "function") throw new TypeError("Response init must be an object");
    // Web IDL dictionaries read members in lexicographic order. Convert the
    // entire HeadersInit here, but validate its HTTP fields during fill below.
    const headers = value.headers;
    if (headers !== undefined) result.headers = headerInit(headers);
    const status = value.status;
    if (status !== undefined) result.status = responseShort(status);
    const statusText = value.statusText;
    if (statusText !== undefined) result.statusText = responseByteString(statusText);
    return result;
  }
  function responseBody(value) {
    if (value === null || value === undefined) return null;
    if (isStream(value) || value instanceof ArrayBuffer || ArrayBuffer.isView(value) ||
        (typeof Blob !== "undefined" && value instanceof Blob) ||
        (typeof FormData !== "undefined" && value instanceof FormData) ||
        (typeof URLSearchParams !== "undefined" && value instanceof URLSearchParams)) return value;
    return requestUSVString(value);
  }
  function nullBodyStatus(status) { return [101, 103, 204, 205, 304].includes(status); }
  function responseState(body, init, json = false) {
    const inferred = new Headers();
    if (isStream(body) && (body.locked || body._disturbed)) throw new TypeError("Response body must be unused and unlocked");
    let source = encodeBody(body, inferred);
    // Extracting a BufferSource takes a snapshot at construction time.
    if (source !== null && !isStream(source)) source = source.slice();
    if (init.status < 200 || init.status > 599) throw new RangeError("Response status must be between 200 and 599");
    if (/[^\t\x20-\x7e\x80-\xff]/.test(init.statusText)) throw new TypeError("Invalid Response statusText");
    const headers = new Headers(init.headers);
    if (source !== null && nullBodyStatus(init.status)) throw new TypeError("This Response status cannot have a body");
    const contentType = json ? "application/json" : inferred.get("content-type");
    if (contentType !== null && !headers.has("content-type")) headers.set("content-type", contentType);
    return {source, state: {status: init.status, statusText: init.statusText, headers,
      type: "default", url: "", redirected: false, immutable: false}};
  }
  function makeResponse(source, state) { return new Response(source, state, responseToken); }
  function nativeResponse(source, raw) {
    return makeResponse(source, {status: raw.status, statusText: raw.statusText,
      headers: new Headers(raw.headers), type: "basic", url: raw.url.split("#", 1)[0],
      redirected: !!raw.redirected, immutable: true});
  }
  function responseSnapshot(value) {
    const state = responseOf(value);
    if (state.status < 200) throw new TypeError("This Response cannot be sent as an HTTP reply");
    return {status: state.status, headers: headerPairsForIteration(state.headers)};
  }
  class Response extends Body {
    constructor(body = null, init = undefined, token = undefined) {
      let source, state;
      if (token === responseToken) { source = body; state = init; }
      else {
        const convertedBody = responseBody(body);
        ({source, state} = responseState(convertedBody, responseInit(init)));
      }
      super(source, state.headers);
      requestApply(requestSet, responseSlots, [this, state]);
      if (state.immutable) headerImmutable(state.headers);
    }
    get status() { return responseOf(this).status; }
    get statusText() { return responseOf(this).statusText; }
    get url() { return responseOf(this).url; }
    get redirected() { return responseOf(this).redirected; }
    get type() { return responseOf(this).type; }
    get headers() { return responseOf(this).headers; }
    get ok() { const status = responseOf(this).status; return status >= 200 && status < 300; }
    clone() {
      const state = responseOf(this);
      return makeResponse(cloneBody(this), {...state, headers: new Headers(headerSnapshot(state.headers))});
    }
    static json(data, init = undefined) {
      if (arguments.length === 0) throw new TypeError("Response.json requires data");
      const options = responseInit(init);
      const text = requestApply(responseStringify, JSON, [data]);
      if (text === undefined) throw new TypeError("The value cannot be serialized to JSON");
      const {source, state} = responseState(new TextEncoder().encode(text), options, true);
      return makeResponse(source, state);
    }
    static error() {
      return makeResponse(null, {status: 0, statusText: "", headers: new Headers(),
        type: "error", url: "", redirected: false, immutable: true});
    }
    static redirect(url, status = 302) {
      if (arguments.length === 0) throw new TypeError("Response.redirect requires a URL");
      const text = requestUSVString(url), code = responseShort(status);
      captureRequestURL();
      if (requestURL === null) throw new TypeError("Response.redirect requires the Web URL API");
      const parsed = new requestURL(text);
      if (![301, 302, 303, 307, 308].includes(code)) throw new RangeError("Invalid redirect status");
      const location = requestApply(requestURLGetters.href, parsed, []);
      return makeResponse(null, {status: code, statusText: "", headers: new Headers({location}),
        type: "default", url: "", redirected: false, immutable: true});
    }
  }
  for (const name of ["status", "statusText", "url", "redirected", "type", "headers", "ok", "clone"]) Object.defineProperty(Response.prototype, name, {enumerable: true});
  for (const name of ["json", "error", "redirect"]) Object.defineProperty(Response, name, {enumerable: true});
  Object.defineProperty(Response.prototype, Symbol.toStringTag, {value: "Response", configurable: true});
`
