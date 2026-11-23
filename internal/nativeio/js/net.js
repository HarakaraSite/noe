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
      async [Symbol.asyncDispose]() { await this.close(); },
    };
  }

  async function socketOperation(signal, start) {
    if (signal === undefined) return await start().promise;

    const signals = host.abortSignals();
    if (!signals || !signals.has(signal)) throw new TypeError("signal must be an AbortSignal");
    if (signals.aborted(signal)) throw signals.reason(signal);

    let operation;
    const cancel = () => { if (operation) operation.cancel(); };
    signals.add(signal, cancel);
    try {
      operation = start();
      if (signals.aborted(signal)) cancel();
      return await operation.promise;
    } catch (error) {
      if (signals.aborted(signal)) throw signals.reason(signal);
      throw error;
    } finally {
      signals.remove(signal, cancel);
    }
  }

  function tlsOptions(options, request, listening) {
    for (const name of ["cert", "key", "ca", "serverName"]) {
      const value = options[name];
      if (value === undefined) continue;
      if (request.network !== "tls") throw new TypeError("TLS options require network tls");
      if (listening && (name === "ca" || name === "serverName"))
        throw new TypeError("ca and serverName are connect-only options");
      if (typeof value !== "string" || value.length === 0)
        throw new TypeError("TLS options must be nonempty PEM or server-name strings");
      request[name] = value;
    }
    if ((request.cert === undefined) !== (request.key === undefined))
      throw new TypeError("TLS requires cert and key together");
  }

  async function connect(options) {
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("options must be an object");

    const network = options.network;
    const request = { network };
    if (network === "tcp" || network === "tls") {
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
      throw new TypeError("network must be tcp, tls, or unix");
    }

    tlsOptions(options, request, false);

    return streams(await socketOperation(options.signal, () => host.connect(JSON.stringify(request))));
  }

  async function listen(options) {
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("options must be an object");

    const network = options.network;
    const request = { network };
    if (network === "tcp" || network === "tls") {
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
      throw new TypeError("network must be tcp, tls, or unix");
    }

    tlsOptions(options, request, true);

    const native = await socketOperation(options.signal, () => host.listen(JSON.stringify(request)));
    return {
      address: native.address,
      accept: () => native.accept().then(streams),
      close: () => native.close(),
      async [Symbol.asyncDispose]() { await this.close(); },
    };
  }

  function datagramAddress(address, binding) {
    if (address === null || typeof address !== "object" || Array.isArray(address))
      throw new TypeError("UDP address must be an object");
    const network = address.network, host = address.host, port = address.port;
    if (network !== "udp") throw new TypeError("UDP addresses require network udp");
    if (typeof host !== "string" || host.length === 0 || host.includes("\0"))
      throw new TypeError("UDP host must be a nonempty string without NUL");
    if (!Number.isInteger(port) || port < (binding ? 0 : 1) || port > 65535)
      throw new RangeError("UDP port is out of range");
    return { network, host, port };
  }

  async function datagramOperation(options, start) {
    if (options === undefined) options = {};
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("operation options must be an object");
    return socketOperation(options.signal, start);
  }

  async function bind(options) {
    const request = datagramAddress(options, true);
    tlsOptions(options, request, false);
    const socket = await datagramOperation(options, () => host.bind(JSON.stringify(request)));
    let closed = false, closePromise;
    return {
      address: socket.address,
      async send(data, address, options) {
        if (closed) throw new Error("UDP socket is closed");
        if (!(data instanceof Uint8Array)) throw new TypeError("send requires Uint8Array");
        const target = datagramAddress(address, false);
        return datagramOperation(options, () => socket.send(data, JSON.stringify(target)));
      },
      async receive(options) {
        if (closed) throw new Error("UDP socket is closed");
        return datagramOperation(options, () => socket.receive());
      },
      close() {
        if (!closePromise) {
          closed = true;
          closePromise = socket.close().then(() => undefined);
        }
        return closePromise;
      },
      async [Symbol.asyncDispose]() { await this.close(); },
    };
  }

  return { connect, listen, bind };
})
