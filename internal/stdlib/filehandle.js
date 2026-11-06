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
  function options(value) {
    if (value === undefined) return {};
    if (value === null || typeof value !== "object" || Array.isArray(value)) invalid("options must be an object");
    return value;
  }
  function mode(value) {
    if (!Number.isSafeInteger(value) || value < 0 || value > 0o777) invalid("mode must be an integer from 0 through 0o777");
    return value;
  }
  function bytes(value) {
    if (!ArrayBuffer.isView(value)) invalid("data must be an ArrayBuffer view or string");
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  }
  function handle(value) {
    return {
      read(length = 16384) {
        try {
          integer(length, "length");
          return value.read(null, length).then(result => result.data);
        } catch (error) { return Promise.reject(error); }
      },
      readAt(position, length) {
        try {
          integer(position, "position");
          integer(length, "length");
          return value.read(position, length).then(result => result.data);
        } catch (error) { return Promise.reject(error); }
      },
      write(data) {
        try {
          if (typeof data === "string") return value.write(null, data);
          return value.write(null, bytes(data));
        } catch (error) { return Promise.reject(error); }
      },
      writeAt(position, data) {
        try {
          integer(position, "position");
          if (typeof data === "string") return value.write(position, data);
          return value.write(position, bytes(data));
        } catch (error) { return Promise.reject(error); }
      },
      stat() { return value.stat(); },
      truncate(length = 0) {
        try {
          integer(length, "length");
          return value.truncate(length);
        } catch (error) { return Promise.reject(error); }
      },
      sync() { return value.sync(); },
      close() { return value.close(); },
      async [Symbol.asyncDispose]() { await this.close(); },
    };
  }
  function open(path, suppliedOptions) {
    try {
      if (typeof path !== "string" || path.length === 0 || path.includes("\0")) invalid("path must be a nonempty string without NUL");
      const {flags = "r", mode: permissions = 0o666} = options(suppliedOptions);
      if (typeof flags !== "string" || !["r", "r+", "w", "wx", "w+", "wx+", "a", "ax", "a+", "ax+"].includes(flags)) {
        invalid("unsupported file open flags");
      }
      mode(permissions);
      return native.open(path, flags, permissions).then(handle);
    } catch (error) { return Promise.reject(error); }
  }
  return {open, handle};
})
