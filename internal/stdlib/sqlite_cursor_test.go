package stdlib

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

func newSQLiteCursorTestDB(t *testing.T) *sqliteDB {
	t.Helper()
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	host := &sqliteHost{resources: make(map[int]*sqliteDB)}
	db := &sqliteDB{
		host: host, conn: conn, stmts: make(map[int]*sqlite3.Stmt),
		cursors: make(map[int]*sqliteCursor), activeStmtCursor: make(map[int]int),
	}
	if err := conn.SetAuthorizer(db.authorize); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.close(); err != nil {
			t.Errorf("close test database: %v", err)
		}
	})
	return db
}

func TestSQLiteCursorReadsBoundedBatchesAndReleasesAtEOF(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	ctx := context.Background()
	cursor, err := db.cursorOpen(ctx, 0, `WITH RECURSIVE items(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM items WHERE value < 5
	) SELECT value FROM items`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}

	first, err := db.cursorRead(ctx, cursor.cursorID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if first.done || len(first.rows) != 2 || first.rows[1].values[0].text != "2" {
		t.Fatalf("first batch = %#v, want rows 1-2 and done=false", first)
	}
	db.mu.Lock()
	active := db.cursors[cursor.cursorID]
	db.mu.Unlock()
	if active == nil || active.stmt.ColumnInt64(0) != 2 {
		t.Fatalf("full batch advanced beyond its last returned row: cursor=%v", active)
	}

	second, err := db.cursorRead(ctx, cursor.cursorID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if second.done || len(second.rows) != 2 || second.rows[0].values[0].text != "3" || second.rows[1].values[0].text != "4" {
		t.Fatalf("second batch = %#v, want rows 3-4 and done=false", second)
	}
	last, err := db.cursorRead(ctx, cursor.cursorID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !last.done || len(last.rows) != 1 || last.rows[0].values[0].text != "5" {
		t.Fatalf("last batch = %#v, want row 5 and done=true", last)
	}
	if len(db.cursors) != 0 || len(db.activeStmtCursor) != 0 {
		t.Fatalf("EOF retained cursor ownership: cursors=%v active=%v", db.cursors, db.activeStmtCursor)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatalf("close after EOF: %v", err)
	}
	if _, err := db.cursorRead(ctx, cursor.cursorID, 1); err == nil {
		t.Fatal("read after EOF unexpectedly succeeded")
	}
	large, err := db.cursorOpen(ctx, 0, "SELECT 6", sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	largeBatch, err := db.cursorRead(ctx, large.cursorID, sqliteMaxSafeInteger)
	if err != nil || !largeBatch.done || len(largeBatch.rows) != 1 {
		t.Fatalf("maximum safe batch = %#v, err=%v", largeBatch, err)
	}
}

func TestSQLiteCursorSupportsSingleStatementReturning(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	if err := db.conn.Exec(`CREATE TABLE returned(value INTEGER)`); err != nil {
		t.Fatal(err)
	}
	cursor, err := db.cursorOpen(context.Background(), 0,
		`INSERT INTO returned(value) VALUES(8), (9) RETURNING value`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := db.cursorRead(context.Background(), cursor.cursorID, 1)
	if err != nil || first.done || len(first.rows) != 1 || first.rows[0].values[0].text != "8" {
		t.Fatalf("first RETURNING batch = %#v, err=%v", first, err)
	}
	second, err := db.cursorRead(context.Background(), cursor.cursorID, 1)
	if err != nil || second.done || len(second.rows) != 1 || second.rows[0].values[0].text != "9" {
		t.Fatalf("second RETURNING batch = %#v, err=%v", second, err)
	}
	last, err := db.cursorRead(context.Background(), cursor.cursorID, 1)
	if err != nil || !last.done || len(last.rows) != 0 {
		t.Fatalf("RETURNING EOF = %#v, err=%v", last, err)
	}
	count, err := db.request(context.Background(), 0, "get", `SELECT count(*) FROM returned`, sqliteParamSet{})
	if err != nil || count.(sqliteRow).values[0].text != "2" {
		t.Fatalf("RETURNING persisted %v, err=%v", count, err)
	}
}

func TestSQLiteCursorSnapshotsBlobAndReusesCellConversion(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	input := rt.NewBytes([]byte{0, 0xff, 0x7f})
	cell := rt.NewObject()
	if err := cell.Set("kind", "blob"); err != nil {
		t.Fatal(err)
	}
	if err := cell.Set("bytes", input); err != nil {
		t.Fatal(err)
	}
	parsed, err := sqliteReadParamCell(cell)
	if err != nil {
		t.Fatal(err)
	}
	inputBytes, ok := input.Bytes()
	if !ok {
		t.Fatal("NewBytes did not return a byte view")
	}
	inputBytes[0] = 0x55
	if !reflect.DeepEqual(parsed.bytes, []byte{0, 0xff, 0x7f}) {
		t.Fatalf("input BLOB was not snapshotted: %v", parsed.bytes)
	}

	db := newSQLiteCursorTestDB(t)
	stmt, err := sqlitePrepareSingle(db.conn, `SELECT X'00FF7F' UNION ALL SELECT X'112233'`)
	if err != nil {
		t.Fatal(err)
	}
	defer stmt.Close()
	if !stmt.Step() {
		t.Fatalf("first BLOB row: %v", stmt.Err())
	}
	first := sqliteReadRow(stmt)
	if !stmt.Step() {
		t.Fatalf("second BLOB row: %v", stmt.Err())
	}
	if !reflect.DeepEqual(first.values[0].bytes, []byte{0, 0xff, 0x7f}) {
		t.Fatalf("result BLOB changed when statement advanced: %v", first.values[0].bytes)
	}

	integers, err := sqlitePrepareSingle(db.conn, `SELECT 42, 9007199254740993, -9223372036854775808, 1.25`)
	if err != nil {
		t.Fatal(err)
	}
	defer integers.Close()
	if !integers.Step() {
		t.Fatalf("numeric row: %v", integers.Err())
	}
	row := sqliteReadRow(integers)
	if got := []string{row.values[0].text, row.values[1].text, row.values[2].text}; !reflect.DeepEqual(got, []string{"42", "9007199254740993", "-9223372036854775808"}) {
		t.Fatalf("integer converter output = %v", got)
	}
	if row.values[3].kind != "number" || row.values[3].value != 1.25 {
		t.Fatalf("number converter output = %#v", row.values[3])
	}

	host := &sqliteHost{rt: rt}
	encoded, err := host.value(sqliteCursorReadResult{rows: sqliteRows{row}, done: true})
	if err != nil {
		t.Fatal(err)
	}
	done, err := encoded.Get("done")
	if err != nil || !done.Bool() {
		t.Fatalf("cursor result done=%v, err=%v", done, err)
	}
	rows, err := encoded.Get("rows")
	if err != nil || rows.Len() != 1 {
		t.Fatalf("cursor result rows=%v, err=%v", rows, err)
	}
	encodedRow, err := rows.Index(0)
	if err != nil {
		t.Fatal(err)
	}
	values, err := encodedRow.Get("values")
	if err != nil {
		t.Fatal(err)
	}
	largeInteger, err := values.Index(1)
	if err != nil {
		t.Fatal(err)
	}
	text, err := largeInteger.Get("text")
	if err != nil || text.String() != "9007199254740993" {
		t.Fatalf("encoded large integer=%q, err=%v", text.String(), err)
	}
}

func TestSQLiteCursorEarlyCloseFinalizesOwnedStatement(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	ctx := context.Background()
	cursor, err := db.cursorOpen(ctx, 0, `WITH RECURSIVE items(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM items
	) SELECT value FROM items`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.cursorRead(ctx, cursor.cursorID, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatal(err)
	}
	if len(db.cursors) != 0 {
		t.Fatalf("early close retained cursor: %v", db.cursors)
	}
	if err := db.conn.Close(); err != nil {
		t.Fatalf("owned statement remained unfinalized after cursor close: %v", err)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatalf("repeated close: %v", err)
	}
}

func TestSQLiteCursorBorrowsStatementBusyThenReusable(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	stmt, err := sqlitePrepareSingle(db.conn, `SELECT value FROM (SELECT 1 AS value UNION ALL SELECT 2)`)
	if err != nil {
		t.Fatal(err)
	}
	db.stmts[1] = stmt
	db.nextStmt = 1
	ctx := context.Background()
	cursor, err := db.cursorOpen(ctx, 1, "", sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.request(ctx, 1, "get", "", sqliteParamSet{}); err == nil {
		t.Fatal("prepared statement operation succeeded while its cursor was active")
	} else {
		var hostErr *sqliteHostError
		if !errors.As(err, &hostErr) || hostErr.code != "ERR_SQLITE_BUSY" {
			t.Fatalf("busy error = %T %v, want ERR_SQLITE_BUSY", err, err)
		}
	}
	if _, err := db.cursorOpen(ctx, 1, "", sqliteParamSet{}); err == nil {
		t.Fatal("second cursor unexpectedly borrowed an active statement")
	}
	other, err := db.request(ctx, 0, "get", "SELECT 99", sqliteParamSet{})
	if err != nil || other.(sqliteRow).values[0].text != "99" {
		t.Fatalf("unrelated SQL during cursor read = %#v, err=%v", other, err)
	}
	batch, err := db.cursorRead(ctx, cursor.cursorID, 1)
	if err != nil || batch.done || batch.rows[0].values[0].text != "1" {
		t.Fatalf("borrowed cursor batch = %#v, err=%v", batch, err)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatal(err)
	}
	value, err := db.request(ctx, 1, "get", "", sqliteParamSet{})
	if err != nil || value.(sqliteRow).values[0].text != "1" {
		t.Fatalf("prepared statement reuse after cursor release = %#v, err=%v", value, err)
	}
}

func TestSQLiteCursorOpenResetsPreviousStatementError(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	stmt, err := sqlitePrepareSingle(db.conn, `SELECT abs(?)`)
	if err != nil {
		t.Fatal(err)
	}
	db.stmts[1] = stmt
	if err := stmt.BindInt64(1, -1<<63); err != nil {
		t.Fatal(err)
	}
	if stmt.Step() || stmt.Err() == nil {
		t.Fatalf("abs(INT64_MIN) did not leave a statement error: %v", stmt.Err())
	}

	params := sqliteParamSet{positional: []sqliteCell{{kind: "integer", text: "7"}}}
	cursor, err := db.cursorOpen(context.Background(), 1, "", params)
	if err != nil {
		t.Fatalf("cursor open after prior statement error: %v", err)
	}
	batch, err := db.cursorRead(context.Background(), cursor.cursorID, 2)
	if err != nil || !batch.done || len(batch.rows) != 1 || batch.rows[0].values[0].text != "7" {
		t.Fatalf("cursor result after prior statement error = %#v, err=%v", batch, err)
	}
}

func TestSQLiteCursorFinalizationAndDatabaseCloseReleaseOwnership(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCursorTestDB(t)
	stmt, err := sqlitePrepareSingle(db.conn, `SELECT 1 UNION ALL SELECT 2`)
	if err != nil {
		t.Fatal(err)
	}
	db.stmts[1] = stmt
	cursor, err := db.cursorOpen(ctx, 1, "", sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.request(ctx, 1, "finalize", "", sqliteParamSet{}); err != nil {
		t.Fatal(err)
	}
	if len(db.cursors) != 0 || len(db.activeStmtCursor) != 0 || len(db.stmts) != 0 {
		t.Fatalf("statement finalization retained cursor state: cursors=%v active=%v statements=%v", db.cursors, db.activeStmtCursor, db.stmts)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatalf("cursor close after statement finalization: %v", err)
	}

	cursor, err = db.cursorOpen(ctx, 0, `SELECT 1 UNION ALL SELECT 2`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.cursorRead(ctx, cursor.cursorID, 1); err != nil {
		t.Fatal(err)
	}
	if err := db.close(); err != nil {
		t.Fatal(err)
	}
	if len(db.cursors) != 0 || db.conn != nil || !db.closed {
		t.Fatalf("database close retained cursor resources: cursors=%v conn=%v closed=%v", db.cursors, db.conn, db.closed)
	}
	if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
		t.Fatalf("cursor close after database close: %v", err)
	}
}

func TestSQLiteCursorHostShutdownFinalizesOpenCursors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	host := &sqliteHost{ctx: ctx, cancel: cancel, resources: make(map[int]*sqliteDB), closeDone: make(chan struct{})}
	db := newSQLiteCursorTestDB(t)
	db.host = host
	db.id = 1
	host.resources[db.id] = db
	cursor, err := db.cursorOpen(context.Background(), 0, `SELECT 1 UNION ALL SELECT 2`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	host.Close()
	if len(db.cursors) != 0 || !db.closed {
		t.Fatalf("host shutdown retained cursor resources: cursors=%v closed=%v", db.cursors, db.closed)
	}
	if err := db.cursorClose(context.Background(), cursor.cursorID); err != nil {
		t.Fatalf("cursor close after host shutdown: %v", err)
	}
}

func TestSQLiteCursorCancellationWhileSteppingAllowsConnectionReuse(t *testing.T) {
	db := newSQLiteCursorTestDB(t)
	cursor, err := db.cursorOpen(context.Background(), 0, `WITH RECURSIVE n(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM n
	) SELECT value FROM n`, sqliteParamSet{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := db.cursorRead(ctx, cursor.cursorID, sqliteMaxSafeInteger)
		result <- err
	}()
	<-started
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("cursor read succeeded after cancellation")
		}
		var hostErr *sqliteHostError
		if !errors.As(err, &hostErr) || hostErr.code != "ABORT_ERR" {
			t.Fatalf("cancelled cursor error = %T %v, want ABORT_ERR", err, err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled cursor read did not stop stepping")
	}
	if len(db.cursors) != 0 {
		t.Fatalf("cancelled read retained cursor resources: %v", db.cursors)
	}
	resultValue, err := db.request(context.Background(), 0, "get", "SELECT 7", sqliteParamSet{})
	if err != nil || resultValue.(sqliteRow).values[0].text != "7" {
		t.Fatalf("connection reuse after cancellation = %#v, err=%v", resultValue, err)
	}
}

// BenchmarkSQLiteCursorRead compares full result materialization with the time
// to open and receive the first 128 rows. B/op is cumulative Go allocation per
// operation, not retained data or RSS. The cursor batch limits rows, not bytes.
func BenchmarkSQLiteCursorRead(b *testing.B) {
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		b.Fatal(err)
	}
	if err := conn.Exec(`CREATE TABLE rows(value INTEGER); WITH RECURSIVE n(value) AS (
		VALUES(1) UNION ALL SELECT value + 1 FROM n WHERE value < 10000
	) INSERT INTO rows SELECT value FROM n`); err != nil {
		b.Fatal(err)
	}
	db := &sqliteDB{conn: conn, stmts: make(map[int]*sqlite3.Stmt), cursors: make(map[int]*sqliteCursor), activeStmtCursor: make(map[int]int)}
	if err := conn.SetAuthorizer(db.authorize); err != nil {
		b.Fatal(err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			b.Error(err)
		}
	}()
	ctx := context.Background()

	b.Run("all_10000_rows", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			if _, err := db.request(ctx, 0, "all", "SELECT value FROM rows", sqliteParamSet{}); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("cursor_first_128_rows", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for range b.N {
			cursor, err := db.cursorOpen(ctx, 0, "SELECT value FROM rows", sqliteParamSet{})
			if err != nil {
				b.Fatal(err)
			}
			batch, err := db.cursorRead(ctx, cursor.cursorID, 128)
			if err != nil {
				b.Fatal(err)
			}
			if batch.done || len(batch.rows) != 128 {
				b.Fatalf("first batch has %d rows, done=%v", len(batch.rows), batch.done)
			}
			if err := db.cursorClose(ctx, cursor.cursorID); err != nil {
				b.Fatal(err)
			}
		}
	})
}
