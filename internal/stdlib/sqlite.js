(native => {
  "use strict";

  const states = new WeakMap();
  const statements = new WeakMap();
  const Signal = AbortSignal;
  const Controller = AbortController;
  const signalAny = Signal.any.bind(Signal);
  const MAX_SAFE = BigInt(Number.MAX_SAFE_INTEGER);
  const MIN_INT64 = -(1n << 63n), MAX_INT64 = (1n << 63n) - 1n;
  const EMPTY = {positional: [], named: []};

  function invalid(message) {
    const error = new TypeError(message);
    error.code = "ERR_INVALID_ARG";
    throw error;
  }
  function closed(message = "the database is closed") {
    const error = new Error(message);
    error.code = "ERR_SQLITE_CLOSED";
    return error;
  }
  function options(value = {}) {
    if (value === null || typeof value !== "object" || Array.isArray(value)) invalid("options must be an object");
    const signal = value.signal;
    if (signal !== undefined && !(signal instanceof Signal)) invalid("signal must be an AbortSignal");
    return {value, signal};
  }
  function sqlText(sql) {
    if (typeof sql !== "string" || sql.includes("\0")) invalid("SQL must be a string without NUL");
    return sql;
  }
  function cell(value) {
    if (value === null || value === undefined) return {kind: "null"};
    if (typeof value === "boolean") return {kind: "integer", text: value ? "1" : "0"};
    if (typeof value === "bigint") {
      if (value < MIN_INT64 || value > MAX_INT64) invalid("SQLite integers must fit signed 64 bits");
      return {kind: "integer", text: String(value)};
    }
    if (typeof value === "number") {
      if (!Number.isFinite(value)) invalid("SQLite numbers must be finite");
      if (Number.isInteger(value)) {
        if (!Number.isSafeInteger(value)) invalid("use bigint for integers outside the safe number range");
        return {kind: "integer", text: String(value)};
      }
      return {kind: "number", value};
    }
    if (typeof value === "string") return {kind: "text", text: value};
    if (ArrayBuffer.isView(value)) {
      return {kind: "blob", bytes: new Uint8Array(value.buffer, value.byteOffset, value.byteLength).slice()};
    }
    if (value instanceof ArrayBuffer || (typeof SharedArrayBuffer === "function" && value instanceof SharedArrayBuffer)) {
      return {kind: "blob", bytes: new Uint8Array(value).slice()};
    }
    invalid("unsupported SQLite parameter type");
  }
  function parameters(value = []) {
    if (Array.isArray(value)) return {positional: Array.from(value, cell), named: []};
    if (value === null || typeof value !== "object" || ArrayBuffer.isView(value) || value instanceof ArrayBuffer) {
      invalid("parameters must be an array or a named-parameter object");
    }
    const entries = Object.entries(value).map(([name, value]) => [name, cell(value)]);
    return {positional: [], named: entries};
  }
  function integer(text) {
    const value = BigInt(text);
    return value >= -MAX_SAFE && value <= MAX_SAFE ? Number(value) : value;
  }
  function decodeCell(value) {
    switch (value.kind) {
      case "null": return null;
      case "integer": return integer(value.text);
      case "number": return value.value;
      case "text": return value.text;
      case "blob": return value.bytes;
      default: throw new Error("invalid SQLite result cell");
    }
  }
  function row(value) {
    if (value === null || value === undefined) return undefined;
    const result = Object.create(null);
    for (let i = 0; i < value.columns.length; i++) result[value.columns[i]] = decodeCell(value.values[i]);
    return result;
  }
  function decode(operation, value) {
    if (operation === "run") return {changes: decodeCell(value.changes), lastInsertRowid: decodeCell(value.lastInsertRowid)};
    if (operation === "get") return row(value);
    if (operation === "all") return value.map(row);
    return undefined;
  }

  // The queue tracks actual native completion, even when an abort lets the
  // caller stop waiting earlier. The next operation cannot overtake it.
  function cancellation(signal) {
    let cancel, reject, aborted = false;
    const failure = signal ? new Promise((_, fail) => { reject = fail; }) : null;
    // A queued operation may not reach its race until the preceding work ends.
    if (failure) failure.catch(() => {});
    const abort = () => {
      if (aborted) return;
      aborted = true;
      if (cancel) cancel();
      reject(signal.reason);
    };
    if (signal) {
      signal.addEventListener("abort", abort, {once: true});
      if (signal.aborted) abort();
    }
    return {
      signal,
      check() { if (signal && signal.aborted) throw signal.reason; },
      register(value) { cancel = value; if (aborted) cancel(); },
      race(promise) { return failure ? Promise.race([promise, failure]) : promise; },
      cleanup() { if (signal) signal.removeEventListener("abort", abort); cancel = null; },
    };
  }
  function enqueue(queue, work, signal, allowFailed = false) {
    const operation = cancellation(signal);
    const result = queue.tail.then(() => {
      operation.check();
      if (!allowFailed && queue.recordFailure && queue.failed !== undefined) throw queue.failed.error;
      return work(operation);
    });
    const completed = result.finally(() => operation.cleanup());
    queue.tail = completed.catch(error => { if (queue.recordFailure && queue.failed === undefined) queue.failed = {error}; });
    return operation.race(completed);
  }
  function dbState(database) {
    const state = states.get(database);
    if (!state || state.transaction) invalid("invalid database receiver");
    return state;
  }
  function ownerState(target) {
    const state = states.get(target);
    if (!state) invalid("invalid SQLite receiver");
    if (state.transaction ? !state.active : state.closed) throw closed(state.transaction ? "the transaction has ended" : undefined);
    if (state.transaction && state.queue.failed !== undefined) throw state.queue.failed.error;
    return state;
  }
  function request(state, operation, sql = "", params = EMPTY, statementID = 0, register = () => {}) {
    return native.request(state.db.id, statementID, operation, sql, params, register);
  }
  function schedule(state, work, signal) {
    if (state.transaction) signal = signal ? signalAny([state.controller.signal, signal]) : state.controller.signal;
    return enqueue(state.queue, work, signal);
  }
  async function execute(target, operation, sql, params, opts) {
    const state = ownerState(target);
    const text = sqlText(sql), values = parameters(params), {signal} = options(opts);
    return schedule(state, async op => decode(operation, await request(state, operation, text, values, 0, op.register)), signal);
  }
  async function prepare(target, sql, opts) {
    const state = ownerState(target), text = sqlText(sql), {signal} = options(opts);
    return schedule(state, async op => {
      const result = await request(state, "prepare", text, EMPTY, 0, op.register);
      const statement = new Statement(state, result.statementID, text);
      if (state.transaction) state.prepared.add(statement);
      return statement;
    }, signal);
  }
  async function executeStatement(target, operation, params, opts) {
    const statement = statements.get(target);
    if (!statement) invalid("invalid statement receiver");
    if (statement.closed) throw closed("the statement is closed");
    const owner = ownerState(statement.owner.object);
    const values = parameters(params), {signal} = options(opts);
    return schedule(owner, async op => decode(operation, await request(owner, operation, "", values, statement.id, op.register)), signal);
  }

  class QueryMethods {
    async exec(sql, opts) {
      const state = ownerState(this), text = sqlText(sql), {signal} = options(opts);
      return schedule(state, async op => { await request(state, "exec", text, EMPTY, 0, op.register); }, signal);
    }
    run(sql, params, opts) { return execute(this, "run", sql, params, opts); }
    get(sql, params, opts) { return execute(this, "get", sql, params, opts); }
    all(sql, params, opts) { return execute(this, "all", sql, params, opts); }
    prepare(sql, opts) { return prepare(this, sql, opts); }
  }

  class Statement {
    constructor(owner, id, sql) {
      native.brand(this);
      statements.set(this, {owner, id, sql, closed: false, finalized: false, closing: null});
    }
    get sql() { return statements.get(this).sql; }
    get closed() {
      const state = statements.get(this);
      return state.closed || (state.owner.transaction ? !state.owner.active : state.owner.closed);
    }
    run(params, opts) { return executeStatement(this, "run", params, opts); }
    get(params, opts) { return executeStatement(this, "get", params, opts); }
    all(params, opts) { return executeStatement(this, "all", params, opts); }
    close() {
      const state = statements.get(this);
      if (!state) return Promise.reject(new TypeError("invalid statement receiver"));
      if (state.closing) return state.closing;
      state.closed = true;
      const owner = state.owner;
      if (owner.transaction ? !owner.active : owner.closed) return state.closing = Promise.resolve();
      state.closing = enqueue(owner.queue, () => request(owner, "finalize", "", EMPTY, state.id), undefined, true).then(() => { state.finalized = true; });
      return state.closing;
    }
    [Symbol.asyncDispose]() { return this.close(); }
  }

  async function finalizeTransaction(state) {
    for (const statement of state.prepared) {
      const prepared = statements.get(statement);
      if (!prepared.finalized) {
        prepared.closed = true;
        await request(state, "finalize", "", EMPTY, prepared.id);
        prepared.finalized = true;
      }
    }
    state.prepared.clear();
  }

  class Database extends QueryMethods {
    constructor(id, path, readOnly) {
      super();
      native.brand(this);
      const state = {object: this, id, path, readOnly, closed: false, closing: null, queue: {tail: Promise.resolve()}, transaction: false};
      state.db = state;
      states.set(this, state);
    }
    get path() { return dbState(this).path; }
    get readOnly() { return dbState(this).readOnly; }
    get closed() { return dbState(this).closed; }
    async transaction(callback, opts) {
      const database = ownerState(this), {value, signal} = options(opts);
      if (typeof callback !== "function") invalid("transaction callback must be a function");
      const mode = value.mode === undefined ? "deferred" : value.mode;
      if (!["deferred", "immediate", "exclusive"].includes(mode)) invalid("unsupported transaction mode");
      return schedule(database, async op => {
        const controller = new Controller();
        const transaction = new QueryMethods();
        native.brand(transaction);
        const state = {object: transaction, db: database, transaction: true, active: true, controller, prepared: new Set(), queue: {tail: Promise.resolve(), recordFailure: true}};
        states.set(transaction, state);
        const stop = () => controller.abort(signal.reason);
        if (signal) signal.addEventListener("abort", stop, {once: true});
        let begun = false;
        try {
          op.check();
          await request(database, "begin", "BEGIN " + mode.toUpperCase(), EMPTY, 0, op.register);
          begun = true;
          op.check();
          const result = await op.race(Promise.resolve().then(() => callback(transaction)));
          state.active = false;
          await state.queue.tail;
          if (state.queue.failed !== undefined) throw state.queue.failed.error;
          op.check();
          await finalizeTransaction(state);
          await request(database, "commit", "", EMPTY, 0, op.register);
          return result;
        } catch (error) {
          state.active = false;
          controller.abort(error);
          await state.queue.tail;
          let cleanupError;
          try { await finalizeTransaction(state); } catch (failure) { cleanupError = failure; }
          if (begun) {
            try { await request(database, "rollback"); } catch (failure) { cleanupError = cleanupError === undefined ? failure : new AggregateError([cleanupError, failure], "transaction cleanup failed"); }
          }
          if (cleanupError !== undefined) throw new AggregateError([error, cleanupError], "transaction and cleanup failed");
          throw error;
        } finally {
          state.active = false;
          if (signal) signal.removeEventListener("abort", stop);
        }
      }, signal);
    }
    close() {
      const state = dbState(this);
      if (state.closing) return state.closing;
      state.closed = true;
      state.closing = enqueue(state.queue, () => request(state, "close"), undefined).then(() => {});
      return state.closing;
    }
    [Symbol.asyncDispose]() { return this.close(); }
  }

  async function open(path = ":memory:", opts) {
    if (typeof path !== "string" || path === "" || path.includes("\0")) invalid("path must be a nonempty string without NUL");
    const {value, signal} = options(opts);
    const readOnly = value.readOnly === undefined ? false : value.readOnly;
    if (typeof readOnly !== "boolean") invalid("readOnly must be boolean");
    const busyTimeout = value.busyTimeout === undefined ? 0 : value.busyTimeout;
    if (!Number.isSafeInteger(busyTimeout) || busyTimeout < 0 || busyTimeout > 2147483647) invalid("busyTimeout must be an integer from 0 through 2147483647");
    const operation = cancellation(signal);
    let result;
    try {
      operation.check();
      // Opening must settle before reporting an abort so a late connection is
      // always closed instead of losing its only public owner.
      result = await native.open(path, readOnly, busyTimeout, operation.register);
      operation.check();
      return new Database(result.id, path, readOnly);
    } catch (error) {
      if (result) await native.request(result.id, 0, "close", "", EMPTY, () => {});
      if (signal && signal.aborted) throw signal.reason;
      throw error;
    } finally { operation.cleanup(); }
  }

  return {open};
})
