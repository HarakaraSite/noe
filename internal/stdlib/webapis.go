package stdlib

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// WebAPIs installs the things a browser has that the language does not, as far
// as they are pure computation: URL and URLSearchParams, TextEncoder and
// TextDecoder, atob and btoa, structuredClone, performance, AbortController,
// Blob, File and FormData, URLPattern, DOMException, MessageChannel, and the
// parts of crypto that need nothing but entropy.
//
// None of them can reach outside the process. random is where getRandomValues
// draws from, and nil means the system source. A MessagePort's messages are
// dispatched by the runtime's Loop if it has one, and otherwise when it next
// runs its jobs; installed by Install, a port that is listening keeps the
// Loop running, as node's does, until it is closed or unref'd.
func WebAPIs(rt *quickjs.Runtime, random io.Reader) error {
	_, err := webAPIs(rt, random, nil)
	return err
}

// webAPIs is WebAPIs, with the loop a listening MessagePort keeps running. It
// returns the runtime's messaging, which its workers talk through.
func webAPIs(rt *quickjs.Runtime, random io.Reader, loop *Loop) (*messaging, error) {
	if random == nil {
		random = rand.Reader
	}
	webcryptoNative, err := newWebCryptoNative(rt, loop, random)
	if err != nil {
		return nil, err
	}
	started := time.Now()

	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"webcrypto": webcryptoNative,
		"now": func() float64 {
			return float64(time.Since(started).Nanoseconds()) / 1e6
		},
		"timeOrigin": float64(started.UnixNano()) / 1e6,
		"abortTimeout": func(callback quickjs.Value, milliseconds float64) error {
			return armAbortTimeout(rt, loop, callback, milliseconds)
		},
		"btoa": func(s string) (string, error) {
			// btoa works on bytes, spelled as a string of code units below 256.
			b := make([]byte, 0, len(s))
			for _, r := range s {
				if r > 0xFF {
					return "", errors.New(
						"the string contains a character outside the byte range")
				}
				b = append(b, byte(r))
			}
			return base64.StdEncoding.EncodeToString(b), nil
		},
		"atob": func(s string) (string, error) {
			b, err := decodeBase64Loosely(s)
			if err != nil {
				return "", err
			}
			out := make([]rune, len(b))
			for i, c := range b {
				out[i] = rune(c)
			}
			return string(out), nil
		},
	}); err != nil {
		return nil, err
	}
	m := &messaging{rt: rt, loop: loop, ports: map[int]*portEnd{}}
	if err := setAll(host, m.host()); err != nil {
		return nil, err
	}
	decoder, err := newTextDecoder(rt, host)
	if err != nil {
		return nil, err
	}
	if err := host.Set("TextDecoder", decoder); err != nil {
		return nil, err
	}

	api, err := evalWithHost(rt, "<webapis>", webAPIsJS, host)
	if err != nil {
		return nil, err
	}
	// Each name goes on the global object, which is where a program written for
	// a browser looks for it.
	for _, name := range []string{
		"URL", "URLSearchParams", "TextEncoder", "TextDecoder",
		"atob", "btoa", "structuredClone", "crypto",
		"AbortController", "AbortSignal", "Event", "EventTarget", "CustomEvent",
		"Blob", "File", "FormData", "URLPattern",
		"DOMException", "MessageChannel", "MessagePort", "MessageEvent", "BroadcastChannel",
	} {
		v, err := api.Get(name)
		if err != nil {
			return nil, err
		}
		if err := rt.Set(name, v); err != nil {
			return nil, err
		}
	}
	if err := installPerformance(rt, host); err != nil {
		return nil, err
	}
	if err := installWebCrypto(rt, webcryptoNative); err != nil {
		return nil, err
	}
	return m, nil
}

// decodeBase64Loosely decodes what atob accepts: padding is optional, and
// whitespace anywhere is ignored.
func decodeBase64Loosely(s string) ([]byte, error) {
	clean := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case ' ', '\t', '\n', '\r', '\f':
		default:
			clean = append(clean, c)
		}
	}
	for len(clean)%4 != 0 {
		clean = append(clean, '=')
	}
	b, err := base64.StdEncoding.DecodeString(string(clean))
	if err != nil {
		return nil, errors.New("the string is not correctly encoded")
	}
	return b, nil
}

// webAPIsJS is everything above that is better said in script: the interfaces,
// their getters and setters, and the parsing.
const webAPIsJS = `(function (host) {
  "use strict";

  // --- Text ---------------------------------------------------------------

  const codecApply = Reflect.apply;
  const codecString = String;
  const codecCharCodeAt = String.prototype.charCodeAt;
  const codecIsView = ArrayBuffer.isView;
  const codecUint8Array = Uint8Array;
  const codecTypedArray = Object.getPrototypeOf(Uint8Array.prototype);
  const codecTag = Object.getOwnPropertyDescriptor(codecTypedArray, Symbol.toStringTag).get;
  const codecBuffer = Object.getOwnPropertyDescriptor(codecTypedArray, "buffer").get;
  const codecLength = Object.getOwnPropertyDescriptor(codecTypedArray, "byteLength").get;
  const codecResizable = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, "resizable")?.get;
  const codecGrowable = typeof SharedArrayBuffer === "undefined" ? undefined
    : Object.getOwnPropertyDescriptor(SharedArrayBuffer.prototype, "growable")?.get;
  const textEncoders = new WeakSet();
  const encoderAdd = WeakSet.prototype.add, encoderHas = WeakSet.prototype.has;

  function webDOMString(value) {
    if (typeof value === "symbol") throw new TypeError("A Symbol cannot be converted to DOMString");
    return codecString(value);
  }

  function requireTextEncoder(value) {
    if (!codecApply(encoderHas, textEncoders, [value])) {
      throw new TypeError("The receiver is not a TextEncoder");
    }
  }

  // The encoder produces well-formed UTF-8, which means an unpaired surrogate
  // becomes U+FFFD: that is what the standard says, and what makes the result
  // safe to hand to anything expecting UTF-8.
  class TextEncoder {
    constructor() { codecApply(encoderAdd, textEncoders, [this]); host.cloneAs(this, "opaque"); }
    get encoding() { requireTextEncoder(this); return "utf-8"; }
    encode(input = "") {
      requireTextEncoder(this);
      const s = webDOMString(input);
      const out = [];
      for (let i = 0; i < s.length; i++) {
        let c = codecApply(codecCharCodeAt, s, [i]);
        if (c >= 0xd800 && c <= 0xdbff && i + 1 < s.length) {
          const next = codecApply(codecCharCodeAt, s, [i + 1]);
          if (next >= 0xdc00 && next <= 0xdfff) {
            c = 0x10000 + ((c - 0xd800) << 10) + (next - 0xdc00);
            i++;
          }
        }
        if (c >= 0xd800 && c <= 0xdfff) c = 0xfffd;
        if (c < 0x80) out.push(c);
        else if (c < 0x800) out.push(0xc0 | (c >> 6), 0x80 | (c & 63));
        else if (c < 0x10000) {
          out.push(0xe0 | (c >> 12), 0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
        } else {
          out.push(0xf0 | (c >> 18), 0x80 | ((c >> 12) & 63),
                   0x80 | ((c >> 6) & 63), 0x80 | (c & 63));
        }
      }
      return new codecUint8Array(out);
    }
    encodeInto(source, dest) {
      requireTextEncoder(this);
      if (arguments.length < 2) throw new TypeError("encodeInto requires source and destination");
      const text = webDOMString(source);
      if (!codecIsView(dest) || codecApply(codecTag, dest, []) !== "Uint8Array") {
        throw new TypeError("The destination must be a Uint8Array");
      }
      const buffer = codecApply(codecBuffer, dest, []);
      let resizable = false;
      if (codecResizable !== undefined) {
        try { resizable = codecApply(codecResizable, buffer, []); }
        catch (_) { resizable = codecGrowable !== undefined && codecApply(codecGrowable, buffer, []); }
      }
      if (resizable) throw new TypeError("The destination buffer must have fixed length");
      const capacity = codecApply(codecLength, dest, []);
      let read = 0, written = 0;
      while (read < text.length) {
        let point = codecApply(codecCharCodeAt, text, [read]), units = 1;
        if (point >= 0xd800 && point <= 0xdbff && read + 1 < text.length) {
          const next = codecApply(codecCharCodeAt, text, [read + 1]);
          if (next >= 0xdc00 && next <= 0xdfff) {
            point = 0x10000 + ((point - 0xd800) << 10) + next - 0xdc00;
            units = 2;
          }
        }
        if (point >= 0xd800 && point <= 0xdfff) point = 0xfffd;
        const size = point < 0x80 ? 1 : point < 0x800 ? 2 : point < 0x10000 ? 3 : 4;
        if (capacity - written < size) break;
        if (size === 1) dest[written++] = point;
        else if (size === 2) {
          dest[written++] = 0xc0 | (point >> 6);
          dest[written++] = 0x80 | (point & 63);
        } else if (size === 3) {
          dest[written++] = 0xe0 | (point >> 12);
          dest[written++] = 0x80 | ((point >> 6) & 63);
          dest[written++] = 0x80 | (point & 63);
        } else {
          dest[written++] = 0xf0 | (point >> 18);
          dest[written++] = 0x80 | ((point >> 12) & 63);
          dest[written++] = 0x80 | ((point >> 6) & 63);
          dest[written++] = 0x80 | (point & 63);
        }
        read += units;
      }
      return {read, written};
    }
  }

  const TextDecoder = host.TextDecoder;

  // --- URLPattern -----------------------------------------------------------

  // A pattern is compiled to a regular expression with named groups, which is
  // what the syntax is a shorthand for: ":id" is a segment, "*" is anything,
  // and what is written in parentheses is a matcher of your own.
  //
  // The parts of a URL a pattern does not mention match anything, so
  // new URLPattern({pathname: "/books/:id"}) is about the path alone.
  const patternParts = [
    "protocol", "username", "password", "hostname", "port",
    "pathname", "search", "hash",
  ];
  const patternStates = new WeakMap();
  const patternWellFormed = String.prototype.toWellFormed;
  const patternGet = WeakMap.prototype.get, patternSet = WeakMap.prototype.set;
  const patternCreate = Object.create, patternDefine = Object.defineProperty;
  function patternString(value) {
    return codecApply(patternWellFormed, webDOMString(value), []);
  }
  function patternInput(value) {
    if (value !== null && value !== undefined && typeof value !== "object" && typeof value !== "function") {
      return patternString(value);
    }
    const result = patternCreate(null);
    // Web IDL dictionary conversion visits members in lexicographic order.
    for (const key of ["baseURL", "hash", "hostname", "password", "pathname", "port", "protocol", "search", "username"]) {
      const item = value == null ? undefined : value[key];
      if (item !== undefined) result[key] = patternString(item);
    }
    return result;
  }
  function patternOptions(value) {
    if (value == null) return false;
    if (typeof value !== "object" && typeof value !== "function") throw new TypeError("URLPattern options must be a dictionary");
    return !!value.ignoreCase;
  }
  function patternOf(value) {
    const state = codecApply(patternGet, patternStates, [value]);
    if (state === undefined) throw new TypeError("The receiver is not a URLPattern");
    return state;
  }
  function escapePattern(value) { return value.replace(/[+*?:{}()\\]/g, "\\$&"); }
  function patternURLValues(url) {
    return {protocol: url.protocol.slice(0, -1), username: url.username,
      password: url.password, hostname: url.hostname, port: url.port,
      pathname: url.pathname, search: url.search.replace(/^\?/, ""), hash: url.hash.replace(/^#/, "")};
  }
  function canonicalPatternValue(value, part, protocol) {
    if (part === "protocol") {
      if (value !== "" && !/^[A-Za-z][A-Za-z0-9+.-]*$/.test(value)) throw new TypeError("Invalid URLPattern protocol");
      return value.toLowerCase();
    }
    if (part === "hostname") {
      if (/[\s/#?@]/.test(value)) throw new TypeError("Invalid URLPattern hostname");
      return value.toLowerCase();
    }
    if (part === "port") {
      if (value === "") return "";
      if (!/^\d+$/.test(value) || +value > 65535) throw new TypeError("Invalid URLPattern port");
      const normalized = String(+value);
      return specialPorts[protocol + ":"] === normalized ? "" : normalized;
    }
    if (part === "username" || part === "password") return encodeSet(value, "/:;=@[\\]^|?#");
    if (part === "search") return encodeSet(value, "#");
    if (part === "hash") return encodeSet(value, "");
    if (part === "pathname") {
      const encoded = encodeSet(value, "?#");
      if (protocol !== "" && !isSpecial(protocol + ":")) return encoded;
      if (encoded === "") return "";
      return encoded.startsWith("/") ? normalizePath(encoded) : normalizePath("/-" + encoded).slice(2);
    }
    return value;
  }
  function canonicalPatternSource(value, part, protocol) {
    const special = protocol === undefined || protocol === "*" || isSpecial(protocol.toLowerCase() + ":");
    if (part === "hostname" && /^\[[0-9A-Fa-f:]+\]$/.test(value)) return escapePattern(value.toLowerCase());
    if (!/[+*?:{}()\\]/.test(value)) return canonicalPatternValue(value, part, part === "pathname" && !special ? protocol : "");
    if (part === "pathname") {
      value = encodeSet(value, "");
      if (special) {
        return value.startsWith("/") ? normalizePath(value) : normalizePath("/-" + value).slice(2);
      }
      return value;
    }
    if (part === "search" || part === "hash") return encodeSet(value, "");
    if (part === "hostname" || part === "protocol") {
      let result = "";
      for (let i = 0; i < value.length;) {
        if (value[i] === "\\") { result += value.slice(i, i + 2); i += 2; continue; }
        if (value[i] === "(") {
          const end = matchingParen(value, i); result += value.slice(i, end + 1); i = end + 1; continue;
        }
        if (value[i] === ":") {
          let end = i + 1; while (end < value.length && /[\w$]/.test(value[end])) end++;
          result += value.slice(i, end); i = end; continue;
        }
        if (/\s/.test(value[i])) throw new TypeError("Invalid URLPattern component");
        result += value[i++].toLowerCase();
      }
      return result;
    }
    return value;
  }
  function processPatternInit(init, pattern) {
    const result = patternCreate(null);
    if (!pattern) for (const part of patternParts) result[part] = "";
    let base;
    if (init.baseURL !== undefined) base = new URL(init.baseURL);
    if (base) {
      const values = patternURLValues(base);
      for (const part of patternParts) values[part] = canonicalPatternValue(values[part], part, values.protocol);
      const inherit = (part, blockers) => {
        if (!blockers.some(key => init[key] !== undefined)) {
          result[part] = pattern ? escapePattern(values[part]) : values[part];
        }
      };
      inherit("protocol", ["protocol"]);
      if (!pattern) {
        inherit("username", ["protocol", "hostname", "port", "username"]);
        inherit("password", ["protocol", "hostname", "port", "username", "password"]);
      }
      inherit("hostname", ["protocol", "hostname"]);
      inherit("port", ["protocol", "hostname", "port"]);
      inherit("pathname", ["protocol", "hostname", "port", "pathname"]);
      inherit("search", ["protocol", "hostname", "port", "pathname", "search"]);
      inherit("hash", ["protocol", "hostname", "port", "pathname", "search", "hash"]);
    }
    for (const part of patternParts) {
      if (init[part] === undefined) continue;
      let value = init[part];
      if (part === "protocol") value = value.replace(/:$/, "");
      if (part === "search") value = value.replace(/^\?/, "");
      if (part === "hash") value = value.replace(/^#/, "");
      if (part === "pathname" && base && (base.hostname !== "" || base.pathname.startsWith("/")) &&
          !value.startsWith("/") && !(pattern && (value.startsWith("\\/") || value.startsWith("{/")))) {
        const path = pattern ? escapePattern(base.pathname) : base.pathname;
        value = path.slice(0, path.lastIndexOf("/") + 1) + value;
      }
      result[part] = pattern ? value : canonicalPatternValue(value, part, result.protocol || "");
    }
    return result;
  }

  function compilePart(pattern, part) {
    // A part nobody constrained matches whatever is there.
    if (pattern === undefined || pattern === "*") {
      return {source: "^(?<_0>.*)$", names: ["0"], wild: true, hasRegExpGroups: false};
    }
    const names = [];
    const segment = part === "pathname";
    // What one :name may match: a path stops at a separator, everything else
    // does not.
    const one = segment ? "[^/]+?" : part === "hostname" ? "[^.]+?" : "[^]+?";
    let hasRegExpGroups = false;
    let out = "^";
    let i = 0;
    let unnamed = 0;
    while (i < pattern.length) {
      const c = pattern[i];
      if (c === "\\") {
        if (++i === pattern.length) throw new TypeError("A pattern escape must have a following character");
        out += escapeRe(pattern[i++]);
        continue;
      }
      if (c === ":") {
        let j = i + 1;
        while (j < pattern.length && /[\w$]/.test(pattern[j])) j++;
        if (j === i + 1) throw new TypeError("a : must be followed by a name");
        const name = pattern.slice(i + 1, j);
        if (/^[0-9]/.test(name)) throw new TypeError("A pattern name must begin with an identifier character");
        names.push(name);
        i = j;
        // A name may be followed by its own matcher, and then by how many of
        // it there may be.
        let body = one;
        if (pattern[i] === "(") {
          hasRegExpGroups = true;
          const end = matchingParen(pattern, i);
          body = pattern.slice(i + 1, end);
          i = end + 1;
        }
        const mod = modifierAt(pattern, i);
        i += mod.length;
        // An optional segment takes the separator before it with it, so that
        // /items/:id? matches /items as well as /items/7 -- and does not ask
        // for /items// in between.
        if (segment && (mod === "?" || mod === "*") && out.endsWith("\\/")) {
          out = out.slice(0, -2);
        }
        out += group(name, body, mod, segment);
        continue;
      }
      if (c === "*") {
        const name = String(unnamed++);
        names.push(name);
        out += "(?<" + safeName(name) + ">.*)";
        i++;
        continue;
      }
      if (c === "(") {
        hasRegExpGroups = true;
        const end = matchingParen(pattern, i);
        const name = String(unnamed++);
        names.push(name);
        out += "(?<" + safeName(name) + ">" + pattern.slice(i + 1, end) + ")";
        i = end + 1;
        continue;
      }
      if (c === "{") {
        // A braced group is a piece that may repeat or be left out; what is
        // inside it is a pattern of its own.
        const end = matchingBrace(pattern, i);
        const inner = compilePart(pattern.slice(i + 1, end), part);
        hasRegExpGroups ||= inner.hasRegExpGroups;
        i = end + 1;
        const mod = modifierAt(pattern, i);
        i += mod.length;
        names.push(...inner.names);
        out += "(?:" + inner.source.slice(1, -1) + ")" + (mod || "");
        continue;
      }
      out += escapeRe(c);
      i++;
    }
    return {source: out + "$", names, wild: false, hasRegExpGroups};
  }

  function group(name, body, mod, segment) {
    const named = "(?<" + safeName(name) + ">" + body + ")";
    switch (mod) {
      case "?": return segment ? "(?:\\/" + named + ")?" : named + "?";
      case "*": return segment ? "(?:\\/" + starred(name, body) + ")?" : named + "*";
      case "+": return segment ? starred(name, body) : named + "+";
    }
    return named;
  }

  // A repeated segment is itself and everything after it, separators and all.
  const starred = (name, body) =>
    "(?<" + safeName(name) + ">" + body + "(?:\\/" + body + ")*)";

  function modifierAt(pattern, i) {
    const c = pattern[i];
    return c === "?" || c === "*" || c === "+" ? c : "";
  }

  // A group name has to be an identifier, and a wildcard's name is a number.
  const safeName = (name) => /^[A-Za-z_$]/.test(name) ? name : "_" + name;

  function matchingParen(s, at) {
    let depth = 0;
    for (let i = at; i < s.length; i++) {
      if (s[i] === "\\") { i++; continue; }
      if (s[i] === "(") depth++;
      else if (s[i] === ")" && --depth === 0) return i;
    }
    throw new TypeError("a ( in a pattern was never closed");
  }

  function matchingBrace(s, at) {
    let depth = 0;
    for (let i = at; i < s.length; i++) {
      if (s[i] === "\\") { i++; continue; }
      if (s[i] === "{") depth++;
      else if (s[i] === "}" && --depth === 0) return i;
    }
    throw new TypeError("a { in a pattern was never closed");
  }

  const escapeRe = (c) => /[\\^$.*+?()[\]{}|\/]/.test(c) ? "\\" + c : c;

  const patternExec = RegExp.prototype.exec;
  function matchPattern(state, input, baseURL) {
    const converted = patternInput(input);
    const base = baseURL === undefined ? undefined : patternString(baseURL);
    if (typeof converted !== "string" && base !== undefined) {
      throw new TypeError("An object input must contain its own baseURL");
    }
    let values, inputs;
    if (typeof converted === "string") {
      try {
        values = patternURLValues(new URL(converted, base));
        for (const part of patternParts) values[part] = canonicalPatternValue(values[part], part, values.protocol);
      } catch (_) { return null; }
      inputs = base === undefined ? [converted] : [converted, base];
    } else {
      try { values = processPatternInit(converted, false); }
      catch (_) { return null; }
      const dictionary = {};
      for (const key of Object.keys(converted)) {
        patternDefine(dictionary, key, {value: converted[key], writable: true, enumerable: true, configurable: true});
      }
      inputs = [dictionary];
    }
    const result = {inputs};
    for (const part of patternParts) {
      const compiled = state.parts[part];
      const found = codecApply(patternExec, compiled.re, [values[part]]);
      if (found === null) return null;
      const groups = {};
      for (const name of compiled.names) {
        patternDefine(groups, name, {value: found.groups[safeName(name)], writable: true, enumerable: true, configurable: true});
      }
      patternDefine(result, part, {value: {input: values[part], groups}, writable: true, enumerable: true, configurable: true});
    }
    return result;
  }
  class URLPattern {
    constructor(input = undefined, baseURLOrOptions = undefined, options = undefined) {
      const converted = patternInput(input);
      const withBase = arguments.length >= 3 || (baseURLOrOptions != null &&
        typeof baseURLOrOptions !== "object" && typeof baseURLOrOptions !== "function");
      const baseURL = withBase ? patternString(baseURLOrOptions) : undefined;
      const ignoreCase = patternOptions(withBase ? options : baseURLOrOptions);
      let init;
      if (typeof converted === "string") init = patternFromString(converted, baseURL);
      else {
        if (withBase) throw new TypeError("An object pattern must contain its own baseURL");
        init = converted;
      }
      const processed = processPatternInit(init, true);
      if (processed.port !== undefined && /^\d+$/.test(processed.port) &&
          specialPorts[processed.protocol + ":"] === String(+processed.port)) processed.port = "";
      const parts = patternCreate(null);
      let hasRegExpGroups = false;
      for (const part of patternParts) {
        const raw = processed[part] === undefined ? "*" : processed[part];
        const source = canonicalPatternSource(raw, part, processed.protocol);
        let compiled, re;
        try {
          compiled = compilePart(source, part);
          re = new RegExp(compiled.source, "u" + (ignoreCase && ["pathname", "search", "hash"].includes(part) ? "i" : ""));
        } catch (_) { throw new TypeError("Invalid URLPattern component: " + part); }
        parts[part] = {value: source, re, names: compiled.names};
        hasRegExpGroups ||= compiled.hasRegExpGroups;
      }
      codecApply(patternSet, patternStates, [this, {parts, hasRegExpGroups}]);
      host.cloneAs(this, "unsupported");
    }
    test(input = undefined, baseURL = undefined) { return matchPattern(patternOf(this), input, baseURL) !== null; }
    exec(input = undefined, baseURL = undefined) { return matchPattern(patternOf(this), input, baseURL); }
    get hasRegExpGroups() { return patternOf(this).hasRegExpGroups; }
  }
  for (const part of patternParts) {
    patternDefine(URLPattern.prototype, part, {get() { return patternOf(this).parts[part].value; }, enumerable: true, configurable: true});
  }
  for (const name of ["test", "exec", "hasRegExpGroups"]) patternDefine(URLPattern.prototype, name, {enumerable: true});
  patternDefine(URLPattern.prototype, Symbol.toStringTag, {value: "URLPattern", configurable: true});

  // patternFromString takes a pattern written as a whole URL apart. The parts
  // are split on the characters that separate them rather than parsed, since
  // what is between them is a pattern and not a URL.
  function patternFromString(text, baseURL) {
    const init = patternCreate(null);
    let rest = text;
    if (baseURL !== undefined) init.baseURL = baseURL;
    const scheme = /^([A-Za-z][\w+.-]*):/.exec(rest);
    if (scheme) {
      init.protocol = scheme[1];
      rest = rest.slice(scheme[0].length);
    } else if (baseURL === undefined) throw new TypeError("A relative URL pattern requires a baseURL");
    if (scheme && rest.startsWith("//")) {
      rest = rest.slice(2);
      const boundaries = separators(rest);
      const slash = rest.indexOf("/");
      const endpoints = [slash, boundaries.query, boundaries.hash].filter(index => index >= 0);
      const end = endpoints.length ? Math.min(...endpoints) : rest.length;
      let authority = rest.slice(0, end);
      rest = rest.slice(end);
      const at = authority.lastIndexOf("@");
      if (at >= 0) {
        const credentials = authority.slice(0, at), colon = credentials.indexOf(":");
        init.username = colon < 0 ? credentials : credentials.slice(0, colon);
        if (colon >= 0) init.password = credentials.slice(colon + 1);
        authority = authority.slice(at + 1);
      }
      const colon = authority.startsWith("[") ? authority.indexOf(":", authority.indexOf("]") + 1) : authority.lastIndexOf(":");
      if (colon > 0 && !authority.slice(colon).includes("}")) {
        init.hostname = authority.slice(0, colon); init.port = authority.slice(colon + 1);
      } else init.hostname = authority;
      if (init.port === undefined) init.port = "";
    }
    // Where the query and the fragment begin cannot be found by looking for
    // the characters that start them: the ? of "/items/:id?" is a modifier,
    // not a query. What tells them apart is what comes before.
    const {query, hash} = separators(rest);
    if (hash >= 0) {
      init.hash = rest.slice(hash + 1);
      if (query < 0 && (scheme || hash > 0)) init.search = "";
      rest = rest.slice(0, hash);
    }
    if (query >= 0) {
      init.search = rest.slice(query + 1);
      rest = rest.slice(0, query);
    }
    if (rest !== "") init.pathname = rest;
    else if (!scheme && query !== 0 && hash !== 0) init.pathname = "";
    else if (scheme && (query >= 0 || hash >= 0)) init.pathname = isSpecial(init.protocol.toLowerCase() + ":") ? "/" : "";
    return init;
  }

  // separators walks a pattern and reports where the query and the fragment
  // begin, skipping what belongs to a name, a group or a matcher.
  function separators(pattern) {
    let query = -1, hash = -1;
    // modifiable is true just after something a ? or a + could apply to.
    let modifiable = false;
    for (let i = 0; i < pattern.length; i++) {
      const c = pattern[i];
      if (c === "\\") { i++; modifiable = false; continue; }
      if (c === ":") {
        let j = i + 1;
        while (j < pattern.length && /[\w$]/.test(pattern[j])) j++;
        i = j - 1;
        modifiable = true;
        continue;
      }
      if (c === "(") { i = matchingParen(pattern, i); modifiable = true; continue; }
      if (c === "{") { i = matchingBrace(pattern, i); modifiable = true; continue; }
      if (c === "*") { modifiable = true; continue; }
      if (c === "?" && modifiable) { modifiable = false; continue; }
      if (c === "+" && modifiable) { modifiable = false; continue; }
      if (c === "?" && query < 0 && hash < 0) { query = i; modifiable = false; continue; }
      if (c === "#" && hash < 0) { hash = i; modifiable = false; continue; }
      modifiable = false;
    }
    return {query, hash};
  }

  // --- Blob and FormData ---------------------------------------------------

  // A Blob is bytes with a type. It is not a file on a disk and never becomes
  // one: what is here is the part of it a program uses to move bytes around --
  // building them out of pieces, cutting them up, and handing them to a
  // request or a response.
  const bytesOfPart = (part) => {
    if (part instanceof Blob) return part._bytes;
    if (part instanceof Uint8Array) return part;
    if (ArrayBuffer.isView(part)) {
      return new Uint8Array(part.buffer, part.byteOffset, part.byteLength);
    }
    if (part instanceof ArrayBuffer) return new Uint8Array(part);
    return new TextEncoder().encode(String(part));
  };

  function joinBytes(pieces) {
    let total = 0;
    for (const p of pieces) total += p.length;
    const out = new Uint8Array(total);
    let at = 0;
    for (const p of pieces) { out.set(p, at); at += p.length; }
    return out;
  }

  // blobs are the Blobs this runtime made, which clone as Blobs.
  const blobs = new WeakSet();

  class Blob {
    constructor(parts = [], options = {}) {
      host.cloneAs(this, "host");
      blobs.add(this);
      const pieces = [];
      for (const part of parts) pieces.push(bytesOfPart(part));
      Object.defineProperty(this, "_bytes", {value: joinBytes(pieces)});
      Object.defineProperty(this, "_type", {value: String((options && options.type) || "").toLowerCase()});
    }
    // What a Blob is lives in it, not in properties of its own, as in Node:
    // Object.keys and a clone see none.
    get type() { return this._type; }
    get [Symbol.toStringTag]() { return "Blob"; }
    get size() { return this._bytes.length; }
    async text() { return new TextDecoder().decode(this._bytes); }
    async bytes() { return this._bytes.slice(); }
    async arrayBuffer() {
      const b = this._bytes;
      return b.buffer.slice(b.byteOffset, b.byteOffset + b.byteLength);
    }
    slice(start = 0, end = this.size, type = "") {
      return new Blob([this._bytes.slice(start, end)], {type});
    }
    stream() {
      if (typeof ReadableStream === "undefined") {
        throw new TypeError("this runtime has no streams");
      }
      return ReadableStream.from([this._bytes.slice()]);
    }
  }

  // A File is a Blob with a name, which is what a form sends and what a
  // program reading one asks for.
  class File extends Blob {
    constructor(parts, name, options = {}) {
      super(parts, options);
      Object.defineProperty(this, "_name", {value: String(name)});
      Object.defineProperty(this, "_lastModified", {value: options && options.lastModified !== undefined
        ? Number(options.lastModified) : Date.now()});
    }
    get name() { return this._name; }
    get lastModified() { return this._lastModified; }
    get [Symbol.toStringTag]() { return "File"; }
  }

  // FormData keeps its entries in order and allows a name more than once,
  // which is the whole reason it is not an object.
  class FormData {
    constructor() {
      host.cloneAs(this, "unsupported");
      Object.defineProperty(this, "_entries", {value: [], writable: true});
    }
    append(name, value, filename) {
      this._entries.push([String(name), wrapEntry(value, filename)]);
    }
    set(name, value, filename) {
      const key = String(name);
      const at = this._entries.findIndex(e => e[0] === key);
      const entry = [key, wrapEntry(value, filename)];
      if (at < 0) this._entries.push(entry);
      else {
        this._entries[at] = entry;
        this._entries = this._entries.filter((e, i) => i <= at || e[0] !== key);
      }
    }
    get(name) {
      const found = this._entries.find(e => e[0] === String(name));
      return found ? found[1] : null;
    }
    getAll(name) {
      return this._entries.filter(e => e[0] === String(name)).map(e => e[1]);
    }
    has(name) { return this._entries.some(e => e[0] === String(name)); }
    delete(name) {
      this._entries = this._entries.filter(e => e[0] !== String(name));
    }
    *entries() { for (const e of this._entries) yield [e[0], e[1]]; }
    *keys() { for (const e of this._entries) yield e[0]; }
    *values() { for (const e of this._entries) yield e[1]; }
    [Symbol.iterator]() { return this.entries(); }
    forEach(fn, thisArg) {
      for (const [k, v] of this._entries) fn.call(thisArg, v, k, this);
    }
  }

  // A value that is not a Blob is text; one that is, and is given a name,
  // becomes a File, since that is what the other end will see.
  function wrapEntry(value, filename) {
    if (value instanceof Blob) {
      if (filename === undefined) {
        return value instanceof File ? value : new File([value], "blob", {type: value.type});
      }
      return new File([value], String(filename), {type: value.type});
    }
    return String(value);
  }

  // --- URL ----------------------------------------------------------------

  const specialPorts = {"http:": "80", "https:": "443", "ws:": "80", "wss:": "443", "ftp:": "21"};
  const isSpecial = (scheme) => Object.prototype.hasOwnProperty.call(specialPorts, scheme) || scheme === "file:";

  function encodeSet(s, extra) {
    let out = "";
    for (const ch of s) {
      const code = ch.codePointAt(0);
      if (code <= 0x20 || code >= 0x7f || extra.includes(ch)) {
        for (const b of new TextEncoder().encode(ch)) {
          out += "%" + b.toString(16).toUpperCase().padStart(2, "0");
        }
      } else {
        out += ch;
      }
    }
    return out;
  }

  // Removing dot segments, which is what makes a/b/../c into a/c.
  function normalizePath(path) {
    const parts = path.split("/");
    const out = [];
    for (let i = 0; i < parts.length; i++) {
      const p = parts[i];
      if (p === ".") {
        if (i === parts.length - 1) out.push("");
        continue;
      }
      if (p === "..") {
        if (out.length > 1) out.pop();
        if (i === parts.length - 1) out.push("");
        continue;
      }
      out.push(p);
    }
    let joined = out.join("/");
    if (!joined.startsWith("/")) joined = "/" + joined;
    return joined;
  }

  function parseURL(input, base) {
    let rest = String(input).trim().replace(/[\t\n\r]/g, "");
    const url = {scheme: "", username: "", password: "", host: "", port: "",
                 path: "", query: null, fragment: null};

    const schemeMatch = /^([A-Za-z][A-Za-z0-9+\-.]*):/.exec(rest);
    if (schemeMatch) {
      url.scheme = schemeMatch[1].toLowerCase() + ":";
      rest = rest.slice(schemeMatch[0].length);
    } else if (base) {
      // A relative reference takes everything up to the part it replaces.
      return resolveRelative(rest, base);
    } else {
      throw new TypeError("Invalid URL: " + input);
    }

    if (isSpecial(url.scheme)) rest = rest.replace(/^[\\/]{2}/, "//");
    if (rest.startsWith("//")) {
      rest = rest.slice(2);
      let authority = rest;
      const end = authority.search(/[/?#]/);
      if (end >= 0) { authority = authority.slice(0, end); rest = rest.slice(end); }
      else rest = "";
      const at = authority.lastIndexOf("@");
      if (at >= 0) {
        const creds = authority.slice(0, at);
        authority = authority.slice(at + 1);
        const colon = creds.indexOf(":");
        url.username = colon < 0 ? creds : creds.slice(0, colon);
        url.password = colon < 0 ? "" : creds.slice(colon + 1);
      }
      if (authority.startsWith("[")) {
        const close = authority.indexOf("]");
        url.host = authority.slice(0, close + 1).toLowerCase();
        const after = authority.slice(close + 1);
        if (after.startsWith(":")) url.port = after.slice(1);
      } else {
        const colon = authority.lastIndexOf(":");
        if (colon >= 0) {
          url.host = authority.slice(0, colon).toLowerCase();
          url.port = authority.slice(colon + 1);
        } else {
          url.host = authority.toLowerCase();
        }
      }
      if (url.port !== "" && !/^\d*$/.test(url.port)) {
        throw new TypeError("Invalid URL: " + input);
      }
      if (url.port === specialPorts[url.scheme]) url.port = "";
      if (isSpecial(url.scheme) && url.host === "" && url.scheme !== "file:") {
        throw new TypeError("Invalid URL: " + input);
      }
    } else if (isSpecial(url.scheme) && url.scheme !== "file:") {
      throw new TypeError("Invalid URL: " + input);
    }

    const hash = rest.indexOf("#");
    if (hash >= 0) { url.fragment = rest.slice(hash + 1); rest = rest.slice(0, hash); }
    const q = rest.indexOf("?");
    if (q >= 0) { url.query = rest.slice(q + 1); rest = rest.slice(0, q); }
    url.path = rest;
    if (url.host !== "" || isSpecial(url.scheme)) url.path = normalizePath(url.path);
    return url;
  }

  // A relative reference is resolved against a base, which is RFC 3986's merge.
  function resolveRelative(ref, base) {
    const url = {scheme: base.scheme, username: base.username, password: base.password,
                 host: base.host, port: base.port, path: base.path,
                 query: base.query, fragment: null};
    if (ref === "") { url.query = base.query; return url; }
    if (ref.startsWith("#")) { url.fragment = ref.slice(1); url.query = base.query; return url; }
    if (ref.startsWith("//")) return parseURL(base.scheme + ref);

    let rest = ref;
    const hash = rest.indexOf("#");
    if (hash >= 0) { url.fragment = rest.slice(hash + 1); rest = rest.slice(0, hash); }
    const q = rest.indexOf("?");
    if (q >= 0) { url.query = rest.slice(q + 1); rest = rest.slice(0, q); }
    else url.query = rest === "" ? base.query : null;

    if (rest === "") { url.path = base.path; return url; }
    if (rest.startsWith("/")) url.path = normalizePath(rest);
    else {
      const dir = base.path.slice(0, base.path.lastIndexOf("/") + 1);
      url.path = normalizePath(dir + rest);
    }
    return url;
  }

  function serialize(u, excludeFragment) {
    let out = u.scheme;
    if (u.host !== "" || u.scheme === "file:") {
      out += "//";
      if (u.username !== "" || u.password !== "") {
        out += u.username;
        if (u.password !== "") out += ":" + u.password;
        out += "@";
      }
      out += u.host;
      if (u.port !== "") out += ":" + u.port;
    }
    out += u.path;
    if (u.query !== null) out += "?" + u.query;
    if (!excludeFragment && u.fragment !== null) out += "#" + u.fragment;
    return out;
  }

  const internal = new WeakMap();

  class URLSearchParams {
    constructor(init = "") {
      host.cloneAs(this, "unsupported");
      let pairs = [];
      if (typeof init === "string") {
        pairs = parseQuery(init);
      } else if (init instanceof URLSearchParams) {
        pairs = internal.get(init).pairs.map(p => p.slice());
      } else if (Array.isArray(init)) {
        pairs = init.map(p => [String(p[0]), String(p[1])]);
      } else if (init && typeof init === "object") {
        pairs = Object.entries(init).map(([k, v]) => [String(k), String(v)]);
      }
      internal.set(this, {pairs, url: null});
    }
    get size() { return internal.get(this).pairs.length; }
    append(name, value) {
      internal.get(this).pairs.push([String(name), String(value)]);
      update(this);
    }
    delete(name, value) {
      const state = internal.get(this);
      const n = String(name);
      state.pairs = state.pairs.filter(
        ([k, v]) => k !== n || (value !== undefined && v !== String(value)));
      update(this);
    }
    get(name) {
      const hit = internal.get(this).pairs.find(([k]) => k === String(name));
      return hit === undefined ? null : hit[1];
    }
    getAll(name) {
      return internal.get(this).pairs.filter(([k]) => k === String(name)).map(([, v]) => v);
    }
    has(name, value) {
      return internal.get(this).pairs.some(
        ([k, v]) => k === String(name) && (value === undefined || v === String(value)));
    }
    set(name, value) {
      const state = internal.get(this);
      const n = String(name);
      const at = state.pairs.findIndex(([k]) => k === n);
      if (at < 0) state.pairs.push([n, String(value)]);
      else {
        state.pairs[at] = [n, String(value)];
        state.pairs = state.pairs.filter(([k], i) => k !== n || i === at);
      }
      update(this);
    }
    sort() {
      const state = internal.get(this);
      state.pairs.sort((a, b) => a[0] < b[0] ? -1 : a[0] > b[0] ? 1 : 0);
      update(this);
    }
    forEach(fn, thisArg) {
      for (const [k, v] of internal.get(this).pairs.slice()) fn.call(thisArg, v, k, this);
    }
    *entries() { for (const p of internal.get(this).pairs.slice()) yield [p[0], p[1]]; }
    *keys() { for (const [k] of internal.get(this).pairs.slice()) yield k; }
    *values() { for (const [, v] of internal.get(this).pairs.slice()) yield v; }
    [Symbol.iterator]() { return this.entries(); }
    toString() {
      return internal.get(this).pairs
        .map(([k, v]) => encodeQuery(k) + "=" + encodeQuery(v)).join("&");
    }
  }

  function parseQuery(s) {
    const out = [];
    for (const part of String(s).replace(/^\?/, "").split("&")) {
      if (part === "") continue;
      const eq = part.indexOf("=");
      const k = eq < 0 ? part : part.slice(0, eq);
      const v = eq < 0 ? "" : part.slice(eq + 1);
      out.push([decodeQuery(k), decodeQuery(v)]);
    }
    return out;
  }

  function decodeQuery(s) {
    try { return decodeURIComponent(s.replace(/\+/g, " ")); }
    catch (e) { return s.replace(/\+/g, " "); }
  }

  function encodeQuery(s) {
    return encodeURIComponent(s).replace(/%20/g, "+").replace(/[!'()~]/g,
      c => "%" + c.charCodeAt(0).toString(16).toUpperCase());
  }

  // Writing to the parameters rewrites the query of the URL they came from.
  function update(params) {
    const state = internal.get(params);
    if (state.url === null) return;
    const text = params.toString();
    internal.get(state.url).url.query = text === "" ? null : text;
  }

  class URL {
    constructor(input, base) {
      host.cloneAs(this, "unsupported");
      let parsedBase = null;
      if (base !== undefined) {
        parsedBase = base instanceof URL ? internal.get(base).url : parseURL(String(base));
      }
      const url = parseURL(String(input), parsedBase);
      const params = new URLSearchParams(url.query === null ? "" : url.query);
      internal.set(this, {url, params});
      internal.get(params).url = this;
    }
    get href() { return serialize(internal.get(this).url); }
    set href(v) {
      const url = parseURL(String(v));
      const state = internal.get(this);
      state.url = url;
      const params = new URLSearchParams(url.query === null ? "" : url.query);
      internal.get(params).url = this;
      state.params = params;
    }
    get protocol() { return internal.get(this).url.scheme; }
    set protocol(v) {
      const s = String(v).replace(/:*$/, "") + ":";
      if (/^[A-Za-z][A-Za-z0-9+\-.]*:$/.test(s)) internal.get(this).url.scheme = s.toLowerCase();
    }
    get username() { return internal.get(this).url.username; }
    set username(v) { internal.get(this).url.username = encodeSet(String(v), ":@/?#"); }
    get password() { return internal.get(this).url.password; }
    set password(v) { internal.get(this).url.password = encodeSet(String(v), ":@/?#"); }
    get host() {
      const u = internal.get(this).url;
      return u.port === "" ? u.host : u.host + ":" + u.port;
    }
    set host(v) {
      const s = String(v);
      const colon = s.lastIndexOf(":");
      const u = internal.get(this).url;
      if (colon > 0 && !s.endsWith("]")) {
        u.host = s.slice(0, colon).toLowerCase();
        u.port = s.slice(colon + 1) === specialPorts[u.scheme] ? "" : s.slice(colon + 1);
      } else {
        u.host = s.toLowerCase();
      }
    }
    get hostname() { return internal.get(this).url.host; }
    set hostname(v) { internal.get(this).url.host = String(v).toLowerCase(); }
    get port() { return internal.get(this).url.port; }
    set port(v) {
      const u = internal.get(this).url;
      const s = String(v);
      if (s === "") { u.port = ""; return; }
      if (/^\d+$/.test(s)) u.port = s === specialPorts[u.scheme] ? "" : s;
    }
    get pathname() { return internal.get(this).url.path; }
    set pathname(v) {
      const u = internal.get(this).url;
      u.path = normalizePath(encodeSet(String(v), "?#"));
    }
    get search() {
      const q = internal.get(this).url.query;
      return q === null || q === "" ? "" : "?" + q;
    }
    set search(v) {
      const s = String(v).replace(/^\?/, "");
      const state = internal.get(this);
      state.url.query = s === "" ? null : encodeSet(s, "#");
      const params = new URLSearchParams(s);
      internal.get(params).url = this;
      state.params = params;
    }
    get searchParams() { return internal.get(this).params; }
    get hash() {
      const f = internal.get(this).url.fragment;
      return f === null || f === "" ? "" : "#" + f;
    }
    set hash(v) {
      const s = String(v).replace(/^#/, "");
      internal.get(this).url.fragment = s === "" ? null : encodeSet(s, "");
    }
    get origin() {
      const u = internal.get(this).url;
      if (!isSpecial(u.scheme) || u.scheme === "file:") return "null";
      return u.scheme + "//" + u.host + (u.port === "" ? "" : ":" + u.port);
    }
    toString() { return this.href; }
    toJSON() { return this.href; }
    static canParse(input, base) {
      try { new URL(input, base); return true; } catch (e) { return false; }
    }
    static parse(input, base) {
      try { return new URL(input, base); } catch (e) { return null; }
    }
  }

  // --- Events and abortion -------------------------------------------------

  const eventStates = new WeakMap(), customEventStates = new WeakMap();
  const eventMapGet = WeakMap.prototype.get, eventMapSet = WeakMap.prototype.set;
  const eventCreate = Object.create;
  const eventDefineProperty = Object.defineProperty;
  function eventOf(value) {
    const state = codecApply(eventMapGet, eventStates, [value]);
    if (state === undefined) throw new TypeError("The receiver is not an Event");
    return state;
  }
  function customEventOf(value) {
    const state = codecApply(eventMapGet, customEventStates, [value]);
    if (state === undefined) throw new TypeError("The receiver is not a CustomEvent");
    return state;
  }
  function eventInit(init) {
    if (init !== undefined && init !== null && typeof init !== "object" && typeof init !== "function") {
      throw new TypeError("Event initialization must be a dictionary");
    }
    return {
      bubbles: !!(init && init.bubbles),
      cancelable: !!(init && init.cancelable),
      composed: !!(init && init.composed),
    };
  }
  function initializeEvent(state, type, bubbles, cancelable) {
    state.type = type;
    state.bubbles = bubbles;
    state.cancelable = cancelable;
    state.canceled = false;
    state.stopped = false;
    state.immediateStopped = false;
    state.target = null;
  }

  class Event {
    constructor(type, init = {}) {
      if (arguments.length === 0) throw new TypeError("Event requires a type");
      const name = webDOMString(type), options = eventInit(init);
      const state = eventCreate(null);
      initializeEvent(state, name, options.bubbles, options.cancelable);
      state.composed = options.composed;
      state.currentTarget = null;
      state.phase = 0;
      state.dispatching = false;
      state.inPassiveListener = false;
      state.timeStamp = host.now();
      codecApply(eventMapSet, eventStates, [this, state]);
      host.cloneAs(this, "opaque");
      eventDefineProperty(this, "isTrusted", {get() { eventOf(this); return false; }, enumerable: true});
    }
    get type() { return eventOf(this).type; }
    get bubbles() { return eventOf(this).bubbles; }
    get cancelable() { return eventOf(this).cancelable; }
    get composed() { return eventOf(this).composed; }
    get defaultPrevented() { return eventOf(this).canceled; }
    get target() { return eventOf(this).target; }
    get srcElement() { return eventOf(this).target; }
    get currentTarget() { return eventOf(this).currentTarget; }
    get eventPhase() { return eventOf(this).phase; }
    get timeStamp() { return eventOf(this).timeStamp; }
    get cancelBubble() { return eventOf(this).stopped; }
    set cancelBubble(value) { const state = eventOf(this); if (value) state.stopped = true; }
    get returnValue() { return !eventOf(this).canceled; }
    set returnValue(value) { const state = eventOf(this); if (!value && state.cancelable && !state.inPassiveListener) state.canceled = true; }
    preventDefault() { const state = eventOf(this); if (state.cancelable && !state.inPassiveListener) state.canceled = true; }
    stopPropagation() { eventOf(this).stopped = true; }
    stopImmediatePropagation() {
      const state = eventOf(this); state.stopped = true; state.immediateStopped = true;
    }
    composedPath() {
      const state = eventOf(this);
      return state.dispatching ? [state.currentTarget] : [];
    }
    initEvent(type, bubbles = false, cancelable = false) {
      const state = eventOf(this);
      if (arguments.length === 0) throw new TypeError("initEvent requires a type");
      const name = webDOMString(type);
      if (!state.dispatching) initializeEvent(state, name, !!bubbles, !!cancelable);
    }
  }
  for (const name of ["type", "bubbles", "cancelable", "composed", "defaultPrevented", "target",
    "srcElement", "currentTarget", "eventPhase", "timeStamp", "cancelBubble", "returnValue"]) {
    Object.defineProperty(Event.prototype, name, {enumerable: true});
  }
  Object.defineProperty(Event.prototype, Symbol.toStringTag, {value: "Event", configurable: true});
  for (const [name, value] of [["NONE", 0], ["CAPTURING_PHASE", 1], ["AT_TARGET", 2], ["BUBBLING_PHASE", 3]]) {
    Object.defineProperty(Event, name, {value, enumerable: true});
    Object.defineProperty(Event.prototype, name, {value, enumerable: true});
  }

  class CustomEvent extends Event {
    constructor(type, init = {}) {
      if (arguments.length === 0) throw new TypeError("CustomEvent requires a type");
      const name = webDOMString(type), options = eventInit(init);
      const value = init == null ? undefined : init.detail;
      super(name, options);
      const state = eventCreate(null);
      state.detail = value === undefined ? null : value;
      codecApply(eventMapSet, customEventStates, [this, state]);
      host.cloneAs(this, "unsupported");
    }
    get detail() { return customEventOf(this).detail; }
    initCustomEvent(type, bubbles = false, cancelable = false, detail = null) {
      const own = customEventOf(this), state = eventOf(this);
      if (arguments.length === 0) throw new TypeError("initCustomEvent requires a type");
      const name = webDOMString(type);
      if (!state.dispatching) {
        initializeEvent(state, name, !!bubbles, !!cancelable);
        own.detail = detail;
      }
    }
  }
  Object.defineProperty(CustomEvent.prototype, "detail", {enumerable: true});
  Object.defineProperty(CustomEvent.prototype, Symbol.toStringTag, {value: "CustomEvent", configurable: true});

  const eventTargetStates = new WeakMap();
  function initializeEventTarget(target) {
    if (!eventTargetStates.has(target)) eventTargetStates.set(target, new Map());
  }
  function targetListeners(target) {
    const listeners = eventTargetStates.get(target);
    if (listeners === undefined) throw new TypeError("The receiver is not an EventTarget");
    return listeners;
  }
  function eventCallback(value) {
    if (value == null) return null;
    if (typeof value !== "object" && typeof value !== "function") {
      throw new TypeError("An event listener must be an object or null");
    }
    return value;
  }
  function listenerOptions(value, adding) {
    const options = {capture: false, once: false, passive: false, signal: null};
    if (value == null) return options;
    if (typeof value !== "object" && typeof value !== "function") {
      options.capture = !!value;
      return options;
    }
    options.capture = !!value.capture;
    if (adding) {
      // Inherited dictionary members precede lexicographically ordered members.
      options.once = !!value.once;
      options.passive = !!value.passive;
      const signal = value.signal;
      if (signal !== undefined) {
        if (!abortSignalData.has(signal)) throw new TypeError("signal must be an AbortSignal");
        options.signal = signal;
      }
    }
    return options;
  }
  function removeEventListenerRecord(target, type, listener) {
    if (listener.removed) return;
    listener.removed = true;
    const listeners = targetListeners(target), list = listeners.get(type);
    const at = list.indexOf(listener);
    if (at >= 0) list.splice(at, 1);
    if (list.length === 0) listeners.delete(type);
    changeAbortListenerCount(target, type, -1);
    if (listener.signal !== null) {
      const data = abortSignalData.get(listener.signal);
      data.algorithms.delete(listener.abortStep);
      updateAbortSignalRetention(listener.signal);
      listener.abortStep = null;
      listener.signal = null;
    }
  }
  function invokeEventListeners(target, event, state, capture) {
    if (state.stopped) return;
    // Each phase takes a snapshot; removed records remain visibly removed.
    const list = (targetListeners(target).get(state.type) || []).slice();
    for (const listener of list) {
      if (state.immediateStopped) break;
      if (listener.removed || listener.capture !== capture) continue;
      if (listener.once) removeEventListenerRecord(target, state.type, listener);
      state.inPassiveListener = listener.passive;
      try {
        if (typeof listener.fn === "function") codecApply(listener.fn, target, [event]);
        else codecApply(listener.fn.handleEvent, listener.fn, [event]);
      } catch (error) {
        // Keep the runtime's asynchronous listener exception reporting path.
        queueMicrotask(() => { throw error; });
      } finally {
        state.inPassiveListener = false;
      }
    }
  }
  class EventTarget {
    constructor() {
      host.cloneAs(this, "opaque");
      initializeEventTarget(this);
    }
    addEventListener(type, fn, options = undefined) {
      const listeners = targetListeners(this);
      if (arguments.length < 2) throw new TypeError("addEventListener requires a type and listener");
      const key = webDOMString(type), callback = eventCallback(fn);
      const normalized = listenerOptions(options, true);
      if (callback === null || (normalized.signal !== null && abortSignalData.get(normalized.signal).aborted)) return;
      let list = listeners.get(key);
      if (list && list.some(listener => listener.fn === callback && listener.capture === normalized.capture)) return;
      if (!list) listeners.set(key, list = []);
      const listener = {fn: callback, ...normalized, removed: false, abortStep: null};
      list.push(listener);
      changeAbortListenerCount(this, key, 1);
      if (listener.signal !== null) {
        listener.abortStep = () => removeEventListenerRecord(this, key, listener);
        abortSignalData.get(listener.signal).algorithms.add(listener.abortStep);
        updateAbortSignalRetention(listener.signal);
      }
    }
    removeEventListener(type, fn, options = undefined) {
      const listeners = targetListeners(this);
      if (arguments.length < 2) throw new TypeError("removeEventListener requires a type and listener");
      const key = webDOMString(type), callback = eventCallback(fn);
      const capture = listenerOptions(options, false).capture;
      const list = listeners.get(key);
      if (!list) return;
      const listener = list.find(item => item.fn === callback && item.capture === capture);
      if (listener) removeEventListenerRecord(this, key, listener);
    }
    dispatchEvent(event) {
      targetListeners(this);
      const state = eventOf(event);
      if (state.dispatching) throw new DOMException("The event is already being dispatched", "InvalidStateError");
      state.dispatching = true;
      state.target = this;
      state.currentTarget = this;
      state.phase = 2;
      try {
        const dispatchAllowed = !state.stopped;
        invokeEventListeners(this, event, state, true);
        const bubbleAllowed = !state.stopped;
        invokeEventListeners(this, event, state, false);
        // Property handlers managed by setHandler are already in the list.
        if (dispatchAllowed && bubbleAllowed && !state.immediateStopped && !(eventHandlers.get(this) || new Map()).has(state.type)) {
          const on = this["on" + state.type];
          if (typeof on === "function") {
            try { on.call(this, event); } catch (e) { queueMicrotask(() => { throw e; }); }
          }
        }
        return !state.canceled;
      } finally {
        state.currentTarget = null;
        state.phase = 0;
        state.dispatching = false;
        state.inPassiveListener = false;
        state.stopped = false;
        state.immediateStopped = false;
      }
    }
  }
  const addListener = EventTarget.prototype.addEventListener;
  const removeListener = EventTarget.prototype.removeEventListener;
  const dispatch = EventTarget.prototype.dispatchEvent;
  for (const name of ["addEventListener", "removeEventListener", "dispatchEvent"]) {
    Object.defineProperty(EventTarget.prototype, name, {enumerable: true});
  }
  Object.defineProperty(EventTarget.prototype, Symbol.toStringTag, {value: "EventTarget", configurable: true});

  // An event handler -- onmessage and the like -- is a listener that runs
  // where it was first set among the others, calling whatever the handler is
  // then, as the DOM has it; setting null takes it out.
  const eventHandlers = new WeakMap();
  function getHandler(target, type) {
    const h = (eventHandlers.get(target) || new Map()).get(type);
    return h ? h.fn : null;
  }
  function setHandler(target, type, fn) {
    let handlers = eventHandlers.get(target);
    if (!handlers) eventHandlers.set(target, handlers = new Map());
    let h = handlers.get(type);
    if (fn === null || (typeof fn !== "object" && typeof fn !== "function")) {
      if (h) {
        removeListener.call(target, type, h.listener);
        handlers.delete(type);
      }
      return;
    }
    if (h) {
      h.fn = fn;
      return;
    }
    h = {fn, listener: null};
    h.listener = function (event) {
      // LegacyTreatNonObjectAsNull retains non-callable objects as inert slots.
      if (typeof h.fn === "function") return codecApply(h.fn, this, [event]);
    };
    handlers.set(type, h);
    addListener.call(target, type, h.listener);
  }

  // Separately installed Web interfaces use the same handler bookkeeping as
  // EventTarget. The internal symbol keeps callbacks in registration order
  // without hiding on* getters or duplicating the dispatcher's fallback.
  Object.defineProperty(EventTarget, Symbol.for("noe.stdlib.eventHandlers"), {
    value: Object.freeze({get: getHandler, set: setHandler, initialize: initializeEventTarget}),
  });

  const abortSignalData = new WeakMap(), abortControllerData = new WeakMap();
  const abortSignalToken = {};
  const abortTrunc = Math.trunc, abortFinite = Number.isFinite;
  const abortSignalFinalizer = new FinalizationRegistry(links => {
    for (const link of links) {
      const source = link.source.deref();
      if (!source) continue;
      const data = abortSignalData.get(source);
      if (data) data.dependents.delete(link.dependent);
    }
  });

  function retainAbortSignal(signal) {
    const data = abortSignalData.get(signal);
    if (!data || !data.dependent || data.retained ||
        (data.abortListenerCount === 0 && data.algorithms.size === 0) || data.links.length === 0) return;
    data.retained = true;
    for (const link of data.links) {
      const source = link.source.deref();
      const sourceData = source && abortSignalData.get(source);
      if (sourceData) sourceData.retainedDependents.add(signal);
    }
  }

  function releaseAbortSignal(signal) {
    const data = abortSignalData.get(signal);
    if (!data || !data.retained) return;
    data.retained = false;
    for (const link of data.links) {
      const source = link.source.deref();
      const sourceData = source && abortSignalData.get(source);
      if (sourceData) sourceData.retainedDependents.delete(signal);
    }
  }

  function changeAbortListenerCount(target, type, delta) {
    if (type !== "abort") return;
    const data = abortSignalData.get(target);
    if (!data || !data.dependent) return;
    data.abortListenerCount = Math.max(0, data.abortListenerCount + delta);
    updateAbortSignalRetention(target);
  }

  function updateAbortSignalRetention(signal) {
    const data = abortSignalData.get(signal);
    if (!data || !data.dependent) return;
    if (data.abortListenerCount > 0 || data.algorithms.size > 0) retainAbortSignal(signal);
    else releaseAbortSignal(signal);
  }

  function unlinkAbortSignal(signal) {
    const data = abortSignalData.get(signal);
    if (!data || data.links.length === 0) return;
    releaseAbortSignal(signal);
    const links = data.links;
    data.links = [];
    data.sources = [];
    abortSignalFinalizer.unregister(signal);
    for (const link of links) {
      const source = link.source.deref();
      const sourceData = source && abortSignalData.get(source);
      if (sourceData) sourceData.dependents.delete(link.dependent);
    }
  }

  function runAbortSteps(signal) {
    const data = abortSignalData.get(signal);
    for (const algorithm of data.algorithms) algorithm();
    data.algorithms.clear();
    updateAbortSignalRetention(signal);
    dispatch.call(signal, new Event("abort"));
  }

  function signalAbort(signal, reason) {
    const data = abortSignalData.get(signal);
    if (data.aborted) return;
    data.aborted = true;
    data.reason = reason;
    const dependents = [];
    if (data) {
      for (const reference of Array.from(data.dependents)) {
        const dependent = reference.deref();
        if (!dependent) {
          data.dependents.delete(reference);
          continue;
        }
        const dependentData = abortSignalData.get(dependent);
        if (!dependentData.aborted) {
          dependentData.aborted = true;
          dependentData.reason = reason;
          dependents.push(dependent);
        }
        unlinkAbortSignal(dependent);
      }
      data.dependents.clear();
    }

    // Composite state is visible before source abort listeners run. Composite
    // events follow the source event, matching dependent-signal abort ordering.
    let dispatchFailed = false;
    let dispatchError;
    const dispatchSignal = target => {
      try { runAbortSteps(target); }
      catch (error) {
        if (!dispatchFailed) {
          dispatchFailed = true;
          dispatchError = error;
        }
      }
    };
    dispatchSignal(signal);
    for (const dependent of dependents) dispatchSignal(dependent);
    if (dispatchFailed) throw dispatchError;
  }

  class AbortSignal extends EventTarget {
    constructor(...args) {
      if (args[0] !== abortSignalToken) throw new TypeError("Illegal constructor");
      super();
      abortSignalData.set(this, {
        aborted: false,
        reason: undefined,
        algorithms: new Set(),
        dependent: false,
        sources: [],
        dependents: new Set(),
        retainedDependents: new Set(),
        links: [],
        abortListenerCount: 0,
        retained: false,
      });
    }
    get aborted() { return abortSignalOf(this).aborted; }
    get reason() { return abortSignalOf(this).reason; }
    get onabort() { abortSignalOf(this); return getHandler(this, "abort"); }
    set onabort(value) { abortSignalOf(this); setHandler(this, "abort", value); }
    throwIfAborted() { const data = abortSignalOf(this); if (data.aborted) throw data.reason; }
    static abort(reason = undefined) {
      const s = new AbortSignal(abortSignalToken);
      const data = abortSignalData.get(s);
      data.aborted = true;
      data.reason = reason === undefined ? new DOMException("This operation was aborted", "AbortError") : reason;
      return s;
    }
    static any(signals) {
      if (signals === null || (typeof signals !== "object" && typeof signals !== "function")) {
        throw new TypeError("AbortSignal.any requires an iterable object");
      }
      const inputs = [];
      for (const signal of signals) {
        if (!abortSignalData.has(signal)) throw new TypeError("AbortSignal.any requires AbortSignal values");
        inputs.push(signal);
      }

      const result = new AbortSignal(abortSignalToken);
      for (const signal of inputs) {
        const data = abortSignalData.get(signal);
        if (data.aborted) {
          const resultData = abortSignalData.get(result);
          resultData.aborted = true;
          resultData.reason = data.reason;
          return result;
        }
      }

      const resultData = abortSignalData.get(result);
      resultData.dependent = true;
      const sources = new Set();
      for (const signal of inputs) {
        const inputData = abortSignalData.get(signal);
        if (inputData.dependent) {
          for (const reference of inputData.sources) {
            const source = reference.deref();
            if (source) sources.add(source);
          }
        } else {
          sources.add(signal);
        }
      }

      const links = [];
      for (const source of sources) {
        const sourceData = abortSignalData.get(source);
        const sourceReference = new WeakRef(source);
        const dependentReference = new WeakRef(result);
        sourceData.dependents.add(dependentReference);
        resultData.sources.push(sourceReference);
        links.push({source: sourceReference, dependent: dependentReference});
      }
      resultData.links = links;
      if (links.length > 0) abortSignalFinalizer.register(result, links, result);
      return result;
    }
    static timeout(ms) {
      if (arguments.length === 0) throw new TypeError("timeout requires milliseconds");
      let delay = +ms;
      if (!abortFinite(delay)) throw new TypeError("Invalid timeout milliseconds");
      delay = abortTrunc(delay);
      if (delay < 0 || delay > 9007199254740991) throw new TypeError("Timeout milliseconds is outside the Web IDL integer range");
      const s = new AbortSignal(abortSignalToken);
      host.abortTimeout(() => {
        signalAbort(s, new DOMException("The operation timed out", "TimeoutError"));
      }, delay);
      return s;
    }
  }

  function abortSignalOf(signal) {
    const data = abortSignalData.get(signal);
    if (!data) throw new TypeError("The receiver is not an AbortSignal");
    return data;
  }
  for (const name of ["aborted", "reason", "onabort", "throwIfAborted"]) {
    Object.defineProperty(AbortSignal.prototype, name, {enumerable: true});
  }
  Object.defineProperty(AbortSignal.prototype, Symbol.toStringTag, {value: "AbortSignal", configurable: true});
  for (const name of ["abort", "any", "timeout"]) Object.defineProperty(AbortSignal, name, {enumerable: true});

  class AbortController {
    constructor() { host.cloneAs(this, "opaque"); abortControllerData.set(this, new AbortSignal(abortSignalToken)); }
    get signal() { return abortControllerSignal(this); }
    abort(reason = undefined) {
      const s = abortControllerSignal(this);
      if (abortSignalOf(s).aborted) return;
      signalAbort(s, reason === undefined ? new DOMException("This operation was aborted", "AbortError") : reason);
    }
  }
  function abortControllerSignal(controller) {
    const signal = abortControllerData.get(controller);
    if (signal === undefined) throw new TypeError("The receiver is not an AbortController");
    return signal;
  }
  for (const name of ["signal", "abort"]) Object.defineProperty(AbortController.prototype, name, {enumerable: true});
  Object.defineProperty(AbortController.prototype, Symbol.toStringTag, {value: "AbortController", configurable: true});

  // --- DOMException --------------------------------------------------------

  // The names a DOMException has a legacy code for, and the constants the
  // interface has for the codes.
  const domExceptionCodes = {
    IndexSizeError: 1, HierarchyRequestError: 3, WrongDocumentError: 4,
    InvalidCharacterError: 5, NoModificationAllowedError: 7, NotFoundError: 8,
    NotSupportedError: 9, InUseAttributeError: 10, InvalidStateError: 11,
    SyntaxError: 12, InvalidModificationError: 13, NamespaceError: 14,
    InvalidAccessError: 15, TypeMismatchError: 17, SecurityError: 18,
    NetworkError: 19, AbortError: 20, URLMismatchError: 21,
    QuotaExceededError: 22, TimeoutError: 23, InvalidNodeTypeError: 24,
    DataCloneError: 25,
  };
  const domExceptionConstants = [
    "INDEX_SIZE_ERR", "DOMSTRING_SIZE_ERR", "HIERARCHY_REQUEST_ERR",
    "WRONG_DOCUMENT_ERR", "INVALID_CHARACTER_ERR", "NO_DATA_ALLOWED_ERR",
    "NO_MODIFICATION_ALLOWED_ERR", "NOT_FOUND_ERR", "NOT_SUPPORTED_ERR",
    "INUSE_ATTRIBUTE_ERR", "INVALID_STATE_ERR", "SYNTAX_ERR",
    "INVALID_MODIFICATION_ERR", "NAMESPACE_ERR", "INVALID_ACCESS_ERR",
    "VALIDATION_ERR", "TYPE_MISMATCH_ERR", "SECURITY_ERR", "NETWORK_ERR",
    "ABORT_ERR", "URL_MISMATCH_ERR", "QUOTA_EXCEEDED_ERR", "TIMEOUT_ERR",
    "INVALID_NODE_TYPE_ERR", "DATA_CLONE_ERR",
  ];

  const domState = new WeakMap();
  function domOf(e) {
    const d = domState.get(e);
    if (d === undefined) throw new TypeError('Value of "this" must be of DOMException');
    return d;
  }

  class DOMException extends Error {
    constructor(message = "", options = "Error") {
      super();
      let name = "Error";
      if (options !== null && typeof options === "object") {
        if (options.name !== undefined) name = String(options.name);
        if ("cause" in options) {
          Object.defineProperty(this, "cause", {value: options.cause, writable: true, configurable: true});
        }
      } else {
        name = String(options);
      }
      domState.set(this, {name, message: String(message)});
    }
    get name() { return domOf(this).name; }
    get message() { return domOf(this).message; }
    get code() { return domExceptionCodes[domOf(this).name] || 0; }
  }
  for (const key of ["name", "message", "code"]) {
    Object.defineProperty(DOMException.prototype, key, {enumerable: true});
  }
  Object.defineProperty(DOMException.prototype, Symbol.toStringTag, {value: "DOMException", configurable: true});
  domExceptionConstants.forEach((name, i) => {
    const d = {value: i + 1, enumerable: true};
    Object.defineProperty(DOMException, name, d);
    Object.defineProperty(DOMException.prototype, name, d);
  });

  // newDOMException is a DOMException Go makes: a clone's, whose stack is the
  // original's, or the DataCloneError a clone throws.
  function newDOMException(message, name, stack) {
    const e = new DOMException(message, name);
    if (stack !== undefined) {
      Object.defineProperty(e, "stack", {value: stack, writable: true, configurable: true});
    }
    return e;
  }

  // --- Messaging -----------------------------------------------------------

  const messageEventState = new WeakMap();
  function messageEventOf(e) {
    const d = messageEventState.get(e);
    if (d === undefined) throw new TypeError('Value of "this" must be of type MessageEvent');
    return d;
  }

  class MessageEvent extends Event {
    constructor(type, init = {}) {
      super(type, init);
      host.cloneAs(this, "unsupported");
      const {data = null, origin = "", lastEventId = "", source = null, ports = []} = init || {};
      messageEventState.set(this, {
        data, origin: String(origin), lastEventId: String(lastEventId), source,
        ports: Object.freeze(messagePorts(ports)),
      });
    }
    get data() { return messageEventOf(this).data; }
    get origin() { return messageEventOf(this).origin; }
    get lastEventId() { return messageEventOf(this).lastEventId; }
    get source() { return messageEventOf(this).source; }
    get ports() { return messageEventOf(this).ports; }
  }

  // messagePorts checks a MessageEvent's ports, which have to be ports, with
  // Node's messages.
  function messagePorts(ports) {
    const show = (v) => typeof v === "string" ? JSON.stringify(v)
      : v !== null && typeof v === "object"
        ? (Array.isArray(v) ? (v.length ? "[...]" : "[]") : (Object.keys(v).length ? "{...}" : "{}"))
        : String(v);
    if (ports === null || typeof ports !== "object") {
      throw new TypeError("MessageEvent constructor: eventInitDict.ports (" + show(ports) + ") is not iterable.");
    }
    if (typeof ports[Symbol.iterator] !== "function") {
      throw new TypeError("MessageEvent constructor: eventInitDict.ports is not iterable.");
    }
    const list = [...ports];
    list.forEach((p, i) => {
      if (!portIds.has(p)) {
        throw new TypeError("MessageEvent constructor: Expected eventInitDict.ports[" + i + "] (\"" + show(p) +
          "\") to be an instance of MessagePort.");
      }
    });
    return list;
  }

  // A port's id is what Go knows it by.
  const portIds = new WeakMap();
  const makingPort = Symbol("makingPort");
  function portIdOf(port) {
    const id = portIds.get(port);
    if (id === undefined) throw new TypeError('Value of "this" must be of type MessagePort');
    return id;
  }

  // transferList reads postMessage's second argument: a list, or an object
  // with one as its transfer.
  function transferList(options) {
    if (options === undefined || options === null) return [];
    if (typeof options === "object" && typeof options[Symbol.iterator] !== "function") {
      options = options.transfer;
      if (options === undefined) return [];
    }
    if (options === null || typeof options !== "object" || typeof options[Symbol.iterator] !== "function") {
      const e = new TypeError("Optional transferList argument must be an iterable");
      e.code = "ERR_INVALID_ARG_TYPE";
      throw e;
    }
    return [...options];
  }

  class MessagePort extends EventTarget {
    constructor(token, id) {
      if (token !== makingPort) {
        const e = new TypeError("Constructor cannot be called");
        e.code = "ERR_CONSTRUCT_CALL_INVALID";
        throw e;
      }
      super();
      host.cloneAs(this, "host");
      portIds.set(this, id);
    }
    postMessage(message, transfer = undefined) {
      host.portPost(portIdOf(this), message, transferList(transfer));
    }
    start() { host.portStart(portIdOf(this)); }
    close() { host.portClose(portIdOf(this)); }
    ref() { host.portRef(portIdOf(this), true); return this; }
    unref() { host.portRef(portIdOf(this), false); return this; }
    hasRef() { return host.portHasRef(portIdOf(this)); }
    get onmessage() { portIdOf(this); return getHandler(this, "message"); }
    set onmessage(fn) {
      portIdOf(this);
      // Setting the handler starts the port, as start() would.
      setHandler(this, "message", fn);
      if (typeof fn === "function") this.start();
    }
    get onmessageerror() { portIdOf(this); return getHandler(this, "messageerror"); }
    set onmessageerror(fn) {
      portIdOf(this);
      setHandler(this, "messageerror", fn);
    }

    // Node's ports are its event emitters too, whose listeners are given a
    // message's data rather than its event -- which is how parentPort is
    // listened to -- and a message listener starts the port.
    on(type, fn) { return this.addListener(type, fn); }
    addListener(type, fn) { nodeListen(this, type, fn, false); return this; }
    once(type, fn) { nodeListen(this, type, fn, true); return this; }
    off(type, fn) { return this.removeListener(type, fn); }
    removeListener(type, fn) {
      const list = nodeListeners.get(this) || [];
      const at = list.findIndex((l) => l.type === String(type) && l.fn === fn);
      if (at >= 0) {
        this.removeEventListener(list[at].type, list[at].listener);
        list.splice(at, 1);
      }
      return this;
    }
    removeAllListeners(type) {
      const list = nodeListeners.get(this) || [];
      for (const l of list.slice()) {
        if (type === undefined || l.type === String(type)) this.removeListener(l.type, l.fn);
      }
      return this;
    }
  }
  Object.defineProperty(MessagePort.prototype, Symbol.toStringTag, {value: "MessagePort", configurable: true});

  const nodeListeners = new WeakMap();
  function nodeListen(port, type, fn, once) {
    type = String(type);
    const call = type === "message" || type === "messageerror" ? (e) => fn.call(port, e.data)
      : type === "close" ? () => fn.call(port) : (e) => fn.call(port, e);
    const listener = once ? (e) => { port.removeListener(type, fn); call(e); } : call;
    let list = nodeListeners.get(port);
    if (!list) nodeListeners.set(port, list = []);
    list.push({type, fn, listener});
    port.addEventListener(type, listener);
    if (type === "message") port.start();
  }

  class MessageChannel {
    constructor() {
      const [port1, port2] = host.channel();
      Object.defineProperty(this, "port1", {value: port1, enumerable: true});
      Object.defineProperty(this, "port2", {value: port2, enumerable: true});
    }
  }

  // hostKind says what Go needs to know of an object it clones: whether it is
  // a port, which has to be transferred, or a DOMException, which clones as
  // one.
  function hostKind(v) {
    const id = portIds.get(v);
    if (id !== undefined) return ["port", id];
    if (blobs.has(v)) {
      // A File is a Blob with a name and a date, which it keeps.
      const file = v instanceof File;
      return ["blob", v._bytes, v.type, file, file ? v.name : "", file ? v.lastModified : 0];
    }
    const d = domState.get(v);
    if (d !== undefined) {
      const stack = v.stack;
      return ["dom", d.name, d.message, typeof stack === "string" ? stack : ""];
    }
    return undefined;
  }

  // deliver dispatches what arrived for a port. An exception a listener
  // throws is uncaught, as one from a timer is.
  function deliver(port, type, data, ports) {
    // A message is dispatched as the port's own, not through a dispatchEvent
    // the script may have put on it; its close is, as in Node.
    if (type === "close") port.dispatchEvent(new Event("close"));
    else dispatch.call(port, new MessageEvent(type, {data, ports}));
  }

  // receiveMessageOnPort takes the next message waiting on a port, if there
  // is one, as node's worker_threads does.
  function receiveMessageOnPort(port) {
    const id = portIds.get(port);
    if (id === undefined) {
      const e = new TypeError('The "port" argument must be a MessagePort instance');
      e.code = "ERR_INVALID_ARG_TYPE";
      throw e;
    }
    const got = host.portReceive(id);
    return got === null ? undefined : {message: got[0]};
  }

  // makeBlob makes the Blob, or the File, that one cloned from another stands
  // for.
  function makeBlob(bytes, type, file, name, lastModified) {
    return file ? new File([bytes], name, {type, lastModified}) : new Blob([bytes], {type});
  }

  host.bindMessaging((id) => new MessagePort(makingPort, id), newDOMException, hostKind, deliver,
    receiveMessageOnPort, makeBlob);

  // --- BroadcastChannel ----------------------------------------------------

  const broadcastState = new WeakMap();
  function broadcastOf(bc) {
    const s = broadcastState.get(bc);
    if (s === undefined) throw new TypeError('Value of "this" must be of type BroadcastChannel');
    return s;
  }

  // A BroadcastChannel hears what the others of its name -- in this runtime
  // and in its workers -- post.
  class BroadcastChannel extends EventTarget {
    constructor(name) {
      if (arguments.length === 0) {
        const e = new TypeError('The "name" argument must be specified');
        e.code = "ERR_MISSING_ARGS";
        throw e;
      }
      super();
      const s = {name: String(name)};
      broadcastState.set(this, s);
      s.id = host.broadcastOpen(s.name, this);
    }
    get name() { return broadcastOf(this).name; }
    postMessage(message) {
      const s = broadcastOf(this);
      if (arguments.length === 0) {
        const e = new TypeError('The "message" argument must be specified');
        e.code = "ERR_MISSING_ARGS";
        throw e;
      }
      host.broadcastPost(s.id, message);
    }
    close() { host.portClose(broadcastOf(this).id); }
    ref() { host.portRef(broadcastOf(this).id, true); return this; }
    unref() { host.portRef(broadcastOf(this).id, false); return this; }
    get onmessage() { broadcastOf(this); return getHandler(this, "message"); }
    set onmessage(fn) { broadcastOf(this); setHandler(this, "message", fn); }
    get onmessageerror() { broadcastOf(this); return getHandler(this, "messageerror"); }
    set onmessageerror(fn) { broadcastOf(this); setHandler(this, "messageerror", fn); }
  }
  Object.defineProperty(BroadcastChannel.prototype, Symbol.toStringTag, {value: "BroadcastChannel", configurable: true});

  // --- Cloning -------------------------------------------------------------

  function structuredClone(value, options) {
    if (arguments.length === 0) {
      const e = new TypeError('The "value" argument must be specified');
      e.code = "ERR_MISSING_ARGS";
      throw e;
    }
    let transfer = [];
    if (options !== undefined && options !== null) {
      if (typeof options !== "object") {
        const e = new TypeError("Failed to execute 'structuredClone': Options cannot be converted to a dictionary");
        e.code = "ERR_INVALID_ARG_TYPE";
        throw e;
      }
      const list = options.transfer;
      if (list !== undefined) {
        if (list === null || typeof list !== "object" || typeof list[Symbol.iterator] !== "function") {
          const e = new TypeError("Failed to execute 'structuredClone': transfer in Options cannot be converted to sequence.");
          e.code = "ERR_INVALID_ARG_TYPE";
          throw e;
        }
        transfer = [...list];
      }
    }
    return host.clone(value, transfer);
  }

  // --- The rest ------------------------------------------------------------

  const isView = ArrayBuffer.isView;
  const reflectApply = Reflect.apply;
  const objectGetPrototypeOf = Object.getPrototypeOf;
  const objectGetOwnPropertyDescriptor = Object.getOwnPropertyDescriptor;
  const typedArrayPrototype = objectGetPrototypeOf(Uint8Array.prototype);
  const typedArrayTagGetter = objectGetOwnPropertyDescriptor(
    typedArrayPrototype, Symbol.toStringTag).get;
  const typedArrayBufferGetter = objectGetOwnPropertyDescriptor(
    typedArrayPrototype, "buffer").get;
  const typedArrayOffsetGetter = objectGetOwnPropertyDescriptor(
    typedArrayPrototype, "byteOffset").get;
  const typedArrayLengthGetter = objectGetOwnPropertyDescriptor(
    typedArrayPrototype, "byteLength").get;
  const typedArraySet = Uint8Array.prototype.set;
  const integerArrayNames = new Set([
    "Int8Array", "Uint8Array", "Uint8ClampedArray", "Int16Array",
    "Uint16Array", "Int32Array", "Uint32Array", "BigInt64Array",
    "BigUint64Array",
  ]);
  const setHas = Set.prototype.has;

  const crypto = {
    getRandomValues(view) {
      let viewName;
      if (isView(view)) {
        try { viewName = reflectApply(typedArrayTagGetter, view, []); }
        catch (_) { viewName = undefined; }
      }
      if (viewName === undefined || !reflectApply(setHas, integerArrayNames, [viewName])) {
        throw new DOMException("An integer typed array is required", "TypeMismatchError");
      }
      let buffer, offset, length;
      try {
        buffer = reflectApply(typedArrayBufferGetter, view, []);
        offset = reflectApply(typedArrayOffsetGetter, view, []);
        length = reflectApply(typedArrayLengthGetter, view, []);
      } catch (_) {
        throw new DOMException("An integer typed array is required", "TypeMismatchError");
      }
      const byteLength = length;
      if (byteLength > 65536) {
        throw new DOMException("getRandomValues may not be asked for more than 65536 bytes",
          "QuotaExceededError");
      }
      // Constructing this view detects a detached buffer, including a
      // detached zero-length view. Shared backing is allowed for this API.
      let target;
      try { target = new Uint8Array(buffer, offset, byteLength); }
      catch (_) { throw new TypeError("The array buffer is detached"); }
      const bytes = host.webcrypto.random(byteLength);
      reflectApply(typedArraySet, target, [bytes]);
      return view;
    },
    randomUUID() {
      const b = host.webcrypto.random(16);
      b[6] = (b[6] & 0x0f) | 0x40;
      b[8] = (b[8] & 0x3f) | 0x80;
      const hex = [];
      for (const x of b) hex.push(x.toString(16).padStart(2, "0"));
      const s = hex.join("");
      return s.slice(0, 8) + "-" + s.slice(8, 12) + "-" + s.slice(12, 16) + "-" +
             s.slice(16, 20) + "-" + s.slice(20);
    },
    subtle: {},
  };
  // Preserve the legacy Node-style Crypto object's opaque clone behavior.
  host.cloneAs(crypto, "opaque");
  host.cloneAs(crypto.subtle, "opaque");

  return {
    URL, URLSearchParams, TextEncoder, TextDecoder,
    atob: (s) => host.atob(String(s)),
    btoa: (s) => host.btoa(String(s)),
    structuredClone, crypto,
    AbortController, AbortSignal, Event, EventTarget, CustomEvent,
    Blob, File, FormData, URLPattern,
    DOMException, MessageChannel, MessagePort, MessageEvent, BroadcastChannel,
  };
})`
