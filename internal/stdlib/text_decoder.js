(function (host) {
  "use strict";

  // Capture the intrinsics used by the binding. Decoder state stays in this
  // closure rather than on the public object, as it does for a native Web API.
  const apply = Reflect.apply;
  const string = String;
  const typeError = TypeError;
  const rangeError = RangeError;
  const stringFromCodePoint = String.fromCodePoint;
  const stringFromCharCode = String.fromCharCode;
  const charCodeAt = String.prototype.charCodeAt;
  const weakMap = new WeakMap();
  const weakMapGet = WeakMap.prototype.get;
  const weakMapHas = WeakMap.prototype.has;
  const weakMapSet = WeakMap.prototype.set;
  const ownDescriptor = Object.getOwnPropertyDescriptor;
  const uint8Array = Uint8Array;
  const isView = ArrayBuffer.isView;
  const arrayBufferByteLength = ownDescriptor(ArrayBuffer.prototype, "byteLength").get;
  const arrayBufferResizable = ownDescriptor(ArrayBuffer.prototype, "resizable")?.get;
  const arrayBufferDetached = ownDescriptor(ArrayBuffer.prototype, "detached")?.get;
  const typedArrayPrototype = Object.getPrototypeOf(Uint8Array.prototype);
  const typedArrayBuffer = ownDescriptor(typedArrayPrototype, "buffer").get;
  const typedArrayByteOffset = ownDescriptor(typedArrayPrototype, "byteOffset").get;
  const typedArrayByteLength = ownDescriptor(typedArrayPrototype, "byteLength").get;
  const dataViewPrototype = DataView.prototype;
  const dataViewBuffer = ownDescriptor(dataViewPrototype, "buffer").get;
  const dataViewByteOffset = ownDescriptor(dataViewPrototype, "byteOffset").get;
  const dataViewByteLength = ownDescriptor(dataViewPrototype, "byteLength").get;
  const sharedByteLength = typeof SharedArrayBuffer === "undefined" ? null
    : ownDescriptor(SharedArrayBuffer.prototype, "byteLength").get;
  const sharedGrowable = typeof SharedArrayBuffer === "undefined" ? null
    : ownDescriptor(SharedArrayBuffer.prototype, "growable")?.get;

  function call(getter, receiver) {
    return apply(getter, receiver, []);
  }

  function domString(value) {
    // String(Symbol()) is a convenience that ToString, and thus DOMString,
    // does not provide.
    if (typeof value === "symbol") {
      throw new typeError("A Symbol cannot be converted to DOMString");
    }
    return string(value);
  }

  function asciiSpace(code) {
    return code === 0x09 || code === 0x0a || code === 0x0c ||
      code === 0x0d || code === 0x20;
  }

  function encodingLabel(label) {
    let first = 0;
    let last = label.length;
    while (first < last && asciiSpace(apply(charCodeAt, label, [first]))) first++;
    while (last > first && asciiSpace(apply(charCodeAt, label, [last - 1]))) last--;
    let normalized = "";
    for (let i = first; i < last; i++) {
      let code = apply(charCodeAt, label, [i]);
      if (code >= 0x41 && code <= 0x5a) code += 0x20;
      normalized += stringFromCharCode(code);
    }
    if (normalized !== "utf-8" && normalized !== "utf8" &&
        normalized !== "unicode-1-1-utf-8" && normalized !== "unicode11utf8" &&
        normalized !== "unicode20utf8" && normalized !== "x-unicode20utf8") {
      throw new rangeError("The encoding label is not supported: " + normalized);
    }
  }

  function dictionary(value) {
    if (value === null || value === undefined) return null;
    if (typeof value !== "object" && typeof value !== "function") {
      throw new typeError("The options value must be an object");
    }
    return value;
  }

  function requireDecoder(receiver) {
    if (!apply(weakMapHas, weakMap, [receiver])) {
      throw new typeError("The receiver is not a TextDecoder");
    }
    return apply(weakMapGet, weakMap, [receiver]);
  }

  function freshUTF8State() {
    return {codePoint: 0, bytesSeen: 0, bytesNeeded: 0,
      lowerBoundary: 0x80, upperBoundary: 0xbf};
  }

  function resetUTF8State(state) {
    state.codePoint = 0;
    state.bytesSeen = 0;
    state.bytesNeeded = 0;
    state.lowerBoundary = 0x80;
    state.upperBoundary = 0xbf;
  }

  function bufferInfo(buffer) {
    let shared = false;
    let byteLength;
    if (sharedByteLength !== null) {
      try {
        byteLength = call(sharedByteLength, buffer);
        shared = true;
      } catch (_) {}
    }
    if (!shared) {
      try {
        byteLength = call(arrayBufferByteLength, buffer);
      } catch (_) {
        throw new typeError("The input is not a valid BufferSource");
      }
    }

    let variableLength = false;
    try {
      const getter = shared ? sharedGrowable : arrayBufferResizable;
      if (getter !== null && getter !== undefined) {
        variableLength = call(getter, buffer);
      }
    } catch (_) {
      throw new typeError("The input buffer is detached or invalid");
    }
    if (variableLength) {
      throw new typeError("Resizable and growable buffers are not supported");
    }
    return {byteLength, shared};
  }

  function isDetached(buffer, shared) {
    if (shared) return false;
    if (arrayBufferDetached !== null && arrayBufferDetached !== undefined) {
      try { return call(arrayBufferDetached, buffer); }
      catch (_) { return true; }
    }
    try {
      new uint8Array(buffer, 0, 0);
      return false;
    } catch (_) {
      return true;
    }
  }

  function sourceParts(value) {
    if (apply(isView, ArrayBuffer, [value])) {
      let buffer;
      try {
        buffer = call(dataViewBuffer, value);
      } catch (_) {
        try {
          buffer = call(typedArrayBuffer, value);
        } catch (_) {
          throw new typeError("The input is not a valid BufferSource");
        }
      }
      const info = bufferInfo(buffer);
      // A detached DataView's byteOffset and byteLength accessors throw, but
      // Web IDL's copy operation produces an empty sequence for any detached
      // BufferSource.
      if (isDetached(buffer, info.shared)) {
        return {buffer, offset: 0, byteLength: 0, shared: false};
      }
      let offset, byteLength;
      try {
        offset = call(dataViewByteOffset, value);
        byteLength = call(dataViewByteLength, value);
      } catch (_) {
        try {
          offset = call(typedArrayByteOffset, value);
          byteLength = call(typedArrayByteLength, value);
        } catch (_) {
          throw new typeError("The input is not a valid BufferSource");
        }
      }
      return {buffer, offset, byteLength, shared: info.shared};
    }

    const info = bufferInfo(value);
    // A raw ArrayBuffer or SharedArrayBuffer covers its whole fixed byte span.
    return {buffer: value, offset: 0, byteLength: info.byteLength, shared: info.shared};
  }

  function copyInput(parts) {
    if (isDetached(parts.buffer, parts.shared)) return new uint8Array(0);
    let source;
    try {
      source = new uint8Array(parts.buffer, parts.offset, parts.byteLength);
    } catch (_) {
      if (isDetached(parts.buffer, parts.shared)) return new uint8Array(0);
      throw new typeError("The input buffer is invalid");
    }
    const bytes = new uint8Array(parts.byteLength);
    for (let i = 0; i < parts.byteLength; i++) bytes[i] = source[i];
    return bytes;
  }

  function appendCodePoint(state, output, codePoint) {
    if (!state.ignoreBOM && !output.bomSeen) {
      output.bomSeen = true;
      if (codePoint === 0xfeff) return;
    }
    output.value += stringFromCodePoint(codePoint);
  }

  // Return 1 for a scalar, 0 for a partial scalar, -1 for a decoding error
  // that consumed the byte, and -2 for a bad continuation that the caller
  // must reprocess after replacing the pending prefix.
  function decodeByte(state, byte, output) {
    if (state.bytesNeeded === 0) {
      if (byte <= 0x7f) {
        appendCodePoint(output.state, output, byte);
        return 1;
      }
      if (byte >= 0xc2 && byte <= 0xdf) {
        state.bytesNeeded = 1;
        state.codePoint = byte & 0x1f;
      } else if (byte >= 0xe0 && byte <= 0xef) {
        if (byte === 0xe0) state.lowerBoundary = 0xa0;
        if (byte === 0xed) state.upperBoundary = 0x9f;
        state.bytesNeeded = 2;
        state.codePoint = byte & 0x0f;
      } else if (byte >= 0xf0 && byte <= 0xf4) {
        if (byte === 0xf0) state.lowerBoundary = 0x90;
        if (byte === 0xf4) state.upperBoundary = 0x8f;
        state.bytesNeeded = 3;
        state.codePoint = byte & 0x07;
      } else {
        return -1;
      }
      return 0;
    }

    if (byte < state.lowerBoundary || byte > state.upperBoundary) {
      resetUTF8State(state);
      return -2;
    }

    state.lowerBoundary = 0x80;
    state.upperBoundary = 0xbf;
    state.codePoint = (state.codePoint << 6) | (byte & 0x3f);
    state.bytesSeen++;
    if (state.bytesSeen !== state.bytesNeeded) return 0;

    appendCodePoint(output.state, output, state.codePoint);
    resetUTF8State(state);
    return 1;
  }

  class TextDecoder {
    constructor(label = "utf-8", options) {
      const normalized = domString(label);
      const opts = dictionary(options);
      const fatal = opts === null ? false : !!opts.fatal;
      const ignoreBOM = opts === null ? false : !!opts.ignoreBOM;
      encodingLabel(normalized);
      host.cloneAs(this, "opaque");
      apply(weakMapSet, weakMap, [this, {
        fatal,
        ignoreBOM,
        doNotFlush: false,
        bomSeen: false,
        utf8: freshUTF8State(),
        pendingBytes: new uint8Array(0),
      }]);
    }

    get encoding() { requireDecoder(this); return "utf-8"; }
    get fatal() { return requireDecoder(this).fatal; }
    get ignoreBOM() { return requireDecoder(this).ignoreBOM; }

    decode(...args) {
      const state = requireDecoder(this);
      // Web IDL converts the input first and the options dictionary second.
      const input = args.length > 0 ? args[0] : undefined;
      const parts = input === undefined ? null : sourceParts(input);
      const opts = dictionary(args.length > 1 ? args[1] : undefined);
      const stream = opts === null ? false : !!opts.stream;

      if (!state.doNotFlush) {
        resetUTF8State(state.utf8);
        state.bomSeen = false;
        state.pendingBytes = new uint8Array(0);
      }
      state.doNotFlush = stream;

      const bytes = parts === null ? new uint8Array(0) : copyInput(parts);
      let queue = bytes;
      if (state.pendingBytes.length > 0) {
        const pending = state.pendingBytes;
        queue = new uint8Array(pending.length + bytes.length);
        for (let i = 0; i < pending.length; i++) queue[i] = pending[i];
        for (let i = 0; i < bytes.length; i++) queue[pending.length + i] = bytes[i];
      }
      state.pendingBytes = new uint8Array(0);
      const output = {state, value: "", bomSeen: state.bomSeen};
      for (let i = 0; i < queue.length;) {
        const result = decodeByte(state.utf8, queue[i], output);
        if (result === -1 || result === -2) {
          if (state.fatal) {
            // A malformed continuation is restored by the UTF-8 decoder
            // before the fatal error reaches the caller. The remaining input
            // queue is also retained while streaming.
            const remainder = result === -2 ? i : i + 1;
            if (stream) {
              state.pendingBytes = new uint8Array(queue.length - remainder);
              for (let j = remainder; j < queue.length; j++) {
                state.pendingBytes[j - remainder] = queue[j];
              }
            }
            throw new typeError("The input is not valid UTF-8");
          }
          appendCodePoint(state, output, 0xfffd);
          if (result === -1) i++;
          continue;
        }
        i++;
      }

      if (!stream && state.utf8.bytesNeeded !== 0) {
        resetUTF8State(state.utf8);
        if (state.fatal) throw new typeError("The input is not valid UTF-8");
        appendCodePoint(state, output, 0xfffd);
      }
      state.bomSeen = output.bomSeen;
      return output.value;
    }
  }

  // Web IDL exposes interface members as enumerable properties on the
  // prototype. Class syntax creates non-enumerable members, so adjust those
  // descriptors and install the platform interface's toString tag.
  const defineProperty = Object.defineProperty;
  for (const name of ["encoding", "fatal", "ignoreBOM", "decode"]) {
    const descriptor = ownDescriptor(TextDecoder.prototype, name);
    descriptor.enumerable = true;
    defineProperty(TextDecoder.prototype, name, descriptor);
  }
  defineProperty(TextDecoder.prototype, Symbol.toStringTag, {
    value: "TextDecoder", configurable: true
  });

  return TextDecoder;
})
