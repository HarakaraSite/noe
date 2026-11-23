(native => {
  "use strict";

  const states = new WeakMap();
  const statements = new WeakMap();
  const iterators = new WeakMap();
  const Signal = AbortSignal;
  const Controller = AbortController;
  const signalAny = Signal.any.bind(Signal);
  const signals = native.abortSignals();
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
    if (signal !== undefined && !signals.has(signal)) invalid("signal must be an AbortSignal");
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
  function row(columns, values) {
    const result = Object.create(null);
    for (let i = 0; i < columns.length; i++) result[columns[i]] = decodeCell(values[i]);
    return result;
  }
  function rows(columns, values) {
    return values.map(cells => row(columns, cells));
  }
  function decode(operation, value) {
    if (operation === "run") return {changes: decodeCell(value.changes), lastInsertRowid: decodeCell(value.lastInsertRowid)};
    if (operation === "get") return value === null || value === undefined ? undefined : row(value.columns, value.values);
    if (operation === "all") return rows(value.columns, value.rows);
    return undefined;
  }

  // The queue tracks actual native completion, even when an abort lets the
  // caller stop waiting earlier. The next operation cannot overtake it.
  function cancellation(signal) {
    let cancel, reject, aborted = false;
    const failure = signal ? new Promise((_, fail) => { reject = fail; }) : null;
    // A queued operation may not reach its race until the preceding work ends.
    if (failure) failure.catch(() => {});
    const abort = reason => {
      if (aborted) return;
      aborted = true;
      if (cancel) cancel();
      reject(reason);
    };
    if (signal) {
      if (signals.aborted(signal)) abort(signals.reason(signal));
      else signals.add(signal, abort);
    }
    return {
      signal,
      check() { if (signal && signals.aborted(signal)) throw signals.reason(signal); },
      register(value) { cancel = value; if (aborted) cancel(); },
      race(promise) { return failure ? Promise.race([promise, failure]) : promise; },
      cleanup() { if (signal) signals.remove(signal, abort); cancel = null; },
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

  function iteratorState(value) {
    const state = iterators.get(value);
    if (!state) invalid("invalid SQLite iterator receiver");
    return state;
  }
  function detachIterator(state) {
    state.owner.iterators.delete(state.object);
    if (state.statement) state.statement.iterators.delete(state.object);
    if (state.signal) signals.remove(state.signal, state.abort);
    state.columns = null;
    state.rows = [];
    state.index = 0;
    state.params = EMPTY;
  }
  function finishIterator(state) {
    state.ended = true;
    detachIterator(state);
    state.closing = Promise.resolve();
  }
  function closeIterator(state) {
    if (state.closing) return state.closing;
    state.ended = true;
    detachIterator(state);
    // Enqueue immediately behind actual native completion. In particular an
    // abort race may reject before cursorOpen has returned its resource ID.
    state.closing = enqueue(state.owner.queue, async () => {
      if (state.id) {
        await native.cursorClose(state.owner.db.id, state.id, () => {});
        state.id = 0;
      }
    }, undefined, true);
    return state.closing;
  }
  function iteratorTask(state, work) {
    const result = state.pending ? state.pending.then(work, work) : work();
    state.pending = result;
    const settled = () => { if (state.pending === result) state.pending = null; };
    result.then(settled, settled);
    return result;
  }
  async function nextRow(state) {
    try {
      if (state.aborted && !state.abortDelivered) {
        state.abortDelivered = true;
        throw signals.reason(state.signal);
      }
      if (state.ended) return {value: undefined, done: true};
      ownerState(state.owner.object);
      if (state.statement && state.statement.closed) throw closed("the statement is closed");
      if (state.index === state.rows.length) {
        let batch;
        // The queue's completion promise retains no batch payload. The
        // iterator retains its column names and only the current row batch.
        await schedule(state.owner, async operation => {
          try {
            if (state.ended) return;
            if (!state.id) {
              const opened = await native.cursorOpen(state.owner.db.id,
                state.statement ? state.statement.id : 0, state.sql, state.params, operation.register);
              state.id = opened.cursorID;
              state.params = EMPTY;
            }
            operation.check();
            if (state.ended) return;
            batch = await native.cursorRead(state.owner.db.id, state.id, state.batchSize, operation.register);
            if (batch.done) state.id = 0;
          } catch (error) {
            operation.check();
            throw error;
          }
        }, state.signal);
        if (state.aborted && !state.abortDelivered) {
          state.abortDelivered = true;
          throw signals.reason(state.signal);
        }
        if (state.ended) return {value: undefined, done: true};
        if (batch.columns !== null) state.columns = batch.columns;
        state.rows = rows(state.columns, batch.rows);
        state.index = 0;
        state.eof = batch.done;
        if (state.rows.length === 0) {
          finishIterator(state);
          return {value: undefined, done: true};
        }
      }
      const value = state.rows[state.index];
      state.rows[state.index++] = undefined;
      if (state.index === state.rows.length && state.eof) finishIterator(state);
      return {value, done: false};
    } catch (error) {
      let cleanupError;
      try { await closeIterator(state); } catch (failure) { cleanupError = failure; }
      if (state.signal && signals.aborted(state.signal)) {
        state.abortDelivered = true;
        throw signals.reason(state.signal);
      }
      if (cleanupError !== undefined) throw new AggregateError([error, cleanupError], "query iteration and cleanup failed");
      throw error;
    }
  }
  class RowIterator {
    constructor(owner, statement, sql, params, opts) {
      native.brand(this);
      const {value, signal: selectedSignal} = options(opts);
      const batchSize = value.batchSize === undefined ? 128 : value.batchSize;
      if (!Number.isSafeInteger(batchSize) || batchSize <= 0) invalid("batchSize must be a positive safe integer");
      const signal = owner.transaction
        ? selectedSignal ? signalAny([owner.controller.signal, selectedSignal]) : owner.controller.signal
        : selectedSignal;
      const state = {object: this, owner, statement, sql, params, signal, batchSize,
        id: 0, columns: null, rows: [], index: 0, eof: false, ended: false, closing: null, pending: null,
        aborted: false, abortDelivered: false, abort: null};
      iterators.set(this, state);
      owner.iterators.add(this);
      if (statement) statement.iterators.add(this);
      state.abort = reason => {
        state.aborted = true;
        if (owner.transaction && owner.queue.failed === undefined) owner.queue.failed = {error: reason};
        closeIterator(state).catch(() => {});
      };
      if (signal) {
        if (signals.aborted(signal)) state.abort(signals.reason(signal));
        else signals.add(signal, state.abort);
      }
    }
    get closed() { return iteratorState(this).ended; }
    next() {
      const state = iteratorState(this);
      return iteratorTask(state, () => nextRow(state));
    }
    return(value = undefined) {
      return closeIterator(iteratorState(this)).then(() => ({value, done: true}));
    }
    close() { return closeIterator(iteratorState(this)); }
    [Symbol.asyncIterator]() { iteratorState(this); return this; }
    [Symbol.asyncDispose]() { return closeIterator(iteratorState(this)); }
  }
  function iterate(target, sql, params, opts) {
    const owner = ownerState(target), text = sqlText(sql), values = parameters(params);
    return new RowIterator(owner, null, text, values, opts);
  }
  function iterateStatement(target, params, opts) {
    const statement = statements.get(target);
    if (!statement) invalid("invalid statement receiver");
    if (statement.closed) throw closed("the statement is closed");
    const owner = ownerState(statement.owner.object), values = parameters(params);
    return new RowIterator(owner, statement, "", values, opts);
  }

  class QueryMethods {
    async exec(sql, opts) {
      const state = ownerState(this), text = sqlText(sql), {signal} = options(opts);
      return schedule(state, async op => { await request(state, "exec", text, EMPTY, 0, op.register); }, signal);
    }
    run(sql, params, opts) { return execute(this, "run", sql, params, opts); }
    get(sql, params, opts) { return execute(this, "get", sql, params, opts); }
    all(sql, params, opts) { return execute(this, "all", sql, params, opts); }
    iterate(sql, params, opts) { return iterate(this, sql, params, opts); }
    prepare(sql, opts) { return prepare(this, sql, opts); }
  }

  class Statement {
    constructor(owner, id, sql) {
      native.brand(this);
      statements.set(this, {owner, id, sql, closed: false, finalized: false, closing: null, iterators: new Set()});
    }
    get sql() { return statements.get(this).sql; }
    get closed() {
      const state = statements.get(this);
      return state.closed || (state.owner.transaction ? !state.owner.active : state.owner.closed);
    }
    run(params, opts) { return executeStatement(this, "run", params, opts); }
    get(params, opts) { return executeStatement(this, "get", params, opts); }
    all(params, opts) { return executeStatement(this, "all", params, opts); }
    iterate(params, opts) { return iterateStatement(this, params, opts); }
    close() {
      const state = statements.get(this);
      if (!state) return Promise.reject(new TypeError("invalid statement receiver"));
      if (state.closing) return state.closing;
      state.closed = true;
      const owner = state.owner;
      const closing = Array.from(state.iterators, iterator => closeIterator(iteratorState(iterator)));
      if (owner.transaction ? !owner.active : owner.closed) return state.closing = Promise.all(closing).then(() => {});
      const finalized = enqueue(owner.queue, () => request(owner, "finalize", "", EMPTY, state.id), undefined, true).then(() => { state.finalized = true; });
      state.closing = Promise.all([...closing, finalized]).then(() => {});
      return state.closing;
    }
    [Symbol.asyncDispose]() { return this.close(); }
  }

  async function finalizeTransaction(state) {
    for (const iterator of Array.from(state.iterators)) await closeIterator(iteratorState(iterator));
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
      const state = {object: this, id, path, readOnly, closed: false, closing: null, queue: {tail: Promise.resolve()}, transaction: false, iterators: new Set()};
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
        const state = {object: transaction, db: database, transaction: true, active: true, controller, prepared: new Set(), iterators: new Set(), queue: {tail: Promise.resolve(), recordFailure: true}};
        states.set(transaction, state);
        const stop = reason => controller.abort(reason);
        if (signal) signals.add(signal, stop);
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
          if (state.queue.failed !== undefined) throw state.queue.failed.error;
          op.check();
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
          if (signal) signals.remove(signal, stop);
        }
      }, signal);
    }
    close() {
      const state = dbState(this);
      if (state.closing) return state.closing;
      state.closed = true;
      const closing = Array.from(state.iterators, iterator => closeIterator(iteratorState(iterator)));
      const databaseClosed = enqueue(state.queue, () => request(state, "close"), undefined);
      state.closing = Promise.all([...closing, databaseClosed]).then(() => {});
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
      if (signal && signals.aborted(signal)) throw signals.reason(signal);
      throw error;
    } finally { operation.cleanup(); }
  }

  return {open};
})
