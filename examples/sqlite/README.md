# SQLite notes

[Examples](../README.md) · [API](../../internal/apidocs/content/noe-sqlite.md)

From the repository root, use a binary built from the matching source:

```sh
./noe run examples/sqlite/notes.js
```

The default is an in-memory database. The example creates a table, inserts two
rows in one reserved transaction, reads them, and closes the database through
`await using`. It needs no external service or provider credentials.

```json
{"inserted":[1,2],"notes":[{"id":1,"text":"hello"},{"id":2,"text":"日本語"}]}
```

For persistence, supply a filename in an existing directory:

```sh
SQLITE_DEMO_DIR=$(mktemp -d)
./noe run examples/sqlite/notes.js "$SQLITE_DEMO_DIR/notes.db"
./noe run examples/sqlite/notes.js "$SQLITE_DEMO_DIR/notes.db"
```

Each run appends two rows, so the second run returns four notes. File databases
use the selected filesystem's Cwd, Root, and ReadOnly policy. All query
methods return promises, and parameters are bound rather than interpolated.
Inside a transaction callback use its `tx` methods so the database reservation
can finish before unrelated work resumes. Transaction-local statements close
automatically when the callback ends. Node's synchronous `node:sqlite` module
is not exposed by NOE.
