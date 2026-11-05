(native => {
  "use strict";

  function invalid(message) {
    const error = new TypeError(message);
    error.code = "ERR_INVALID_ARG";
    throw error;
  }
  function integer(value, name) {
    if (!Number.isSafeInteger(value) || value < 0) invalid(name + " must be a nonnegative safe integer");
    return value;
  }
  function position(value) {
    return value === undefined || value === null ? null : integer(value, "position");
  }
  function options(value) {
    if (value === null || typeof value !== "object" || Array.isArray(value)) invalid("options must be an object");
    return value;
  }
  function fileMode(value) {
    if (!Number.isSafeInteger(value) || value < 0 || value > 0o777) invalid("mode must be an integer from 0 through 0o777");
    return value;
  }
  function bytes(buffer) {
    if (!ArrayBuffer.isView(buffer)) invalid("buffer must be an ArrayBuffer view");
    return new Uint8Array(buffer.buffer, buffer.byteOffset, buffer.byteLength);
  }
  function range(buffer, offset, length, pos) {
    const view = bytes(buffer);
    offset = integer(offset === undefined ? 0 : offset, "offset");
    length = integer(length === undefined ? view.byteLength - offset : length, "length");
    if (offset > view.byteLength || length > view.byteLength - offset) invalid("offset and length must fit the buffer");
    return {buffer, view, offset, length, position: position(pos)};
  }
  function handle(value) {
    return {
      read(buffer, offset, length, pos) {
        try {
          if (buffer === undefined || !ArrayBuffer.isView(buffer)) {
            const opts = options(buffer === undefined ? {} : buffer);
            buffer = opts.buffer === undefined ? new Uint8Array(16384) : opts.buffer;
            ({offset, length, position: pos} = opts);
          } else if (offset !== null && typeof offset === "object") {
            ({offset, length, position: pos} = options(offset));
          }
          const r = range(buffer, offset, length, pos);
          return value.read(r.position, r.length).then(result => {
            r.view.set(result.data, r.offset);
            return {bytesRead: result.bytesRead, buffer};
          }, error => {
            if (error.data !== undefined) {
              r.view.set(error.data, r.offset);
              delete error.data;
              error.buffer = buffer;
            }
            throw error;
          });
        } catch (error) { return Promise.reject(error); }
      },
      write(buffer, offset, length, pos) {
        try {
          let writing;
          if (typeof buffer === "string") {
            const at = position(offset);
            if (length !== undefined && length !== "utf8" && length !== "utf-8") invalid("only UTF-8 string writes are supported");
            writing = value.write(at, buffer);
          } else {
            if (offset !== null && typeof offset === "object") {
              ({offset, length, position: pos} = options(offset));
            }
            const r = range(buffer, offset, length, pos);
            writing = value.write(r.position, r.view.subarray(r.offset, r.offset + r.length));
          }
          return writing.then(bytesWritten => ({bytesWritten, buffer}), error => {
            if (error.bytesWritten !== undefined) error.buffer = buffer;
            throw error;
          });
        } catch (error) { return Promise.reject(error); }
      },
      stat() { return value.stat(); },
      truncate(length = 0) {
        try {
          if (!Number.isSafeInteger(length)) invalid("length must be a safe integer");
          return value.truncate(Math.max(0, length));
        } catch (error) { return Promise.reject(error); }
      },
      sync() { return value.sync(); },
      close() { return value.close(); },
    };
  }
  function open(path, flags = "r", mode = 0o666) {
    try {
      if (typeof path !== "string" || path.length === 0 || path.includes("\0")) invalid("path must be a nonempty string without NUL");
      if (!["r", "r+", "w", "wx", "w+", "wx+", "a", "ax", "a+", "ax+"].includes(flags)) invalid("unsupported file open flags");
      mode = fileMode(mode);
      return native.open(path, flags, mode).then(handle);
    } catch (error) { return Promise.reject(error); }
  }
  return {open, handle};
})
