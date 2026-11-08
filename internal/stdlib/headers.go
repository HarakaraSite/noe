package stdlib

// headersJS implements the web's Headers value and the private helpers used by
// Request and Response. It is inserted inside fetchJS's IIFE so header lists
// remain private to the network implementation.
const headersJS = `
  const headersApply = Reflect.apply;
  const headersString = String;
  const headersCharCodeAt = String.prototype.charCodeAt;
  const headersSlice = String.prototype.slice;
  const headersLowercase = String.prototype.toLowerCase;
  const headersTypeError = TypeError;
  const headersArrayPush = Array.prototype.push;
  const headersArraySort = Array.prototype.sort;
  const headersObjectCreate = Object.create;
  const headersDefineProperty = Object.defineProperty;
  const headersGetOwnPropertyDescriptor = Object.getOwnPropertyDescriptor;
  const headersOwnKeys = Reflect.ownKeys;
  const headersGetPrototypeOf = Object.getPrototypeOf;
  const headersFromEntries = Object.fromEntries;
  const headersIteratorSymbol = Symbol.iterator;
  const headersToStringTag = Symbol.toStringTag;
  const headersWeakMapGet = WeakMap.prototype.get;
  const headersWeakMapSet = WeakMap.prototype.set;
  const headerSlots = new WeakMap();
  const headerIteratorSlots = new WeakMap();
  const headerIteratorBase = headersGetPrototypeOf(
    headersGetPrototypeOf(headersApply(Array.prototype[headersIteratorSymbol], [], []))
  );
  const headerIteratorPrototype = headersObjectCreate(headerIteratorBase);

  function headerByteString(value) {
    if (typeof value === "symbol") throw new headersTypeError("A Symbol cannot be converted to a string");
    const text = headersString(value);
    for (let i = 0; i < text.length; i++) {
      if (headersApply(headersCharCodeAt, text, [i]) > 255) {
        throw new headersTypeError("Value is not a ByteString");
      }
    }
    return text;
  }

  function headerToken(name) {
    if (name.length === 0) return false;
    for (let i = 0; i < name.length; i++) {
      const c = headersApply(headersCharCodeAt, name, [i]);
      if ((c >= 0x30 && c <= 0x39) || (c >= 0x41 && c <= 0x5a) ||
          (c >= 0x61 && c <= 0x7a) ||
          c === 0x21 || c === 0x23 || c === 0x24 || c === 0x25 ||
          c === 0x26 || c === 0x27 || c === 0x2a || c === 0x2b ||
          c === 0x2d || c === 0x2e || c === 0x5e || c === 0x5f ||
          c === 0x60 || c === 0x7c || c === 0x7e) continue;
      return false;
    }
    return true;
  }

  function headerName(value) {
    const text = headerByteString(value);
    if (!headerToken(text)) throw new headersTypeError("Invalid HTTP header name");
    return headersApply(headersLowercase, text, []);
  }

  function headerValue(value) {
    const text = headerByteString(value);
    let start = 0;
    let end = text.length;
    while (start < end) {
      const c = headersApply(headersCharCodeAt, text, [start]);
      if (c !== 0x20 && c !== 0x09 && c !== 0x0a && c !== 0x0d) break;
      start++;
    }
    while (end > start) {
      const c = headersApply(headersCharCodeAt, text, [end - 1]);
      if (c !== 0x20 && c !== 0x09 && c !== 0x0a && c !== 0x0d) break;
      end--;
    }
    const normalized = headersApply(headersSlice, text, [start, end]);
    for (let i = 0; i < normalized.length; i++) {
      const c = headersApply(headersCharCodeAt, normalized, [i]);
      if (c === 0x00 || c === 0x0a || c === 0x0d) {
        throw new headersTypeError("Invalid HTTP header value");
      }
    }
    return normalized;
  }

  function headerState(value) {
    const state = headersApply(headersWeakMapGet, headerSlots, [value]);
    if (state === undefined) throw new headersTypeError("Illegal invocation");
    return state;
  }

  function headerGuard(state) {
    if (state.guard === "immutable") throw new headersTypeError("Headers are immutable");
  }

  function headerAppend(state, name, value) {
    headerGuard(state);
    headersApply(headersArrayPush, state.list, [[name, value]]);
  }

  function headerHasName(state, name) {
    for (let i = 0; i < state.list.length; i++) {
      if (state.list[i][0] === name) return true;
    }
    return false;
  }

  function headerGetValue(state, name) {
    let result = null;
    for (let i = 0; i < state.list.length; i++) {
      const pair = state.list[i];
      if (pair[0] !== name) continue;
      result = result === null ? pair[1] : result + ", " + pair[1];
    }
    return result;
  }

  function headerPairsForIteration(headers) {
    const state = headerState(headers);
    const names = [];
    for (let i = 0; i < state.list.length; i++) {
      const name = state.list[i][0];
      let found = false;
      for (let j = 0; j < names.length; j++) {
        if (names[j] === name) { found = true; break; }
      }
      if (!found) headersApply(headersArrayPush, names, [name]);
    }
    headersApply(headersArraySort, names, [(a, b) => a < b ? -1 : a > b ? 1 : 0]);

    const pairs = [];
    for (let i = 0; i < names.length; i++) {
      const name = names[i];
      if (name === "set-cookie") {
        for (let j = 0; j < state.list.length; j++) {
          const pair = state.list[j];
          if (pair[0] === name) headersApply(headersArrayPush, pairs, [[name, pair[1]]]);
        }
      } else {
        headersApply(headersArrayPush, pairs, [[name, headerGetValue(state, name)]]);
      }
    }
    return pairs;
  }

  function headerInitIteratorMethod(value) {
    const method = value[headersIteratorSymbol];
    if (method === null || method === undefined) return undefined;
    if (typeof method !== "function") throw new headersTypeError("Iterator method is not callable");
    return method;
  }

  // Web IDL sequence conversion is deliberately manual. In particular, an
  // element conversion error does not close the iterator with its return().
  function headerSequence(value, method, convert) {
    if ((typeof value !== "object" && typeof value !== "function") || value === null) {
      throw new headersTypeError("Value is not an object");
    }
    const iterator = headersApply(method, value, []);
    if ((typeof iterator !== "object" && typeof iterator !== "function") || iterator === null) {
      throw new headersTypeError("Iterator is not an object");
    }
    const next = iterator.next;
    if (typeof next !== "function") throw new headersTypeError("Iterator next is not callable");
    const values = [];
    while (true) {
      const result = headersApply(next, iterator, []);
      if ((typeof result !== "object" && typeof result !== "function") || result === null) {
        throw new headersTypeError("Iterator result is not an object");
      }
      if (!!result.done) return values;
      headersApply(headersArrayPush, values, [convert(result.value)]);
    }
  }

  function headerPairSequence(value) {
    if ((typeof value !== "object" && typeof value !== "function") || value === null) {
      throw new headersTypeError("Header pair is not an object");
    }
    const method = headerInitIteratorMethod(value);
    if (method === undefined) throw new headersTypeError("Header pair is not iterable");
    return headerSequence(value, method, headerByteString);
  }

  function headerRecord(value) {
    if ((typeof value !== "object" && typeof value !== "function") || value === null) {
      throw new headersTypeError("HeadersInit must be an object or iterable");
    }
    const keys = headersApply(headersOwnKeys, undefined, [value]);
    const pairs = [];
    for (let i = 0; i < keys.length; i++) {
      const key = keys[i];
      const descriptor = headersApply(headersGetOwnPropertyDescriptor, undefined, [value, key]);
      if (descriptor === undefined || !descriptor.enumerable) continue;
      const name = headerByteString(key);
      const item = value[key];
      const headerValueText = headerByteString(item);
      headersApply(headersArrayPush, pairs, [[name, headerValueText]]);
    }
    return pairs;
  }

  function headerInit(init) {
    if (init === undefined) return undefined;
    if ((typeof init === "object" && init !== null) || typeof init === "function") {
      const method = headerInitIteratorMethod(init);
      if (method !== undefined) return headerSequence(init, method, headerPairSequence);
      return headerRecord(init);
    }
    throw new headersTypeError("HeadersInit must be an object or iterable");
  }

  function headerFill(state, pairs) {
    for (let i = 0; i < pairs.length; i++) {
      const pair = pairs[i];
      if (pair.length !== 2) throw new headersTypeError("Each header pair must contain exactly two items");
      const name = headerName(pair[0]);
      const value = headerValue(pair[1]);
      headerAppend(state, name, value);
    }
  }

  function headerSnapshot(headers) {
    const list = headerState(headers).list;
    const pairs = [];
    for (let i = 0; i < list.length; i++) {
      headersApply(headersArrayPush, pairs, [[list[i][0], list[i][1]]]);
    }
    return pairs;
  }

  function headerImmutable(headers) {
    headerState(headers).guard = "immutable";
  }

  function headerIteratorNext() {
    const state = headersApply(headersWeakMapGet, headerIteratorSlots, [this]);
    if (state === undefined) throw new headersTypeError("Illegal invocation");
    const pairs = headerPairsForIteration(state.target);
    if (state.index >= pairs.length) return {value: undefined, done: true};
    const pair = pairs[state.index];
    state.index++;
    if (state.kind === "key") return {value: pair[0], done: false};
    if (state.kind === "value") return {value: pair[1], done: false};
    return {value: [pair[0], pair[1]], done: false};
  }

  headersDefineProperty(headerIteratorPrototype, "next", {
    value: headerIteratorNext, writable: true, enumerable: true, configurable: true,
  });
  headersDefineProperty(headerIteratorPrototype, headersToStringTag, {
    value: "Headers Iterator", writable: false, enumerable: false, configurable: true,
  });

  function headerMakeIterator(target, kind) {
    headerState(target);
    const iterator = headersObjectCreate(headerIteratorPrototype);
    headersApply(headersWeakMapSet, headerIteratorSlots, [iterator, {target, kind, index: 0}]);
    return iterator;
  }

  class Headers {
    constructor(init = undefined) {
      const converted = headerInit(init);
      host.cloneAs(this, "unsupported");
      const state = {list: [], guard: "none"};
      headersApply(headersWeakMapSet, headerSlots, [this, state]);
      if (converted !== undefined) headerFill(state, converted);
    }
    append(name, value) {
      const state = headerState(this);
      if (arguments.length < 2) throw new headersTypeError("append requires 2 arguments");
      const nameText = headerByteString(name);
      const valueText = headerByteString(value);
      headerAppend(state, headerName(nameText), headerValue(valueText));
    }
    delete(name) {
      const state = headerState(this);
      if (arguments.length < 1) throw new headersTypeError("delete requires 1 argument");
      const normalized = headerName(headerByteString(name));
      headerGuard(state);
      const list = [];
      for (let i = 0; i < state.list.length; i++) {
        const pair = state.list[i];
        if (pair[0] !== normalized) headersApply(headersArrayPush, list, [pair]);
      }
      state.list = list;
    }
    get(name) {
      const state = headerState(this);
      if (arguments.length < 1) throw new headersTypeError("get requires 1 argument");
      return headerGetValue(state, headerName(headerByteString(name)));
    }
    getSetCookie() {
      const state = headerState(this);
      const cookies = [];
      for (let i = 0; i < state.list.length; i++) {
        const pair = state.list[i];
        if (pair[0] === "set-cookie") headersApply(headersArrayPush, cookies, [pair[1]]);
      }
      return cookies;
    }
    has(name) {
      const state = headerState(this);
      if (arguments.length < 1) throw new headersTypeError("has requires 1 argument");
      return headerHasName(state, headerName(headerByteString(name)));
    }
    set(name, value) {
      const state = headerState(this);
      if (arguments.length < 2) throw new headersTypeError("set requires 2 arguments");
      const nameText = headerByteString(name);
      const valueText = headerByteString(value);
      const normalizedName = headerName(nameText);
      const normalizedValue = headerValue(valueText);
      headerGuard(state);
      const list = [];
      let replaced = false;
      for (let i = 0; i < state.list.length; i++) {
        const pair = state.list[i];
        if (pair[0] === normalizedName) {
          if (!replaced) {
            headersApply(headersArrayPush, list, [[normalizedName, normalizedValue]]);
            replaced = true;
          }
        } else {
          headersApply(headersArrayPush, list, [pair]);
        }
      }
      if (!replaced) headersApply(headersArrayPush, list, [[normalizedName, normalizedValue]]);
      state.list = list;
    }
    forEach(callback, thisArg = undefined) {
      const state = headerState(this);
      if (arguments.length < 1) throw new headersTypeError("forEach requires a callback");
      if (typeof callback !== "function") throw new headersTypeError("callback is not callable");
      let pairs = headerPairsForIteration(this);
      let i = 0;
      while (i < pairs.length) {
        const pair = pairs[i];
        headersApply(callback, thisArg, [pair[1], pair[0], this]);
        pairs = headerPairsForIteration(this);
        i++;
      }
    }
    entries() { return headerMakeIterator(this, "key+value"); }
    keys() { return headerMakeIterator(this, "key"); }
    values() { return headerMakeIterator(this, "value"); }
    toJSON() {
      const pairs = headerPairsForIteration(this);
      const output = [];
      for (let i = 0; i < pairs.length; i++) {
        const pair = pairs[i];
        if (pair[0] === "set-cookie") {
          // Keep the historical host utility's combined-cookie object view.
          if (i === 0 || pairs[i - 1][0] !== "set-cookie") {
            headersApply(headersArrayPush, output, [[pair[0], headerGetValue(headerState(this), pair[0])]]);
          }
        } else {
          headersApply(headersArrayPush, output, [pair]);
        }
      }
      return headersApply(headersFromEntries, undefined, [output]);
    }
  }

  // Web IDL operation and iterable methods are enumerable data properties.
  for (const name of ["append", "delete", "get", "getSetCookie", "has", "set", "forEach", "entries", "keys", "values"]) {
    const descriptor = headersGetOwnPropertyDescriptor(Headers.prototype, name);
    headersDefineProperty(Headers.prototype, name, {
      value: descriptor.value, writable: true, enumerable: true, configurable: true,
    });
  }
  headersDefineProperty(Headers.prototype, headersIteratorSymbol, {
    value: Headers.prototype.entries, writable: true, enumerable: false, configurable: true,
  });
  headersDefineProperty(Headers.prototype, headersToStringTag, {
    value: "Headers", writable: false, enumerable: false, configurable: true,
  });
`
