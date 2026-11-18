(native => {
  "use strict";

  const combine = globalThis.AbortSignal?.any;
  const states = new WeakMap();
  const done = value => ({value, done: true});

  function invalid(message) {
    const error = new TypeError(message);
    error.code = "ERR_INVALID_ARG";
    throw error;
  }
  function stateOf(object) {
    const state = states.get(object);
    if (!state) throw new TypeError("invalid directory iterator receiver");
    return state;
  }
  function detach(state) {
    state.signal?.removeEventListener("abort", state.abort);
    state.entries = [];
    state.index = 0;
  }
  function stop(state) {
    if (!state.closing) {
      state.ended = true;
      detach(state);
      // Mark native closure immediately, even while opening/reading. Join the
      // current native read as well, so a late open cannot outlive close().
      const reading = state.reading;
      const closing = state.handle ? state.handle.close() : Promise.resolve();
      state.closing = Promise.all([closing, reading?.catch(() => {})]).then(() => undefined);
      state.closing.catch(() => {});
    }
    return state.closing;
  }
  function enqueue(state, work) {
    const result = state.pending ? state.pending.then(work, work) : work();
    state.pending = result;
    const settled = () => { if (state.pending === result) state.pending = null; };
    result.then(settled, settled);
    return result;
  }
  function checkAbort(state) {
    if (state.aborted && !state.abortDelivered) {
      state.abortDelivered = true;
      throw state.signal.reason;
    }
  }
  async function nextEntry(state) {
    try {
      checkAbort(state);
      if (state.ended) return done();
      state.handle ??= native.create(state.path);
      state.handle.check();
      if (state.index === state.entries.length) {
        const reading = state.handle.read(state.batchSize);
        state.reading = reading;
        let batch;
        try { batch = await reading; }
        catch (error) {
          checkAbort(state);
          if (state.ended) return done();
          throw error;
        }
        finally { if (state.reading === reading) state.reading = null; }
        checkAbort(state);
        if (state.ended) return done();
        state.entries = batch.entries;
        state.index = 0;
        state.eof = batch.done;
        if (state.entries.length === 0) {
          await stop(state);
          return done();
        }
      }
      const value = state.entries[state.index];
      state.entries[state.index++] = undefined;
      if (state.index === state.entries.length && state.eof) {
        state.ended = true;
        detach(state);
        state.closing = Promise.resolve();
      }
      return {value, done: false};
    } catch (error) {
      let cleanupError;
      try { await stop(state); } catch (failure) { cleanupError = {value: failure}; }
      if (state.aborted) {
        state.abortDelivered = true;
        throw state.signal.reason;
      }
      if (cleanupError) throw new AggregateError([error, cleanupError.value], "directory iteration and cleanup failed");
      throw error;
    }
  }
  class DirectoryIterator {
    constructor(path, batchSize, signal) {
      const state = {path, batchSize, signal, entries: [], index: 0, eof: false,
        ended: false, closing: null, pending: null, reading: null, handle: null,
        aborted: false, abortDelivered: false, abort: null};
      states.set(this, state);
      state.abort = () => {
        state.aborted = true;
        stop(state);
      };
      if (signal) {
        signal.addEventListener("abort", state.abort, {once: true});
        if (signal.aborted) state.abort();
      }
    }
    get closed() { return stateOf(this).ended; }
    next() {
      const state = stateOf(this);
      return enqueue(state, () => nextEntry(state));
    }
    close() { return stop(stateOf(this)); }
    return(value = undefined) { return stop(stateOf(this)).then(() => done(value)); }
    throw(error) { return stop(stateOf(this)).then(() => { throw error; }); }
    [Symbol.asyncIterator]() { stateOf(this); return this; }
    [Symbol.asyncDispose]() { return stop(stateOf(this)); }
  }
  function iterateDir(path, options = {}) {
    if (typeof path !== "string" || path.length === 0 || path.includes("\0")) invalid("path must be a nonempty string without NUL");
    if (options === null || typeof options !== "object" || Array.isArray(options)) invalid("options must be an object");
    const {batchSize = 128, signal} = options;
    if (!Number.isSafeInteger(batchSize) || batchSize <= 0) invalid("batchSize must be a positive safe integer");
    if (signal !== undefined && typeof combine !== "function") invalid("signal requires AbortSignal support");
    return new DirectoryIterator(path, batchSize, signal === undefined ? undefined : combine.call(AbortSignal, [signal]));
  }
  return {iterateDir};
})
