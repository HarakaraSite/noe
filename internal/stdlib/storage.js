(function (host) {
  "use strict";

  const Object_ = Object;
  const Array_ = Array;
  const Set_ = Set;
  const WeakMap_ = WeakMap;
  const JSON_ = JSON;
  const Reflect_ = Reflect;
  const TypeError_ = TypeError;
  const DOMException_ = DOMException;
  const objectCreate = Object_.create;
  const objectDefineProperty = Object_.defineProperty;
  const objectGetOwnPropertyDescriptor = Object_.getOwnPropertyDescriptor;
  const arrayIsArray = Array_.isArray;
  const arrayJoin = Array_.prototype.join;
  const setHas = Set_.prototype.has;
  const setAdd = Set_.prototype.add;
  const weakGet = WeakMap_.prototype.get;
  const weakSet = WeakMap_.prototype.set;
  const jsonParse = JSON_.parse;
  const reflectApply = Reflect_.apply;
  const reflectDelete = Reflect_.deleteProperty;
  const reflectDefine = Reflect_.defineProperty;
  const reflectGet = Reflect_.get;
  const reflectHas = Reflect_.has;
  const reflectOwnKeys = Reflect_.ownKeys;
  const reflectSet = Reflect_.set;
  const hasOwnProperty = Object_.prototype.hasOwnProperty;
  const charCodeAt = String.prototype.charCodeAt;
  const toStringTag = Symbol.toStringTag;
  const states = new WeakMap_();
  const hasLocal = host.hasLocal;
  const transact = host.transact;
  const cloneAs = host.cloneAs;

  function apply(fn, receiver, args) {
    return reflectApply(fn, receiver, args);
  }

  function hasOwn(object, key) {
    return apply(hasOwnProperty, object, [key]);
  }

  function dataDescriptor(value) {
    const descriptor = objectCreate(null);
    descriptor.value = value;
    descriptor.writable = true;
    descriptor.enumerable = true;
    descriptor.configurable = true;
    return descriptor;
  }

  function append(array, value) {
    objectDefineProperty(array, "" + array.length, dataDescriptor(value));
  }

  function stateOf(receiver) {
    if ((typeof receiver !== "object" && typeof receiver !== "function") || receiver === null) {
      throw new TypeError_("Illegal invocation");
    }
    const state = apply(weakGet, states, [receiver]);
    if (state === undefined) throw new TypeError_("Illegal invocation");
    return state;
  }

  function required(actual, count) {
    if (actual < count) {
      throw new TypeError_(count + " argument" + (count === 1 ? "" : "s") + " required");
    }
  }

  function domString(value) {
    if (typeof value === "symbol") throw new TypeError_("Cannot convert a Symbol value to a string");
    return `${value}`;
  }

  function unsignedLong(value) {
    return (+value) >>> 0;
  }

  function find(entries, key) {
    for (let i = 0; i < entries.length; i++) {
      if (entries[i][0] === key) return i;
    }
    return -1;
  }

  function parseDocument(text) {
    let document;
    try {
      document = apply(jsonParse, JSON_, [text]);
    } catch (_) {
      return null;
    }
    if (document === null || typeof document !== "object" ||
        !hasOwn(document, "version") || !hasOwn(document, "entries") ||
        document.version !== 1 || !arrayIsArray(document.entries)) {
      return null;
    }
    const entries = document.entries;
    const seen = new Set_();
    for (let i = 0; i < entries.length; i++) {
      const pair = entries[i];
      if (!arrayIsArray(pair) || pair.length !== 2 ||
          typeof pair[0] !== "string" || typeof pair[1] !== "string" ||
          apply(setHas, seen, [pair[0]])) {
        return null;
      }
      apply(setAdd, seen, [pair[0]]);
    }
    return entries;
  }

  function quoteJSON(text) {
    const pieces = ["\""];
    const hex = "0123456789abcdef";
    for (let i = 0; i < text.length; i++) {
      const code = apply(charCodeAt, text, [i]);
      if (code === 0x22) append(pieces, "\\\"");
      else if (code === 0x5c) append(pieces, "\\\\");
      else if (code < 0x20) {
        append(pieces, "\\u" + hex[(code >>> 12) & 15] + hex[(code >>> 8) & 15] +
          hex[(code >>> 4) & 15] + hex[code & 15]);
      } else {
        // Escaping each UTF-16 code unit preserves lone surrogates verbatim.
        append(pieces, "\\u" + hex[(code >>> 12) & 15] + hex[(code >>> 8) & 15] +
          hex[(code >>> 4) & 15] + hex[code & 15]);
      }
    }
    append(pieces, "\"");
    return apply(arrayJoin, pieces, [""]);
  }

  function serializeDocument(entries) {
    const pieces = ["{\"version\":1,\"entries\":["];
    for (let i = 0; i < entries.length; i++) {
      if (i !== 0) append(pieces, ",");
      append(pieces, "[" + quoteJSON(entries[i][0]) + "," + quoteJSON(entries[i][1]) + "]");
    }
    append(pieces, "]}");
    return apply(arrayJoin, pieces, [""]);
  }

  function perform(entries, operation, key, value) {
    switch (operation) {
      case "length":
        return {value: entries.length, changed: false};
      case "keys": {
        const keys = [];
        for (let i = 0; i < entries.length; i++) append(keys, entries[i][0]);
        return {value: keys, changed: false};
      }
      case "key":
        return {value: key < entries.length ? entries[key][0] : null, changed: false};
      case "getItem": {
        const index = find(entries, key);
        return {value: index < 0 ? null : entries[index][1], changed: false};
      }
      case "getProperty": {
        const index = find(entries, key);
        return {value: index < 0 ? undefined : entries[index][1], changed: false};
      }
      case "hasProperty":
        return {value: find(entries, key) >= 0, changed: false};
      case "setItem": {
        const index = find(entries, key);
        if (index < 0) append(entries, [key, value]);
        else if (entries[index][1] === value) return {value: undefined, changed: false};
        else entries[index][1] = value;
        return {value: undefined, changed: true};
      }
      case "removeItem": {
        const index = find(entries, key);
        if (index < 0) return {value: undefined, changed: false};
        for (let i = index; i < entries.length - 1; i++) entries[i] = entries[i + 1];
        delete entries[entries.length - 1];
        entries.length--;
        return {value: undefined, changed: true};
      }
      case "clear": {
        if (entries.length === 0) return {value: undefined, changed: false};
        entries.length = 0;
        return {value: undefined, changed: true};
      }
      default:
        throw new TypeError_("Invalid storage operation");
    }
  }

  function invalidState() {
    return {value: undefined, error: "InvalidStateError"};
  }

  function run(state, operation, key, value, writing) {
    let outcome;
    if (state.persistent) {
      outcome = transact(writing, function (text) {
        const entries = parseDocument(text);
        if (entries === null) return {result: invalidState(), data: undefined};
        const applied = perform(entries, operation, key, value);
        const result = {value: applied.value, error: undefined};
        if (!applied.changed) return {result, data: undefined};
        return {result, data: serializeDocument(entries)};
      });
    } else {
      const applied = perform(state.entries, operation, key, value);
      outcome = {value: applied.value, error: undefined};
    }
    if (outcome && outcome.error === "InvalidStateError") {
      throw new DOMException_("The localStorage data is invalid", "InvalidStateError");
    }
    return outcome.value;
  }

  function createFacade(persistent) {
    const target = objectCreate(Storage.prototype);
    const proxy = new Proxy(target, {
      get(target, key, receiver) {
        if (typeof key !== "string" || reflectHas(target, key)) {
          return reflectGet(target, key, receiver);
        }
        return run(stateOf(proxy), "getProperty", key, undefined, false);
      },
      set(target, key, value, receiver) {
        if (typeof key !== "string" || receiver !== proxy) return reflectSet(target, key, value, receiver);
        const stored = domString(value);
        run(stateOf(receiver), "setItem", key, stored, true);
        return true;
      },
      deleteProperty(target, key) {
        if (typeof key !== "string") return reflectDelete(target, key);
        if (reflectHas(target, key)) return reflectDelete(target, key);
        run(stateOf(proxy), "removeItem", key, undefined, true);
        return true;
      },
      has(target, key) {
        if (typeof key !== "string" || reflectHas(target, key)) return reflectHas(target, key);
        return run(stateOf(proxy), "hasProperty", key, undefined, false);
      },
      ownKeys(target) {
        const state = stateOf(proxy);
        const named = run(state, "keys", undefined, undefined, false);
        const keys = [];
        for (let i = 0; i < named.length; i++) {
          const key = named[i];
          if (!reflectHas(target, key)) append(keys, key);
        }
        const own = reflectOwnKeys(target);
        for (let i = 0; i < own.length; i++) {
          let found = false;
          for (let j = 0; j < keys.length; j++) {
            if (keys[j] === own[i]) { found = true; break; }
          }
          if (!found) append(keys, own[i]);
        }
        return keys;
      },
      getOwnPropertyDescriptor(target, key) {
        if (typeof key !== "string" || reflectHas(target, key)) {
          return objectGetOwnPropertyDescriptor(target, key);
        }
        const value = run(stateOf(proxy), "getProperty", key, undefined, false);
        if (value === undefined) return undefined;
        const descriptor = objectCreate(null);
        descriptor.value = value;
        descriptor.writable = true;
        descriptor.enumerable = true;
        descriptor.configurable = true;
        return descriptor;
      },
      defineProperty(target, key, descriptor) {
        if (typeof key !== "string") return reflectDefine(target, key, descriptor);
        if (hasOwn(descriptor, "get") || hasOwn(descriptor, "set") || descriptor.configurable === false) return false;
        if (!hasOwn(descriptor, "value") && !hasOwn(descriptor, "writable")) return false;
        const value = domString(descriptor.value);
        run(stateOf(proxy), "setItem", key, value, true);
        return true;
      },
      preventExtensions() {
        return false;
      },
    });
    apply(weakSet, states, [proxy, {persistent, entries: persistent ? null : []}]);
    apply(cloneAs, host, [proxy, "unsupported"]);
    return proxy;
  }

  class Storage {
    constructor() {
      throw new TypeError_("Illegal constructor");
    }
    get length() {
      return run(stateOf(this), "length", undefined, undefined, false);
    }
    key(index) {
      required(arguments.length, 1);
      const state = stateOf(this);
      return run(state, "key", unsignedLong(index), undefined, false);
    }
    getItem(key) {
      required(arguments.length, 1);
      const state = stateOf(this);
      return run(state, "getItem", domString(key), undefined, false);
    }
    setItem(key, value) {
      required(arguments.length, 2);
      const state = stateOf(this);
      run(state, "setItem", domString(key), domString(value), true);
    }
    removeItem(key) {
      required(arguments.length, 1);
      const state = stateOf(this);
      run(state, "removeItem", domString(key), undefined, true);
    }
    clear() {
      const state = stateOf(this);
      run(state, "clear", undefined, undefined, true);
    }
  }

  objectDefineProperty(Storage.prototype, toStringTag, {value: "Storage", configurable: true});
  const lengthDescriptor = objectGetOwnPropertyDescriptor(Storage.prototype, "length");
  objectDefineProperty(Storage.prototype, "length", {
    get: lengthDescriptor.get, enumerable: true, configurable: true,
  });
  for (const name of ["key", "getItem", "setItem", "removeItem", "clear"]) {
    const descriptor = objectGetOwnPropertyDescriptor(Storage.prototype, name);
    objectDefineProperty(Storage.prototype, name, {
      value: descriptor.value, writable: true, enumerable: true, configurable: true,
    });
  }
  const sessionStorage = createFacade(false);
  const localStorage = hasLocal ? createFacade(true) : undefined;

  objectDefineProperty(globalThis, "Storage", {
    value: Storage, writable: true, enumerable: false, configurable: true,
  });
  objectDefineProperty(globalThis, "sessionStorage", {
    get: function sessionStorageGetter() { return sessionStorage; },
    enumerable: true, configurable: true,
  });
  if (hasLocal) {
    objectDefineProperty(globalThis, "localStorage", {
      get: function localStorageGetter() { return localStorage; },
      enumerable: true, configurable: true,
    });
  }
})
