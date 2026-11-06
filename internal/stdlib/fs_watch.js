((api) => {
  const combine = globalThis.AbortSignal?.any;
  const done = value => ({value, done: true});
  let Watcher;

  function invalid(message, unsupported = false) {
    const error = unsupported ? new Error(message) : new TypeError(message);
    error.code = unsupported ? "ERR_NOT_SUPPORTED" : "ERR_INVALID_ARG";
    throw error;
  }

  function optionsFor(path, options) {
    if (typeof path !== "string" || path.length === 0 || path.includes("\0")) {
      invalid("watch path must be a nonempty string without NUL");
    }
    if (typeof options === "string") options = {encoding: options};
    if (options === undefined || options === null) options = {};
    if (typeof options !== "object") invalid("watch options must be an object or encoding string");
    const {
      encoding = "utf8", persistent = true, recursive = false,
      signal, maxQueue = 2048, overflow = "ignore", ignore,
      throwIfNoEntry = true,
    } = options;
    if (encoding !== "utf8" && encoding !== "utf-8" && encoding !== "buffer") {
      invalid("watch encoding must be utf8, utf-8, or buffer");
    }
    if (encoding === "buffer" && typeof Buffer === "undefined") invalid("buffer watch encoding needs node:buffer", true);
    if (typeof persistent !== "boolean" || typeof recursive !== "boolean" || typeof throwIfNoEntry !== "boolean") {
      invalid("watch persistent, recursive, and throwIfNoEntry options must be booleans");
    }
    if (recursive) invalid("recursive filesystem watching is not supported", true);
    if (ignore !== undefined || !throwIfNoEntry) {
      invalid("watch ignore and throwIfNoEntry:false are not supported", true);
    }
    if (!Number.isSafeInteger(maxQueue) || maxQueue < 1) invalid("watch maxQueue must be a positive safe integer");
    if (overflow !== "ignore" && overflow !== "error") invalid("watch overflow must be ignore or error");
    // The existing combination primitive validates genuine signals and keeps
    // synthetic abort events from closing a watcher.
    if (signal !== undefined && typeof combine !== "function") invalid("watch signals need the web APIs", true);
    return {
      path, persistent, maxQueue, overflow, bytes: encoding === "buffer",
      signal: signal === undefined ? undefined : combine.call(AbortSignal, [signal]),
    };
  }

  function open(options) {
    return api.open(options.path, options.persistent, options.maxQueue, options.overflow === "ignore", options.bytes);
  }

  function eventOf(item, options) {
    if (item.dropped > 0) console.warn(`Filesystem watch dropped ${item.dropped} events because its queue was full`);
    const {eventType, filename} = item.value;
    return {eventType, filename: options.bytes && filename !== null ? Buffer.from(filename) : filename};
  }

  function abortError(signal) {
    const error = new Error("The operation was aborted", {cause: signal.reason});
    error.name = "AbortError";
    error.code = "ABORT_ERR";
    return error;
  }

  function watcherClass() {
    if (Watcher) return Watcher;
    const EventEmitter = api.events();
    Watcher = class FSWatcher extends EventEmitter {
      #native;
      #options;
      #closed = false;
      #closing;
      #abort;

      constructor(options, listener) {
        super();
        this.#options = options;
        if (listener !== undefined) this.on("change", listener);
        this.#abort = () => this.close();
        if (options.signal?.aborted) {
          this.close();
          return;
        }
        this.#native = open(options);
        options.signal?.addEventListener("abort", this.#abort, {once: true});
        this.#pump().catch(error => {
          try { this.close(); } finally { api.report(error); }
        });
      }

      async #pump() {
        while (!this.#closed) {
          let item;
          try {
            item = await this.#native.next();
          } catch (error) {
            if (this.#closed) return;
            this.close();
            this.emit("error", error);
            return;
          }
          if (this.#closed) return;
          if (item.done) {
            this.close();
            return;
          }
          const {eventType, filename} = eventOf(item, this.#options);
          this.emit("change", eventType, filename);
        }
      }

      close() {
        if (this.#closed) return;
        this.#closed = true;
        this.#options.signal?.removeEventListener("abort", this.#abort);
        this.#native?.close();
        this.#closing = Promise.resolve().then(() => {
          try { this.emit("close"); } catch (error) { api.report(error); }
        });
      }

      ref() { if (!this.#closed) this.#native?.ref(); return this; }
      unref() { if (!this.#closed) this.#native?.unref(); return this; }
      async [Symbol.asyncDispose]() { this.close(); await this.#closing; }
    };
    return Watcher;
  }

  function watch(path, options, listener) {
    if (typeof options === "function") { listener = options; options = undefined; }
    if (listener !== undefined && typeof listener !== "function") invalid("watch listener must be a function");
    const parsed = optionsFor(path, options);
    return new (watcherClass())(parsed, listener);
  }

  function watchPromises(path, options) {
    const parsed = optionsFor(path, options);
    let native, closed = false, failure, unreported = false, pending = 0;
    const onAbort = () => {
      failure = abortError(parsed.signal);
      unreported = pending === 0;
      stop();
    };
    function stop() {
      if (closed) return;
      closed = true;
      parsed.signal?.removeEventListener("abort", onAbort);
      native?.close();
    }
    function next() {
      if (closed) {
        if (unreported) { unreported = false; return Promise.reject(failure); }
        return Promise.resolve(done());
      }
      try {
        if (!native) {
          if (parsed.signal?.aborted) {
            onAbort();
            return next();
          }
          native = open(parsed);
          parsed.signal?.addEventListener("abort", onAbort, {once: true});
        }
        pending++;
        return native.next().then(item => {
          if (failure !== undefined) throw failure;
          if (closed) return done();
          if (item.done) { stop(); return done(); }
          return {value: eventOf(item, parsed), done: false};
        }).catch(error => {
          stop();
          throw error;
        }).finally(() => { pending--; });
      } catch (error) {
        stop();
        return Promise.reject(error);
      }
    }
    return {
      next,
      async close() { stop(); unreported = false; },
      async return(value) { await this.close(); return done(value); },
      async throw(error) { await this.close(); throw error; },
      async [Symbol.asyncDispose]() { await this.close(); },
      [Symbol.asyncIterator]() { return this; },
    };
  }

  return {watch, watchPromises};
})
