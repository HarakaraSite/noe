(host => {
  "use strict";
  const noop = () => {};

  function readable(endpoint) {
    let stopped = false;
    return new ReadableStream({
      async pull(controller) {
        const chunk = await endpoint.read();
        if (stopped) return;
        if (chunk === null) { stopped = true; controller.close(); }
        else controller.enqueue(chunk);
      },
      cancel() { stopped = true; return endpoint.close(); },
    }, { highWaterMark: 0 });
  }

  function writable(endpoint) {
    return new WritableStream({
      write(chunk) {
        if (!(chunk instanceof Uint8Array)) throw new TypeError("write requires Uint8Array");
        return endpoint.write(chunk);
      },
      close() { return endpoint.close(); },
      abort() { return endpoint.close(); },
    });
  }

  async function pipe(source, destination, { signal, preventClose = false } = {}) {
    const reader = source.getReader();
    let writer;
    try { writer = destination.getWriter(); }
    catch (error) { reader.releaseLock(); throw error; }
    let aborted = false, reason, stopping;
    const stop = error => {
      if (!stopping) {
        // Start both cancellations before awaiting either: either endpoint may
        // currently be waiting on its peer. Handle both rejections immediately.
        stopping = Promise.allSettled([reader.cancel(error), writer.abort(error)]);
      }
      return stopping;
    };
    const abort = () => {
      aborted = true;
      reason = signal.reason === undefined ? new Error("The operation was aborted") : signal.reason;
      stop(reason);
    };
    try {
      if (signal) {
        signal.addEventListener("abort", abort, { once: true });
        if (signal.aborted) abort();
      }
      while (!aborted) {
        const { value, done } = await reader.read();
        if (aborted) break;
        if (done) {
          if (!preventClose) await writer.close();
          if (aborted) throw reason;
          return;
        }
        await writer.write(value);
      }
      throw reason;
    } catch (error) {
      await stop(aborted ? reason : error);
      throw aborted ? reason : error;
    } finally {
      if (signal) signal.removeEventListener("abort", abort);
      reader.releaseLock();
      writer.releaseLock();
    }
  }

  async function spawn(command, args = [], options = {}) {
    if (typeof command !== "string" || !command || command.includes("\0"))
      throw new TypeError("command must be a nonempty string without NUL");
    if (!Array.isArray(args) || args.some(v => typeof v !== "string" || v.includes("\0")))
      throw new TypeError("args must be an array of strings without NUL");
    if (options === null || typeof options !== "object" || Array.isArray(options))
      throw new TypeError("options must be an object");
    const modes = {};
    for (const key of ["stdin", "stdout", "stderr"]) {
      modes[key] = options[key] === undefined ? "pipe" : options[key];
      if (!["pipe", "inherit", "ignore"].includes(modes[key]))
        throw new TypeError(key + " must be pipe, inherit, or ignore");
    }
    if (options.cwd !== undefined && (typeof options.cwd !== "string" || options.cwd.includes("\0")))
      throw new TypeError("cwd must be a string without NUL");
    const env = { ...process.env };
    if (options.env !== undefined) {
      if (options.env === null || typeof options.env !== "object" || Array.isArray(options.env))
        throw new TypeError("env must be an object");
      for (const [key, value] of Object.entries(options.env)) {
        if (!key || key.includes("=") || key.includes("\0") || (value !== null && (typeof value !== "string" || value.includes("\0"))))
          throw new TypeError("env entries must have valid names and string or null values");
        // Windows environment names are case insensitive.
        if (process.platform === "win32") {
          for (const existing of Object.keys(env)) if (existing.toLowerCase() === key.toLowerCase()) delete env[existing];
        }
        if (value === null) delete env[key]; else env[key] = value;
      }
    }
    const child = await host.spawn(JSON.stringify({ command, args, cwd: options.cwd === undefined ? process.cwd() : options.cwd, env, ...modes }));
    child.exited.catch(noop);
    return {
      pid: child.pid,
      stdin: child.stdin ? writable(child.stdin) : null,
      stdout: child.stdout ? readable(child.stdout) : null,
      stderr: child.stderr ? readable(child.stderr) : null,
      exited: child.exited,
      kill(signal = "SIGTERM") {
        if (typeof signal !== "string") return Promise.reject(new TypeError("signal must be a string"));
        return child.kill(signal);
      },
      close: () => child.close(),
    };
  }

  return { stdin: readable(host.stdin), stdout: writable(host.stdout), stderr: writable(host.stderr), pipe, spawn };
})
