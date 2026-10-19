(native => {
  "use strict";

  function invalid(message) {
    const error = new TypeError(message);
    error.code = "ERR_INVALID_ARG";
    return Promise.reject(error);
  }

  function safeInteger(value, name) {
    return Number.isSafeInteger(value) && value >= 0
      ? null
      : invalid(name + " must be a nonnegative safe integer");
  }

  function handle(value) {
    return {
      readAt(position, length) {
        let error = safeInteger(position, "position");
        if (error) return error;
        error = safeInteger(length, "length");
        if (error) return error;
        return value.readAt(position, length);
      },
      writeAt(position, bytes) {
        const error = safeInteger(position, "position");
        if (error) return error;
        if (!(bytes instanceof Uint8Array)) return invalid("bytes must be a Uint8Array");
        return value.writeAt(position, bytes);
      },
      close() { return value.close(); },
    };
  }

  function open(path, options = {}) {
    if (typeof path !== "string" || path.length === 0 || path.includes("\0"))
      return invalid("path must be a nonempty string without NUL");
    if (options === null || typeof options !== "object" || Array.isArray(options))
      return invalid("options must be an object");
    const mode = options.mode === undefined ? "r" : options.mode;
    if (mode !== "r" && mode !== "r+") return invalid('mode must be "r" or "r+"');
    return native.open(path, mode).then(handle);
  }

  return { open };
})
