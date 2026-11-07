(function (host) {
  "use strict";

  const native = host.native;
  const crypto = globalThis.crypto;
  const subtle = crypto.subtle;
  const DOMExceptionCtor = globalThis.DOMException;
  const TypeErrorCtor = TypeError;
  const StringCtor = String;
  const MathTrunc = Math.trunc;
  const MathCeil = Math.ceil;
  const NumberIsFinite = Number.isFinite;
  const stringCharCodeAt = String.prototype.charCodeAt;
  const stringFromCharCode = String.fromCharCode;
  const reflectApply = Reflect.apply;
  const objectDefineProperty = Object.defineProperty;
  const objectGetOwnPropertyDescriptor = Object.getOwnPropertyDescriptor;
  const objectGetPrototypeOf = Object.getPrototypeOf;
  const objectCreate = Object.create;
  const objectFreeze = Object.freeze;
  const arrayBufferIsView = ArrayBuffer.isView;
  const Uint8ArrayCtor = Uint8Array;
  const ArrayBufferCtor = ArrayBuffer;
  const DataViewCtor = DataView;
  const typedArrayPrototype = objectGetPrototypeOf(Uint8ArrayCtor.prototype);
  const typedArrayBufferGetter = objectGetOwnPropertyDescriptor(typedArrayPrototype, "buffer").get;
  const typedArrayOffsetGetter = objectGetOwnPropertyDescriptor(typedArrayPrototype, "byteOffset").get;
  const typedArrayByteLengthGetter = objectGetOwnPropertyDescriptor(typedArrayPrototype, "byteLength").get;
  const typedArraySet = Uint8ArrayCtor.prototype.set;
  const dataViewPrototype = DataViewCtor.prototype;
  const dataViewBufferGetter = objectGetOwnPropertyDescriptor(dataViewPrototype, "buffer").get;
  const dataViewOffsetGetter = objectGetOwnPropertyDescriptor(dataViewPrototype, "byteOffset").get;
  const dataViewByteLengthGetter = objectGetOwnPropertyDescriptor(dataViewPrototype, "byteLength").get;
  const arrayBufferByteLengthGetter = objectGetOwnPropertyDescriptor(ArrayBufferCtor.prototype, "byteLength").get;
  const sharedByteLengthGetter = typeof SharedArrayBuffer === "function"
    ? objectGetOwnPropertyDescriptor(SharedArrayBuffer.prototype, "byteLength").get
    : null;
  const weakMapGet = WeakMap.prototype.get;
  const weakMapSet = WeakMap.prototype.set;
  const setHas = Set.prototype.has;
  const request = native.request;
  const equal = native.equal;
  const usagesSet = new Set([
    "encrypt", "decrypt", "sign", "verify", "deriveKey", "deriveBits",
    "wrapKey", "unwrapKey",
  ]);
  const hmacUsagesSet = new Set(["sign", "verify"]);
  const derivationUsagesSet = new Set(["deriveKey", "deriveBits"]);
  const formatSet = new Set(["raw", "spki", "pkcs8", "jwk"]);
  const keyStates = new WeakMap();
  const keyToken = objectFreeze({});
  const emptyBytes = new Uint8ArrayCtor(0);
  const usageOrder = [
    "encrypt", "decrypt", "sign", "verify", "deriveKey", "deriveBits",
    "wrapKey", "unwrapKey",
  ];
  const MAX_UINT32 = 0xffffffff;

  function domException(name, message) {
    return new DOMExceptionCtor(message, name);
  }

  function toDOMString(value, label) {
    if (typeof value === "symbol") {
      throw new TypeErrorCtor(`${label} cannot be a Symbol`);
    }
    return StringCtor(value);
  }

  function asciiUpper(value) {
    let result = "";
    for (let i = 0; i < value.length; i++) {
      const code = reflectApply(stringCharCodeAt, value, [i]);
      result += code >= 0x61 && code <= 0x7a
        ? reflectApply(stringFromCharCode, StringCtor, [code - 0x20])
        : value[i];
    }
    return result;
  }

  function requiredMember(dictionary, name) {
    if (dictionary === null || dictionary === undefined) {
      throw new TypeErrorCtor(`The algorithm dictionary requires a ${name} member`);
    }
    const value = dictionary[name];
    if (value === undefined) {
      throw new TypeErrorCtor(`The algorithm dictionary requires a ${name} member`);
    }
    return value;
  }

  function algorithmName(algorithm) {
    let value;
    if (typeof algorithm === "string") {
      value = algorithm;
    } else {
      value = requiredMember(algorithm, "name");
    }
    return asciiUpper(toDOMString(value, "algorithm name"));
  }

  function enforceRangeUint32(value, label) {
    // Unary plus follows Web IDL's ToNumber conversion (including rejecting
    // BigInt), unlike Number(), which accepts BigInt values.
    const number = +value;
    if (!NumberIsFinite(number)) {
      throw new TypeErrorCtor(`${label} is outside the unsigned long range`);
    }
    const integer = MathTrunc(number);
    if (integer < 0 || integer > MAX_UINT32) {
      throw new TypeErrorCtor(`${label} is outside the unsigned long range`);
    }
    return integer;
  }

  function normalizeAlgorithm(algorithm, operation) {
    const name = algorithmName(algorithm);
    switch (name) {
      case "SHA-1":
      case "SHA-256":
      case "SHA-384":
      case "SHA-512":
        if (operation !== "digest") {
          throw domException("NotSupportedError", `${name} does not support ${operation}`);
        }
        return {name};
      case "HMAC": {
        if (operation !== "sign" && operation !== "verify" &&
            operation !== "importKey" && operation !== "exportKey" &&
            operation !== "get key length") {
          throw domException("NotSupportedError", `HMAC does not support ${operation}`);
        }
        if (operation !== "importKey" && operation !== "get key length") {
          return {name};
        }
        const hash = normalizeAlgorithm(requiredMember(algorithm, "hash"), "digest").name;
        const lengthValue = algorithm === null || algorithm === undefined
          ? undefined : algorithm.length;
        return lengthValue === undefined
          ? {name, hash, hasLength: false}
          : {name, hash, hasLength: true, length: enforceRangeUint32(lengthValue, "length")};
      }
      case "PBKDF2":
      case "HKDF": {
        if (operation !== "importKey" && operation !== "deriveBits" &&
            operation !== "get key length") {
          throw domException("NotSupportedError", `${name} does not support ${operation}`);
        }
        if (operation !== "deriveBits") return {name};
        if (name === "PBKDF2") {
          const salt = copyBufferSource(requiredMember(algorithm, "salt"));
          const iterations = enforceRangeUint32(requiredMember(algorithm, "iterations"), "iterations");
          const hash = normalizeAlgorithm(requiredMember(algorithm, "hash"), "digest").name;
          return {name, hash, salt, iterations};
        }
        const hash = normalizeAlgorithm(requiredMember(algorithm, "hash"), "digest").name;
        const salt = copyBufferSource(requiredMember(algorithm, "salt"));
        const info = copyBufferSource(requiredMember(algorithm, "info"));
        return {name, hash, salt, info};
      }
      default:
        throw domException("NotSupportedError", `Unrecognized algorithm name: ${name}`);
    }
  }

  function isSharedArrayBuffer(buffer) {
    if (sharedByteLengthGetter === null) return false;
    try {
      reflectApply(sharedByteLengthGetter, buffer, []);
      return true;
    } catch (_) {
      return false;
    }
  }

  function bufferSourceParts(value) {
    if (arrayBufferIsView(value)) {
      let buffer, offset, byteLength;
      try {
        buffer = reflectApply(dataViewBufferGetter, value, []);
        offset = reflectApply(dataViewOffsetGetter, value, []);
        byteLength = reflectApply(dataViewByteLengthGetter, value, []);
      } catch (_) {
        try {
          buffer = reflectApply(typedArrayBufferGetter, value, []);
          offset = reflectApply(typedArrayOffsetGetter, value, []);
          byteLength = reflectApply(typedArrayByteLengthGetter, value, []);
        } catch (_) {
          throw new TypeErrorCtor("The value is not a valid BufferSource");
        }
      }
      return {buffer, offset, byteLength};
    }
    let byteLength;
    try {
      byteLength = reflectApply(arrayBufferByteLengthGetter, value, []);
    } catch (_) {
      throw new TypeErrorCtor("The value is not a valid BufferSource");
    }
    return {buffer: value, offset: 0, byteLength};
  }

  function copyBufferSource(value) {
    const {buffer, offset, byteLength} = bufferSourceParts(value);
    if (isSharedArrayBuffer(buffer)) {
      throw new TypeErrorCtor("SharedArrayBuffer-backed BufferSource values are not supported");
    }
    let source;
    try {
      // Constructing a view also detects a detached zero-length buffer.
      source = new Uint8ArrayCtor(buffer, offset, byteLength);
    } catch (_) {
      throw new TypeErrorCtor("The BufferSource is detached or invalid");
    }
    const copy = new Uint8ArrayCtor(byteLength);
    reflectApply(typedArraySet, copy, [source]);
    return copy;
  }

  function arrayBufferFrom(bytes) {
    const copy = new Uint8ArrayCtor(reflectApply(typedArrayByteLengthGetter, bytes, []));
    reflectApply(typedArraySet, copy, [bytes]);
    return reflectApply(typedArrayBufferGetter, copy, []);
  }

  function appendOwn(array, value) {
    const descriptor = objectCreate(null);
    descriptor.value = value;
    descriptor.writable = true;
    descriptor.enumerable = true;
    descriptor.configurable = true;
    objectDefineProperty(array, array.length, descriptor);
  }

  function enumString(value, allowed, label) {
    const string = toDOMString(value, label);
    if (!reflectApply(setHas, allowed, [string])) {
      throw new TypeErrorCtor(`The ${label} value ${string} is not valid`);
    }
    return string;
  }

  function snapshotUsages(value) {
    if (value === null || value === undefined) {
      throw new TypeErrorCtor("keyUsages must be a sequence");
    }
    const supplied = [];
    for (const item of value) {
      appendOwn(supplied, enumString(item, usagesSet, "key usage"));
    }
    const normalized = [];
    for (let i = 0; i < usageOrder.length; i++) {
      const item = usageOrder[i];
      for (let j = 0; j < supplied.length; j++) {
        if (supplied[j] === item) {
          appendOwn(normalized, item);
          break;
        }
      }
    }
    return normalized;
  }

  function hasUsage(state, usage) {
    for (let i = 0; i < state.usages.length; i++) {
      if (state.usages[i] === usage) return true;
    }
    return false;
  }

  function keyState(key) {
    const state = reflectApply(weakMapGet, keyStates, [key]);
    if (state === undefined) {
      throw new TypeErrorCtor("The key is not a CryptoKey created by this runtime");
    }
    return state;
  }

  function hmacKeyBytes(raw, length) {
    const rawByteLength = reflectApply(typedArrayByteLengthGetter, raw, []);
    const dataBits = rawByteLength * 8;
    if (dataBits === 0 || length === 0 || length > dataBits || length <= dataBits - 8) {
      throw domException("DataError", "The HMAC key length is invalid");
    }
    const byteLength = MathCeil(length / 8);
    const trimmed = new Uint8ArrayCtor(byteLength);
    const rawBuffer = reflectApply(typedArrayBufferGetter, raw, []);
    const rawOffset = reflectApply(typedArrayOffsetGetter, raw, []);
    const source = new Uint8ArrayCtor(rawBuffer, rawOffset, byteLength);
    reflectApply(typedArraySet, trimmed, [source]);
    const remainder = length % 8;
    if (remainder !== 0) trimmed[byteLength - 1] &= 0xff << (8 - remainder);
    return trimmed;
  }

  function validateUsagesForKey(name, usages) {
    const allowed = name === "HMAC" ? hmacUsagesSet : derivationUsagesSet;
    for (let i = 0; i < usages.length; i++) {
      const usage = usages[i];
      if (!reflectApply(setHas, allowed, [usage])) {
        throw domException("SyntaxError", `The ${usage} usage is not valid for ${name}`);
      }
    }
  }

  function createKey(name, hash, length, bytes, extractable, usages) {
    const state = objectCreate(null);
    state.type = "secret";
    state.name = name;
    state.hash = hash;
    state.length = length;
    state.bytes = bytes;
    state.extractable = extractable;
    state.usages = usages;
    state.algorithmCached = undefined;
    state.usagesCached = undefined;
    return new CryptoKey(keyToken, state);
  }

  function defineOwnValue(object, name, value) {
    const descriptor = objectCreate(null);
    descriptor.value = value;
    descriptor.writable = true;
    descriptor.enumerable = true;
    descriptor.configurable = true;
    objectDefineProperty(object, name, descriptor);
  }

  class CryptoKey {
    constructor(token, state) {
      if (token !== keyToken || state === undefined) {
        throw new TypeErrorCtor("Illegal constructor");
      }
      reflectApply(weakMapSet, keyStates, [this, state]);
      host.cloneAs(this, "unsupported");
    }
    get type() { return keyState(this).type; }
    get algorithm() {
      const state = keyState(this);
      if (state.algorithmCached === undefined) {
        const algorithm = {name: state.name};
        if (state.name === "HMAC") {
          const hash = {name: state.hash};
          defineOwnValue(algorithm, "hash", hash);
          defineOwnValue(algorithm, "length", state.length);
        }
        state.algorithmCached = algorithm;
      }
      return state.algorithmCached;
    }
    get extractable() { return keyState(this).extractable; }
    get usages() {
      const state = keyState(this);
      if (state.usagesCached === undefined) {
        const copied = [];
        for (let i = 0; i < state.usages.length; i++) {
          appendOwn(copied, state.usages[i]);
        }
        state.usagesCached = copied;
      }
      return state.usagesCached;
    }
  }
  for (const property of ["type", "algorithm", "extractable", "usages"]) {
    const descriptor = objectCreate(null);
    descriptor.enumerable = true;
    objectDefineProperty(CryptoKey.prototype, property, descriptor);
  }
  objectDefineProperty(CryptoKey.prototype, Symbol.toStringTag, {value: "CryptoKey", configurable: true});

  function requestBytes(operation, hash, data, key, salt, info, iterations, length) {
    return (async () => {
      try {
        return await reflectApply(request, native, [
          operation, hash, data, key, salt, info, iterations, length,
        ]);
      } catch (_) {
        throw domException("OperationError", "The cryptographic operation failed");
      }
    })();
  }

  function digestSizeBits(hash) {
    switch (hash) {
      case "SHA-1": return 160;
      case "SHA-256": return 256;
      case "SHA-384": return 384;
      case "SHA-512": return 512;
    }
    throw domException("NotSupportedError", `Unsupported hash algorithm: ${hash}`);
  }

  function hmacBlockSizeBits(hash) {
    return hash === "SHA-384" || hash === "SHA-512" ? 1024 : 512;
  }

  function deriveLengthBits(value) {
    if (value === undefined || value === null) return null;
    return enforceRangeUint32(value, "length");
  }

  function validateDeriveLength(algorithm, length) {
    if (length === null || length % 8 !== 0) {
      throw domException("OperationError", "The derived bit length must be a multiple of 8");
    }
    if (algorithm.name === "PBKDF2" && algorithm.iterations === 0) {
      throw domException("OperationError", "PBKDF2 requires at least one iteration");
    }
    if (algorithm.name === "HKDF" && length > 255 * digestSizeBits(algorithm.hash)) {
      throw domException("OperationError", "The HKDF output is too long");
    }
  }

  function keyLengthFor(algorithm) {
    if (algorithm.name === "HMAC") {
      if (!algorithm.hasLength) return hmacBlockSizeBits(algorithm.hash);
      if (algorithm.length === 0) {
        throw new TypeErrorCtor("The HMAC key length must be greater than zero");
      }
      return algorithm.length;
    }
    return null;
  }

  async function deriveRaw(algorithm, state, length) {
    validateDeriveLength(algorithm, length);
    const operation = algorithm.name === "PBKDF2" ? "pbkdf2" : "hkdf";
    const bytes = length / 8;
    const info = algorithm.name === "HKDF" ? algorithm.info : emptyBytes;
    const iterations = algorithm.name === "PBKDF2" ? algorithm.iterations : 0;
    return requestBytes(operation, algorithm.hash, emptyBytes, state.bytes,
      algorithm.salt, info, iterations, bytes);
  }

  const methods = {
    async digest(algorithm, data) {
      const bytes = copyBufferSource(data);
      const normalized = normalizeAlgorithm(algorithm, "digest");
      const result = await requestBytes("digest", normalized.name, bytes,
        emptyBytes, emptyBytes, emptyBytes, 0, 0);
      return arrayBufferFrom(result);
    },

    async importKey(format, data, algorithm, extractable, usages) {
      const keyFormat = enumString(format, formatSet, "key format");
      const raw = keyFormat === "raw" ? copyBufferSource(data) : null;
      const canExtract = !!extractable;
      const keyUsages = snapshotUsages(usages);
      const normalized = normalizeAlgorithm(algorithm, "importKey");
      if (keyFormat !== "raw") {
        throw domException("NotSupportedError", `The ${keyFormat} key format is not supported`);
      }
      validateUsagesForKey(normalized.name, keyUsages);
      if (normalized.name === "HMAC") {
        const rawLength = reflectApply(typedArrayByteLengthGetter, raw, []);
        if (rawLength === 0) throw domException("DataError", "An HMAC key must not be empty");
        const length = normalized.hasLength ? normalized.length : rawLength * 8;
        const bytes = hmacKeyBytes(raw, length);
        if (keyUsages.length === 0) {
          throw domException("SyntaxError", "An imported secret key must have at least one usage");
        }
        return createKey("HMAC", normalized.hash, length, bytes, canExtract, keyUsages);
      }
      if (normalized.name === "PBKDF2" || normalized.name === "HKDF") {
        if (canExtract) {
          throw domException("SyntaxError", `${normalized.name} keys must be non-extractable`);
        }
        if (keyUsages.length === 0) {
          throw domException("SyntaxError", "An imported secret key must have at least one usage");
        }
        return createKey(normalized.name, undefined, undefined, raw, false, keyUsages);
      }
      throw domException("NotSupportedError", `Key import is not supported for ${normalized.name}`);
    },

    async exportKey(format, key) {
      const keyFormat = enumString(format, formatSet, "key format");
      const state = keyState(key);
      if (state.name !== "HMAC") {
        throw domException("NotSupportedError", `Key export is not supported for ${state.name}`);
      }
      if (!state.extractable) {
        throw domException("InvalidAccessError", "The CryptoKey is not extractable");
      }
      if (keyFormat !== "raw") {
        throw domException("NotSupportedError", `The ${keyFormat} key format is not supported`);
      }
      return arrayBufferFrom(state.bytes);
    },

    async sign(algorithm, key, data) {
      const state = keyState(key);
      const bytes = copyBufferSource(data);
      const normalized = normalizeAlgorithm(algorithm, "sign");
      if (state.name !== normalized.name || !hasUsage(state, "sign")) {
        throw domException("InvalidAccessError", "The CryptoKey cannot be used for signing");
      }
      const result = await requestBytes("hmac", state.hash, bytes, state.bytes,
        emptyBytes, emptyBytes, 0, 0);
      return arrayBufferFrom(result);
    },

    async verify(algorithm, key, signature, data) {
      const state = keyState(key);
      const signatureBytes = copyBufferSource(signature);
      const dataBytes = copyBufferSource(data);
      const normalized = normalizeAlgorithm(algorithm, "verify");
      if (state.name !== normalized.name || !hasUsage(state, "verify")) {
        throw domException("InvalidAccessError", "The CryptoKey cannot be used for verification");
      }
      const expected = await requestBytes("hmac", state.hash, dataBytes, state.bytes,
        emptyBytes, emptyBytes, 0, 0);
      return reflectApply(equal, native, [signatureBytes, expected]);
    },

    async deriveBits(algorithm, baseKey, length) {
      const state = keyState(baseKey);
      const bits = deriveLengthBits(length);
      const normalized = normalizeAlgorithm(algorithm, "deriveBits");
      if (state.name !== normalized.name || !hasUsage(state, "deriveBits")) {
        throw domException("InvalidAccessError", "The CryptoKey cannot be used to derive bits");
      }
      const result = await deriveRaw(normalized, state, bits);
      return arrayBufferFrom(result);
    },

    async deriveKey(algorithm, baseKey, derivedKeyType, extractable, usages) {
      const state = keyState(baseKey);
      const canExtract = !!extractable;
      const keyUsages = snapshotUsages(usages);
      const normalized = normalizeAlgorithm(algorithm, "deriveBits");
      const derived = normalizeAlgorithm(derivedKeyType, "importKey");
      if (state.name !== normalized.name || !hasUsage(state, "deriveKey")) {
        throw domException("InvalidAccessError", "The CryptoKey cannot be used to derive a key");
      }
      const bits = keyLengthFor(derived);
      if (bits === null) {
        throw domException("OperationError", "The target key algorithm has no fixed length");
      }
      validateUsagesForKey(derived.name, keyUsages);
      if (keyUsages.length === 0) {
        throw domException("SyntaxError", "A derived secret key must have at least one usage");
      }
      if (derived.name === "PBKDF2" || derived.name === "HKDF") {
        if (canExtract) {
          throw domException("SyntaxError", `${derived.name} keys must be non-extractable`);
        }
      }
      if (bits % 8 !== 0) {
        throw domException("OperationError", "The derived bit length must be a multiple of 8");
      }
      const raw = await deriveRaw(normalized, state, bits);
      if (derived.name !== "HMAC") {
        throw domException("NotSupportedError", `Derived ${derived.name} keys are not supported`);
      }
      const rawLength = reflectApply(typedArrayByteLengthGetter, raw, []);
      if (rawLength === 0) throw domException("DataError", "An HMAC key must not be empty");
      const keyLength = derived.hasLength ? derived.length : rawLength * 8;
      const keyBytes = hmacKeyBytes(raw, keyLength);
      return createKey("HMAC", derived.hash, keyLength, keyBytes, canExtract, keyUsages);
    },
  };

  for (const name of ["digest", "importKey", "exportKey", "sign", "verify", "deriveBits", "deriveKey"]) {
    objectDefineProperty(subtle, name, {
      value: methods[name], writable: true, configurable: true,
    });
  }
  objectDefineProperty(globalThis, "CryptoKey", {
    value: CryptoKey, writable: true, configurable: true,
  });
  return CryptoKey;
})
