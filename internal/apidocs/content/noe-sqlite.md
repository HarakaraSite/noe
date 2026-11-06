# noe:sqlite

### API information

Public exports: `open`.

Reference: `noe:sqlite`. Owner: noestdlib. Named export only.

Type declarations: `stdlib/types/noestdlib.d.ts` in the matching NOE source
distribution. This is a NOE-specific Promise API, separate from Node's
synchronous `node:sqlite`; that Node module is not installed.

Availability: NOE CLI, Workers, and managed Go stdlib profiles. A Loop and the
base Web APIs are required. Memory databases need no filesystem authority;
file databases require `Profile.Modules.FS` and share its Root, Cwd, and
ReadOnly policy. SQLite does not grant filesystem access independently.

Public-definition source: internal/stdlib/sqlite.go, internal/stdlib/sqlite.js,
internal/stdlib/sqlite_host.go, internal/stdlib/sqlite_vfs.go. Reading this
reference evaluates no target module and makes no provider request.

All I/O and SQL execution run away from the runtime owner. Operations on one
connection execute in submission order, so timers and network callbacks can
continue while SQLite computes or waits for a lock. Separate connections can
operate concurrently, subject to SQLite locking. Handles cannot be cloned or
transferred to another Worker. Open another connection there instead.

```js
import {open} from "noe:sqlite";

await using db = await open("notes.db");
await db.exec("CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, text TEXT)");
const inserted = await db.run("INSERT INTO notes(text) VALUES(?)", ["hello"]);
console.log(await db.get("SELECT * FROM notes WHERE id=?", [inserted.lastInsertRowid]));
```

SQL data values use bound parameters. Positional values are an array; named
values are an object, with exact `:name`, `@name`, `$name` keys or an unambiguous
bare name. The supplied parameters must match the statement. Missing, unknown,
and ambiguous names reject. Each call clears earlier bindings and snapshots
binary data at submission time.

Input values are null/undefined, booleans, finite numbers, signed 64-bit
bigints, strings, and binary buffers/views. Booleans become integer 1/0;
undefined becomes SQL NULL. Use bigint for integral values outside JavaScript's
safe number range. INTEGER results are numbers when safe and bigints otherwise;
REAL, TEXT, BLOB, and NULL become number, string, Uint8Array, and null. Rows
have a null prototype, so column aliases such as `__proto__` are ordinary keys.

SQL failures reject with a symbolic SQLite `code` and numeric `sqliteCode`.
Argument/handle errors use `ERR_INVALID_ARG`/`ERR_SQLITE_CLOSED`; file authority
failures use the corresponding filesystem or read-only error. No SQL parameter
values are added to diagnostic logs by this module.

## open

`open(path = ":memory:", {readOnly = false, busyTimeout = 0, signal?})`
returns `Promise<Database>`. `:memory:` creates an isolated memory database.
Other paths use the selected filesystem, with relative paths based on its Cwd.
Path is a nonempty string without NUL; it is a filename rather than a SQLite
connection URI. `busyTimeout` is a nonnegative integer in milliseconds, up to
2147483647, and controls waiting for a database lock. Zero returns busy errors
without a configured wait. `readOnly: true` opens an existing database without
allowing data changes; read-only filesystem profiles also prevent file writes.

An AbortSignal can cancel opening. Foreign keys are enabled and double-quoted
string literals are disabled by default. The database exposes read-only
`path`, `readOnly`, and `closed` properties. Path preserves the supplied
filename; closed becomes true as soon as close rejects further submissions.

WAL mode is supported on writable filesystems, including connections opened
with readOnly true there. An FS.ReadOnly profile refuses a WAL-format database
with ERR_READ_ONLY: the driver's shared-memory index can otherwise write a
sidecar even for a read-only connection. Use rollback-journal databases for
that profile. The implementation does not silently assume an actively changing
database is immutable.

## Database.exec

`db.exec(sql, {signal?}) -> Promise<void>` executes SQL without bound values.
Multiple statements are allowed. For data values, use run/get/all or a prepared
statement. SQL must be a string without NUL. Earlier successful statements in
a multi-statement exec are not automatically rolled back; use transaction
when the entire operation must be atomic.

## Database.run

`db.run(sql, params = [], {signal?}) -> Promise<{changes, lastInsertRowid}>`
executes one statement. Both result integers use the safe-number/bigint rule.
Use this for inserts, updates, and deletes. Additional SQL statements reject;
use exec for an unbound multi-statement script.

## Database.get

`db.get(sql, params = [], {signal?}) -> Promise<Row | undefined>` returns the
first matching row, or undefined if none. The statement is reset before the
connection accepts its next operation.

## Database.all

`db.all(sql, params = [], {signal?}) -> Promise<Row[]>` collects all matching
rows. Limit or page large queries in SQL when collecting everything is not
appropriate. Result shape and value conversions are the same as get.

## Database.prepare

`db.prepare(sql, {signal?}) -> Promise<Statement>` prepares one statement for
reuse. The statement has read-only sql and closed properties. Its run/get/all
methods accept `(params = [], {signal?})`, return the same shapes as their
database counterparts, and use the same connection queue. They are all
asynchronous. `statement.close()` finalizes it and rejects new submissions;
it is idempotent and also implements Symbol.asyncDispose.

## Database.transaction

`db.transaction(async tx => result, {mode = "deferred", signal?})`
returns `Promise<result>`. Modes are deferred, immediate, and exclusive.
The callback receives exec/run/get/all/prepare methods for that transaction.
Use these methods inside the callback. Calling methods on the outer database
or its existing statements would wait behind the transaction's reservation.

```js
const count = await db.transaction(async tx => {
  await tx.run("INSERT INTO notes(text) VALUES(?)", ["first"]);
  await tx.run("INSERT INTO notes(text) VALUES(?)", ["second"]);
  return (await tx.get("SELECT COUNT(*) AS count FROM notes")).count;
});
```

The reservation covers the complete callback, including its awaits. Other
database operations wait until commit or rollback completes. A callback throw,
operation failure, or cancellation rolls the transaction back. Any failed
transaction SQL operation causes rollback even if its rejection is caught in
the callback; subsequent transaction SQL is skipped/rejected. The transaction
object and statements created through it become
invalid when its callback ends; scoped statements are finalized automatically.
Nested transactions and manual BEGIN/COMMIT/ROLLBACK/SAVEPOINT SQL are not
part of this API. ATTACH, VACUUM (including INTO), filesystem-directory PRAGMAs, extension
loading, and JavaScript SQL callbacks are outside this initial slice.

## Database.close

`db.close() -> Promise<void>` immediately stops new submissions, drains already
accepted operations, finalizes remaining statements, and closes the connection.
It is idempotent and implements Symbol.asyncDispose, so `await using` is the
normal scoped form. Prepared statements support the same disposal mechanism.

The signal option rejects with the signal's original reason. Queued cancelled
operations skip execution; running operations are interrupted. The connection
waits for actual native completion before starting later work. Cancellation
cannot undo a write that SQLite has already committed. Transactions roll back
before releasing the connection to later requests.

Execution shutdown and Worker termination cancel native work, join it, and
close databases even when the caller omitted close. They do not close the
borrowed VM, Loop, or filesystem backend. An idle database does not keep the
event loop alive by itself.
