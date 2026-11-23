(function (host, webDOMString, TextEncoder, TextDecoder) {
  "use strict";
  // Blob storage is immutable and private. Derived Blobs and renamed Files
  // can share it, while every author-visible byte result owns its buffer.
  const binaryApply = Reflect.apply;
  const binaryGet = WeakMap.prototype.get, binarySet = WeakMap.prototype.set;
  const binaryCreate = Object.create;
  const binaryBytes = Uint8Array;
  const binaryBufferLength = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, "byteLength").get;
  const binaryBufferResizable = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, "resizable").get;
  const binaryBufferDetached = Object.getOwnPropertyDescriptor(ArrayBuffer.prototype, "detached").get;
  const binaryViewBuffer = Object.getOwnPropertyDescriptor(DataView.prototype, "buffer").get;
  const binaryViewOffset = Object.getOwnPropertyDescriptor(DataView.prototype, "byteOffset").get;
  const binaryViewLength = Object.getOwnPropertyDescriptor(DataView.prototype, "byteLength").get;
  const binaryTyped = Object.getPrototypeOf(Uint8Array.prototype);
  const binaryTypedBuffer = Object.getOwnPropertyDescriptor(binaryTyped, "buffer").get;
  const binaryTypedOffset = Object.getOwnPropertyDescriptor(binaryTyped, "byteOffset").get;
  const binaryTypedLength = Object.getOwnPropertyDescriptor(binaryTyped, "byteLength").get;
  const binarySetBytes = Uint8Array.prototype.set;
  const binaryPush = Array.prototype.push, binarySplice = Array.prototype.splice;
  const binaryEncode = TextEncoder.prototype.encode, binaryDecode = TextDecoder.prototype.decode;
  const binaryLower = String.prototype.toLowerCase;
  const binaryLength = bytes => binaryApply(binaryTypedLength, bytes, []);
  const binaryRange = (bytes, start, end) => new binaryBytes(binaryApply(binaryTypedBuffer, bytes, []), binaryApply(binaryTypedOffset, bytes, []) + start, end - start);
  const binaryIsView = ArrayBuffer.isView;
  const binaryWellFormed = String.prototype.toWellFormed;
  const binaryNow = Date.now;
  const blobStates = new WeakMap(), fileStates = new WeakMap(), formStates = new WeakMap();
  const emptyBlobBytes = new binaryBytes(0);
  const blobState = value => binaryApply(binaryGet, blobStates, [value]);
  const fileState = value => binaryApply(binaryGet, fileStates, [value]);
  const formState = value => binaryApply(binaryGet, formStates, [value]);
  function binaryUSV(value) { return binaryApply(binaryWellFormed, webDOMString(value), []); }
  function requireBlob(value) {
    const state = blobState(value);
    if (state === undefined) throw new TypeError("The receiver is not a Blob");
    return state;
  }
  function requireFile(value) {
    const state = fileState(value);
    if (state === undefined) throw new TypeError("The receiver is not a File");
    return state;
  }
  function requireForm(value) {
    const entries = formState(value);
    if (entries === undefined) throw new TypeError("The receiver is not FormData");
    return entries;
  }
  function copyBinary(bytes) {
    const copy = new binaryBytes(binaryLength(bytes));
    binaryApply(binarySetBytes, copy, [bytes]);
    return copy;
  }
  function binaryView(value) {
    const isView = binaryIsView(value);
    let buffer = value, dataView = false;
    if (isView) {
      try {
        buffer = binaryApply(binaryViewBuffer, value, []);
        dataView = true;
      } catch (_) {
        buffer = binaryApply(binaryTypedBuffer, value, []);
      }
    }
    let length;
    try { length = binaryApply(binaryBufferLength, buffer, []); }
    catch (_) {
      if (isView) throw new TypeError("SharedArrayBuffer-backed Blob parts are not supported");
      return null;
    }
    if (binaryApply(binaryBufferResizable, buffer, [])) {
      throw new TypeError("Resizable ArrayBuffer-backed Blob parts are not supported");
    }
    if (binaryApply(binaryBufferDetached, buffer, [])) return emptyBlobBytes;
    if (!isView) return new binaryBytes(buffer, 0, length);
    const offset = binaryApply(dataView ? binaryViewOffset : binaryTypedOffset, value, []);
    length = binaryApply(dataView ? binaryViewLength : binaryTypedLength, value, []);
    return new binaryBytes(buffer, offset, length);
  }
  function blobParts(value, optional) {
    if (optional && value === undefined) return [];
    if (value === null || (typeof value !== "object" && typeof value !== "function")) throw new TypeError("Blob parts must be an iterable object");
    const iterator = value[Symbol.iterator];
    if (typeof iterator !== "function") throw new TypeError("Blob parts must be iterable");
    // Convert the complete sequence before reading the options dictionary.
    const parts = [];
    for (const part of {[Symbol.iterator]: () => binaryApply(iterator, value, [])}) {
      const state = blobState(part);
      if (state !== undefined) binaryApply(binaryPush, parts, [{bytes: state.bytes, immutable: true, text: undefined}]);
      else {
        const bytes = binaryView(part);
        const converted = bytes === null
          ? {bytes: undefined, text: binaryUSV(part), immutable: true}
          : {bytes, immutable: bytes === emptyBlobBytes, text: undefined};
        binaryApply(binaryPush, parts, [converted]);
      }
    }
    return parts;
  }
  function blobOptions(options, file) {
    if (options != null && typeof options !== "object" && typeof options !== "function") throw new TypeError("Blob options must be a dictionary");
    const endings = options == null ? undefined : options.endings;
    const ending = endings === undefined ? "transparent" : webDOMString(endings);
    if (ending !== "transparent" && ending !== "native") throw new TypeError("Invalid Blob endings");
    const type = options == null ? undefined : options.type;
    const mime = type === undefined ? "" : blobType(webDOMString(type));
    let modified;
    if (file) {
      const value = options == null ? undefined : options.lastModified;
      const number = value === undefined ? binaryNow() : +value;
      modified = Number.isFinite(number) ? Number(BigInt.asIntN(64, BigInt(Math.trunc(number)))) : 0;
    }
    return {endings: ending, type: mime, lastModified: modified};
  }
  function blobType(type) { return /[^\x20-\x7e]/.test(type) ? "" : binaryApply(binaryLower, type, []); }
  function buildBlob(parts, endings) {
    const pieces = [];
    let length = 0;
    for (let index = 0; index < parts.length; index++) {
      const part = parts[index];
      let bytes = part.bytes;
      if (bytes === undefined) {
        const text = endings === "native" ? part.text.replace(/\r\n|\r|\n/g, host.nativeEOL) : part.text;
        bytes = binaryApply(binaryEncode, new TextEncoder(), [text]);
      } else if (!part.immutable && binaryApply(binaryBufferDetached, binaryApply(binaryTypedBuffer, bytes, []), [])) {
        // Options or a File name can detach a buffer after parts conversion.
        bytes = emptyBlobBytes;
      }
      binaryApply(binaryPush, pieces, [bytes]); length += binaryLength(bytes);
    }
    if (pieces.length === 0) return emptyBlobBytes;
    if (pieces.length === 1) return parts[0].immutable || pieces[0] === emptyBlobBytes ? pieces[0] : copyBinary(pieces[0]);
    const out = new binaryBytes(length);
    let offset = 0;
    for (let index = 0; index < pieces.length; index++) {
      const piece = pieces[index]; binaryApply(binarySetBytes, out, [piece, offset]); offset += binaryLength(piece);
    }
    return out;
  }
  function initializeBlob(value, bytes, type) {
    host.cloneAs(value, "host");
    binaryApply(binarySet, blobStates, [value, {bytes, type}]);
    return value;
  }
  function makeBinaryFile(bytes, type, name, lastModified) {
    const value = initializeBlob(binaryCreate(File.prototype), bytes, type);
    binaryApply(binarySet, fileStates, [value, {name, lastModified}]);
    return value;
  }
  class Blob {
    constructor(parts = undefined, options = undefined) {
      const converted = blobParts(parts, true), bag = blobOptions(options, false);
      initializeBlob(this, buildBlob(converted, bag.endings), bag.type);
    }
    get size() { return binaryLength(requireBlob(this).bytes); }
    get type() { return requireBlob(this).type; }
    get [Symbol.toStringTag]() { return "Blob"; }
    async bytes() { return copyBinary(requireBlob(this).bytes); }
    async arrayBuffer() { return copyBinary(requireBlob(this).bytes).buffer; }
    async text() { return binaryApply(binaryDecode, new TextDecoder(), [requireBlob(this).bytes]); }
    slice(start = 0, end = undefined, type = "") {
      const state = requireBlob(this), size = binaryLength(state.bytes);
      const relative = (value) => {
        const number = +value;
        if (Number.isNaN(number)) return 0;
        const integer = Math.trunc(number);
        return integer < 0 ? Math.max(size + integer, 0) : Math.min(integer, size);
      };
      const from = relative(start), to = end === undefined ? size : relative(end);
      const mime = blobType(webDOMString(type));
      const bytes = binaryRange(state.bytes, from, Math.max(from, to));
      return initializeBlob(binaryCreate(Blob.prototype), bytes, mime);
    }
    stream() {
      const bytes = requireBlob(this).bytes;
      let offset = 0;
      return new ReadableStream({pull(controller) {
        if (offset === binaryLength(bytes)) {controller.close(); return;}
        const end = Math.min(offset + 65536, binaryLength(bytes));
        controller.enqueue(copyBinary(binaryRange(bytes, offset, end)));
        offset = end;
      }}, {highWaterMark: 0});
    }
  }
  class File extends Blob {
    constructor(parts, name, options = undefined) {
      if (arguments.length < 2) throw new TypeError("File requires parts and a name");
      const converted = blobParts(parts, false), filename = binaryUSV(name), bag = blobOptions(options, true);
      super();
      initializeBlob(this, buildBlob(converted, bag.endings), bag.type);
      binaryApply(binarySet, fileStates, [this, {name: filename, lastModified: bag.lastModified}]);
    }
    get name() { return requireFile(this).name; }
    get lastModified() { return requireFile(this).lastModified; }
    get [Symbol.toStringTag]() { return "File"; }
  }
  function formEntry(name, value, filename, hasFilename) {
    const blob = blobState(value);
    if (hasFilename && blob === undefined) throw new TypeError("A filename requires a Blob value");
    const key = binaryUSV(name);
    if (blob === undefined) return [key, binaryUSV(value)];
    const file = fileState(value);
    const renamed = hasFilename && filename !== undefined ? binaryUSV(filename) : undefined;
    if (file !== undefined && renamed === undefined) return [key, value];
    return [key, makeBinaryFile(blob.bytes, blob.type, renamed === undefined ? "blob" : renamed,
      file === undefined ? binaryNow() : file.lastModified)];
  }
  function requiredArguments(count, minimum) { if (count < minimum) throw new TypeError("Not enough arguments"); }
  function formIterator(value, kind) {
    const entries = requireForm(value);
    return (function* () {
      for (let index = 0; index < entries.length; index++) {
        const entry = entries[index];
        yield kind === "keys" ? entry[0] : kind === "values" ? entry[1] : [entry[0], entry[1]];
      }
    })();
  }
  class FormData {
    constructor(form = undefined) {
      if (form !== undefined) throw new TypeError("This host has no HTML form elements");
      host.cloneAs(this, "unsupported");
      binaryApply(binarySet, formStates, [this, []]);
    }
    append(name, value, filename = undefined) {
      const entries = requireForm(this); requiredArguments(arguments.length, 2);
      binaryApply(binaryPush, entries, [formEntry(name, value, filename, arguments.length >= 3)]);
    }
    set(name, value, filename = undefined) {
      const entries = requireForm(this); requiredArguments(arguments.length, 2);
      const entry = formEntry(name, value, filename, arguments.length >= 3);
      let replaced = false;
      for (let i = 0; i < entries.length; i++) if (entries[i][0] === entry[0]) {
        if (!replaced) {entries[i] = entry; replaced = true;}
        else binaryApply(binarySplice, entries, [i--, 1]);
      }
      if (!replaced) binaryApply(binaryPush, entries, [entry]);
    }
    get(name) {
      const entries = requireForm(this); requiredArguments(arguments.length, 1);
      const key = binaryUSV(name);
      for (let i = 0; i < entries.length; i++) if (entries[i][0] === key) return entries[i][1];
      return null;
    }
    getAll(name) {
      const entries = requireForm(this); requiredArguments(arguments.length, 1);
      const key = binaryUSV(name);
      const values = [];
      for (let i = 0; i < entries.length; i++) if (entries[i][0] === key) binaryApply(binaryPush, values, [entries[i][1]]);
      return values;
    }
    has(name) {
      const entries = requireForm(this); requiredArguments(arguments.length, 1);
      const key = binaryUSV(name);
      for (let i = 0; i < entries.length; i++) if (entries[i][0] === key) return true;
      return false;
    }
    delete(name) {
      const entries = requireForm(this); requiredArguments(arguments.length, 1);
      const key = binaryUSV(name);
      for (let i = 0; i < entries.length; i++) if (entries[i][0] === key) binaryApply(binarySplice, entries, [i--, 1]);
    }
    entries() { return formIterator(this, "entries"); }
    keys() { return formIterator(this, "keys"); }
    values() { return formIterator(this, "values"); }
    forEach(callback, thisArg = undefined) {
      const entries = requireForm(this);
      if (typeof callback !== "function") throw new TypeError("FormData callback must be callable");
      for (let index = 0; index < entries.length; index++) {
        const entry = entries[index]; binaryApply(callback, thisArg, [entry[1], entry[0], this]);
      }
    }
    get [Symbol.toStringTag]() { return "FormData"; }
  }
  FormData.prototype[Symbol.iterator] = FormData.prototype.entries;
  // Fetch and WebSocket obtain native state through a Go-private callback.
  host.bindBinaryData((value, operation) => {
    const blob = blobState(value), entries = formState(value);
    if (operation === "is") return blob !== undefined || entries !== undefined;
    if (operation === "form") {
      if (entries === undefined) return null;
      const copy = [];
      for (let i = 0; i < entries.length; i++) binaryApply(binaryPush, copy, [[entries[i][0], entries[i][1]]]);
      return copy;
    }
    if (blob === undefined) return null;
    const file = fileState(value);
    return {bytes: copyBinary(blob.bytes), type: blob.type,
      name: file === undefined ? "blob" : file.name};
  });

  return {Blob, File, FormData, blobState, fileState};
})
