package stdlib

import (
	"context"
	"errors"

	sqlite3 "github.com/ncruces/go-sqlite3"
)

const (
	sqliteCursorInitialCapacity = 128
	sqliteMaxSafeInteger        = int64(1<<53 - 1)
)

type sqliteCursor struct {
	id          int
	stmt        *sqlite3.Stmt
	statementID int
	owned       bool
	columns     []string
}

// cursorOpen creates a native cursor over one lazily prepared or borrowed
// statement. Params have already been snapshotted from JavaScript by the host
// callback before they cross the worker boundary.
func (db *sqliteDB) cursorOpen(ctx context.Context, statementID int, sql string, params sqliteParamSet) (sqliteCursorOpenResult, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.conn == nil {
		return sqliteCursorOpenResult{}, &sqliteHostError{message: "SQLite database is closed", code: "ERR_SQLITE_CLOSED"}
	}
	if err := ctx.Err(); err != nil {
		return sqliteCursorOpenResult{}, sqliteContextError(ctx)
	}
	oldInterrupt := db.conn.SetInterrupt(ctx)
	defer db.conn.SetInterrupt(oldInterrupt)

	var stmt *sqlite3.Stmt
	owned := statementID == 0
	if owned {
		var err error
		stmt, err = sqlitePrepareSingle(db.conn, sql)
		if err != nil {
			return sqliteCursorOpenResult{}, err
		}
	} else {
		stmt = db.stmts[statementID]
		if stmt == nil {
			return sqliteCursorOpenResult{}, &sqliteHostError{message: "prepared statement is closed", code: "ERR_SQLITE_CLOSED"}
		}
		if db.activeStmtCursor[statementID] != 0 {
			return sqliteCursorOpenResult{}, &sqliteHostError{message: "prepared statement is in use by an active cursor", code: "ERR_SQLITE_BUSY"}
		}
	}

	cleanupOnError := func(cause error) (sqliteCursorOpenResult, error) {
		return sqliteCursorOpenResult{}, errors.Join(cause, sqliteCleanupStatement(stmt, owned))
	}
	// Reset can report the previous execution's terminal error even though it
	// has successfully returned the statement to its initial state. Match the
	// existing run/get/all path and report the current clear/bind failures.
	_ = stmt.Reset()
	if err := stmt.ClearBindings(); err != nil {
		return cleanupOnError(err)
	}
	if err := sqliteBindParams(stmt, params); err != nil {
		return cleanupOnError(err)
	}
	if err := ctx.Err(); err != nil {
		return cleanupOnError(sqliteContextError(ctx))
	}

	if db.cursors == nil {
		db.cursors = make(map[int]*sqliteCursor)
	}
	if db.activeStmtCursor == nil {
		db.activeStmtCursor = make(map[int]int)
	}
	db.nextCursor++
	cursor := &sqliteCursor{id: db.nextCursor, stmt: stmt, statementID: statementID, owned: owned}
	db.cursors[cursor.id] = cursor
	if !owned {
		db.activeStmtCursor[statementID] = cursor.id
	}
	return sqliteCursorOpenResult{cursorID: cursor.id}, nil
}

// cursorRead snapshots at most batchSize rows. It observes EOF only when a
// Step call returns false; a full batch does not advance the statement again.
func (db *sqliteDB) cursorRead(ctx context.Context, cursorID int, batchSize int64) (sqliteRows, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.conn == nil {
		return sqliteRows{}, &sqliteHostError{message: "SQLite database is closed", code: "ERR_SQLITE_CLOSED"}
	}
	if batchSize <= 0 || batchSize > sqliteMaxSafeInteger {
		return sqliteRows{}, &sqliteHostError{message: "SQLite cursor batch size must be a positive safe integer", code: "ERR_INVALID_ARG"}
	}
	cursor := db.cursors[cursorID]
	if cursor == nil {
		return sqliteRows{}, &sqliteHostError{message: "SQLite cursor is closed", code: "ERR_SQLITE_CLOSED"}
	}
	if err := ctx.Err(); err != nil {
		readErr := sqliteContextError(ctx)
		return sqliteRows{}, errors.Join(readErr, db.releaseCursorLocked(cursorID))
	}
	oldInterrupt := db.conn.SetInterrupt(ctx)
	defer db.conn.SetInterrupt(oldInterrupt)

	capacity := sqliteCursorInitialCapacity
	if batchSize < sqliteCursorInitialCapacity {
		capacity = int(batchSize)
	}
	batch := sqliteRows{rows: make([][]sqliteCell, 0, capacity)}
	for i := int64(0); i < batchSize; i++ {
		if cursor.stmt.Step() {
			if cursor.columns == nil {
				cursor.columns = sqliteColumnNames(cursor.stmt)
				batch.columns = cursor.columns
			}
			batch.rows = append(batch.rows, sqliteReadCells(cursor.stmt, len(cursor.columns)))
			if err := ctx.Err(); err != nil {
				readErr := sqliteContextError(ctx)
				return sqliteRows{}, errors.Join(readErr, db.releaseCursorLocked(cursorID))
			}
			continue
		}

		readErr := cursor.stmt.Err()
		if ctx.Err() != nil {
			readErr = sqliteContextError(ctx)
		}
		cleanupErr := db.releaseCursorLocked(cursorID)
		if readErr != nil {
			return sqliteRows{}, errors.Join(readErr, cleanupErr)
		}
		if cleanupErr != nil {
			return sqliteRows{}, cleanupErr
		}
		batch.done = true
		break
	}
	return batch, nil
}

// cursorClose releases a cursor explicitly. Repeated closes are harmless, and
// a cursor owned by a closed database has already been released by closeLocked.
func (db *sqliteDB) cursorClose(ctx context.Context, cursorID int) error {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.conn == nil || db.cursors[cursorID] == nil {
		return nil
	}
	oldInterrupt := db.conn.SetInterrupt(ctx)
	defer db.conn.SetInterrupt(oldInterrupt)
	return db.releaseCursorLocked(cursorID)
}

// releaseCursorLocked removes native ownership before cleaning the statement,
// so even a finalization error cannot leave it permanently marked busy.
func (db *sqliteDB) releaseCursorLocked(cursorID int) error {
	cursor := db.cursors[cursorID]
	if cursor == nil {
		return nil
	}
	delete(db.cursors, cursorID)
	if !cursor.owned && db.activeStmtCursor[cursor.statementID] == cursorID {
		delete(db.activeStmtCursor, cursor.statementID)
	}
	return sqliteCleanupStatement(cursor.stmt, cursor.owned)
}

func sqliteCleanupStatement(stmt *sqlite3.Stmt, owned bool) error {
	if stmt == nil {
		return nil
	}
	resetErr := stmt.Reset()
	clearErr := stmt.ClearBindings()
	var closeErr error
	if owned {
		closeErr = stmt.Close()
	}
	return errors.Join(resetErr, clearErr, closeErr)
}
