(host => {
  "use strict";

  function streams(socket) {
    let readStopped = false;
    let writeStopped = false;
    let readController;
    let writeController;
    let closePromise;

    const readable = new ReadableStream({
      start(controller) { readController = controller; },
      async pull(controller) {
        readController = controller;
        if (readStopped) {
          controller.close();
          return;
        }
        try {
          const chunk = await socket.read();
          if (readStopped) return;
          if (chunk === null) {
            readStopped = true;
            controller.close();
          } else {
            controller.enqueue(chunk);
          }
        } catch (error) {
          if (!readStopped) {
            readStopped = true;
            controller.error(error);
          }
        }
      },
      cancel() {
        readStopped = true;
        return socket.cancelRead();
      },
    }, { highWaterMark: 0 });

    const writable = new WritableStream({
      write(chunk) {
        if (!(chunk instanceof Uint8Array)) throw new TypeError("write requires Uint8Array");
        if (writeStopped) throw new TypeError("socket write side is closed");
        return socket.write(chunk);
      },
      close() {
        if (writeStopped) return;
        writeStopped = true;
        return socket.closeWrite();
      },
      abort() {
        if (writeStopped) return;
        writeStopped = true;
        return socket.abortWrite();
      },
      start(controller) { writeController = controller; },
    });

    return {
      readable,
      writable,
      close() {
        if (!closePromise) {
          const reason = new Error("connection is closed");
          if (!readStopped) {
            readStopped = true;
            if (readController) readController.error(reason);
          }
          if (!writeStopped) {
            writeStopped = true;
            if (writeController) writeController.error(reason);
          }
          closePromise = socket.close();
        }
        return closePromise;
      },
    };
  }

  function abortReason(signal) {
    return signal.reason === undefined ? new Error("The operation was aborted") : signal.reason;
  }

  async function connect(options) {
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("options must be an object");

    const network = options.network;
    const request = { network };
    if (network === "tcp") {
      if (typeof options.host !== "string" || options.host.length === 0 || options.host.includes("\0"))
        throw new TypeError("host must be a nonempty string without NUL");
      if (!Number.isInteger(options.port) || options.port < 1 || options.port > 65535)
        throw new RangeError("port must be an integer between 1 and 65535");
      request.host = options.host;
      request.port = options.port;
    } else if (network === "unix") {
      if (typeof options.path !== "string" || options.path.length === 0 || options.path.includes("\0"))
        throw new TypeError("path must be a nonempty string without NUL");
      request.path = options.path;
    } else {
      throw new TypeError("network must be tcp or unix");
    }

    const signal = options.signal;
    if (signal !== undefined && !(signal instanceof AbortSignal))
      throw new TypeError("signal must be an AbortSignal");
    if (signal && signal.aborted) throw abortReason(signal);

    let operation;
    const cancel = () => { if (operation) operation.cancel(); };
    if (signal) signal.addEventListener("abort", cancel, { once: true });
    try {
      operation = host.connect(JSON.stringify(request));
      if (signal && signal.aborted) cancel();
      return streams(await operation.promise);
    } catch (error) {
      if (signal && signal.aborted) throw abortReason(signal);
      throw error;
    } finally {
      if (signal) signal.removeEventListener("abort", cancel);
    }
  }

  async function listen(options) {
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("options must be an object");

    const network = options.network;
    const request = { network };
    if (network === "tcp") {
      if (typeof options.host !== "string" || options.host.length === 0 || options.host.includes("\0"))
        throw new TypeError("host must be a nonempty string without NUL");
      if (!Number.isInteger(options.port) || options.port < 0 || options.port > 65535)
        throw new RangeError("port must be an integer between 0 and 65535");
      request.host = options.host;
      request.port = options.port;
    } else if (network === "unix") {
      if (typeof options.path !== "string" || options.path.length === 0 || options.path.includes("\0"))
        throw new TypeError("path must be a nonempty string without NUL");
      request.path = options.path;
    } else {
      throw new TypeError("network must be tcp or unix");
    }

    const signal = options.signal;
    if (signal !== undefined && !(signal instanceof AbortSignal))
      throw new TypeError("signal must be an AbortSignal");
    if (signal && signal.aborted) throw abortReason(signal);

    let operation;
    const cancel = () => { if (operation) operation.cancel(); };
    if (signal) signal.addEventListener("abort", cancel, { once: true });
    try {
      operation = host.listen(JSON.stringify(request));
      if (signal && signal.aborted) cancel();
      const native = await operation.promise;
      return {
        address: native.address,
        accept: () => native.accept().then(streams),
        close: () => native.close(),
      };
    } catch (error) {
      if (signal && signal.aborted) throw abortReason(signal);
      throw error;
    } finally {
      if (signal) signal.removeEventListener("abort", cancel);
    }
  }

  return { connect, listen };
})
