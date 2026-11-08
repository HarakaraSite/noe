(function (host) {
  "use strict";

  const apply = Reflect.apply;
  const String_ = String;
  const charCodeAt = String.prototype.charCodeAt;
  const fromCharCode = String.fromCharCode;
  const defineProperty = Object.defineProperty;
  const ownDescriptor = Object.getOwnPropertyDescriptor;
  const weakMapGet = WeakMap.prototype.get;
  const weakMapHas = WeakMap.prototype.has;
  const weakMapSet = WeakMap.prototype.set;

  const encoderStreams = new WeakMap();
  const decoderStreams = new WeakMap();

  function call(fn, receiver, ...args) {
    return apply(fn, receiver, args);
  }

  function domString(value) {
    // Web IDL's DOMString conversion uses ToString, which rejects Symbols.
    if (typeof value === "symbol") {
      throw new TypeError("A Symbol cannot be converted to DOMString");
    }
    return String_(value);
  }

  function getState(map, receiver, name) {
    if (!call(weakMapHas, map, receiver)) {
      throw new TypeError("The receiver is not a " + name);
    }
    return call(weakMapGet, map, receiver);
  }

  function isLeadingSurrogate(code) {
    return code >= 0xd800 && code <= 0xdbff;
  }

  function isTrailingSurrogate(code) {
    return code >= 0xdc00 && code <= 0xdfff;
  }

  const replacement = "\ufffd";

  class TextEncoderStream {
    constructor() {
      // Resolve the codec when the stream is constructed. Streams can be
      // installed by themselves, before WebAPIs has supplied TextEncoder.
      const Encoder = globalThis.TextEncoder;
      if (typeof Encoder !== "function") {
        throw new TypeError("TextEncoder is not available");
      }
      const encoder = new Encoder();
      let leadingSurrogate = null;
      const transform = new host.TransformStream({
        transform(chunk, controller) {
          const input = domString(chunk);
          let output = "";
          let at = 0;

          // Empty chunks do not consume a pending leading surrogate.
          if (leadingSurrogate !== null) {
            if (input.length === 0) return;
            const first = call(charCodeAt, input, 0);
            const lead = leadingSurrogate;
            leadingSurrogate = null;
            if (isTrailingSurrogate(first)) {
              output += call(fromCharCode, String_, lead, first);
              at = 1;
            } else {
              output += replacement;
            }
          }

          while (at < input.length) {
            const code = call(charCodeAt, input, at);
            if (isLeadingSurrogate(code)) {
              if (at + 1 === input.length) {
                leadingSurrogate = code;
                break;
              }
              const next = call(charCodeAt, input, at + 1);
              if (isTrailingSurrogate(next)) {
                output += call(fromCharCode, String_, code, next);
                at += 2;
              } else {
                output += replacement;
                at++;
              }
              continue;
            }

            output += isTrailingSurrogate(code)
              ? replacement : call(fromCharCode, String_, code);
            at++;
          }

          if (output.length !== 0) {
            const bytes = encoder.encode(output);
            if (bytes.byteLength !== 0) controller.enqueue(bytes);
          }
        },
        flush(controller) {
          if (leadingSurrogate !== null) {
            leadingSurrogate = null;
            controller.enqueue(encoder.encode(replacement));
          }
        },
      });

      host.cloneAs(this, "unsupported");
      call(weakMapSet, encoderStreams, this, {
        encoding: "utf-8",
        readable: transform.readable,
        writable: transform.writable,
      });
    }

    get encoding() {
      return getState(encoderStreams, this, "TextEncoderStream").encoding;
    }
    get readable() {
      return getState(encoderStreams, this, "TextEncoderStream").readable;
    }
    get writable() {
      return getState(encoderStreams, this, "TextEncoderStream").writable;
    }
  }

  class TextDecoderStream {
    constructor(label = "utf-8", options) {
      // Like TextEncoder, TextDecoder is resolved only on construction so
      // Streams-alone installation does not depend on WebAPIs at evaluation.
      const Decoder = globalThis.TextDecoder;
      if (typeof Decoder !== "function") {
        throw new TypeError("TextDecoder is not available");
      }

      // Delegate label/options conversions and codec state to the existing
      // TextDecoder implementation. Leaving omitted options as undefined
      // gives it Web IDL's empty-dictionary behavior without reading inherited
      // Object.prototype properties.
      const decoder = new Decoder(label, options);
      const transform = new host.TransformStream({
        transform(chunk, controller) {
          // TextDecoder.decode(undefined) means an empty input, but the stream
          // contract requires every written chunk to be a BufferSource.
          if (chunk === undefined) {
            throw new TypeError("The chunk is not a valid BufferSource");
          }
          const text = decoder.decode(chunk, {stream: true});
          if (text.length !== 0) controller.enqueue(text);
        },
        flush(controller) {
          const text = decoder.decode();
          if (text.length !== 0) controller.enqueue(text);
        },
      });

      host.cloneAs(this, "unsupported");
      call(weakMapSet, decoderStreams, this, {
        decoder,
        encoding: decoder.encoding,
        readable: transform.readable,
        writable: transform.writable,
      });
    }

    get encoding() {
      return getState(decoderStreams, this, "TextDecoderStream").encoding;
    }
    get fatal() {
      return getState(decoderStreams, this, "TextDecoderStream").decoder.fatal;
    }
    get ignoreBOM() {
      return getState(decoderStreams, this, "TextDecoderStream").decoder.ignoreBOM;
    }
    get readable() {
      return getState(decoderStreams, this, "TextDecoderStream").readable;
    }
    get writable() {
      return getState(decoderStreams, this, "TextDecoderStream").writable;
    }
  }

  // Web IDL exposes interface members as enumerable prototype attributes.
  for (const [prototype, names] of [
    [TextEncoderStream.prototype, ["encoding", "readable", "writable"]],
    [TextDecoderStream.prototype,
      ["encoding", "fatal", "ignoreBOM", "readable", "writable"]],
  ]) {
    for (const name of names) {
      const descriptor = ownDescriptor(prototype, name);
      descriptor.enumerable = true;
      defineProperty(prototype, name, descriptor);
    }
  }
  defineProperty(TextEncoderStream.prototype, Symbol.toStringTag, {
    value: "TextEncoderStream", configurable: true,
  });
  defineProperty(TextDecoderStream.prototype, Symbol.toStringTag, {
    value: "TextDecoderStream", configurable: true,
  });

  return {TextEncoderStream, TextDecoderStream};
})
