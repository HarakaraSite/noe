package stdlib

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
	sqlite3 "github.com/ncruces/go-sqlite3"
)

type sqliteHost struct {
	rt          *quickjs.Runtime
	loop        *Loop
	backend     *filesystem.Backend
	ctx         context.Context
	cancel      context.CancelFunc
	stopRuntime func() bool
	mu          sync.Mutex
	closed      bool
	resources   map[int]*sqliteDB
	nextDB      int
	wg          sync.WaitGroup
	closeDone   chan struct{}
	vfsName     string
	vfs         *sqliteSandboxVFS
}

type sqliteDB struct {
	host     *sqliteHost
	id       int
	mu       sync.Mutex
	closed   bool
	conn     *sqlite3.Conn
	stmts    map[int]*sqlite3.Stmt
	nextStmt int
	allowTx  bool
	readOnly bool
}

type sqliteCell struct {
	kind  string
	text  string
	value float64
	bytes []byte
}

type sqliteParamSet struct {
	positional []sqliteCell
	named      []sqliteNamedParam
}

type sqliteNamedParam struct {
	name  string
	value sqliteCell
}

type sqliteRow struct {
	columns []string
	values  []sqliteCell
}

type sqliteRows []sqliteRow

type sqliteRunResult struct {
	changes int64
	rowID   int64
}

type sqliteOpenResult struct{ id int }

type sqlitePrepareResult struct{ id int }

type sqliteHostError struct {
	message string
	code    string
	cause   error
}

func (e *sqliteHostError) Error() string {
	if e.message != "" {
		return e.message
	}
	if e.cause != nil {
		return e.cause.Error()
	}
	return "SQLite operation failed"
}

func (e *sqliteHostError) Unwrap() error { return e.cause }

var sqliteVFSSequence atomic.Uint64

func newSQLiteHost(rt *quickjs.Runtime, loop *Loop, cfg *FS) (quickjs.Value, error) {
	if rt == nil || loop == nil || !loop.Owns(rt) {
		return quickjs.Value{}, errors.New("SQLite requires a loop owned by its runtime")
	}
	var backend *filesystem.Backend
	if cfg != nil {
		backend = cfg.Backend
		if backend == nil {
			var err error
			backend, err = filesystem.New(filesystem.Config{Root: cfg.Root, Cwd: cfg.Cwd, ReadOnly: cfg.ReadOnly})
			if err != nil {
				return quickjs.Value{}, err
			}
		}
	}
	ctx, cancel := context.WithCancel(loopContext(loop))
	h := &sqliteHost{
		rt: rt, loop: loop, backend: backend, ctx: ctx, cancel: cancel,
		resources: make(map[int]*sqliteDB), closeDone: make(chan struct{}),
	}
	h.vfsName = fmt.Sprintf("noe_sqlite_%x", sqliteVFSSequence.Add(1))
	h.vfs = newSQLiteSandboxVFS(backend)
	registerSQLiteVFS(h.vfsName, h.vfs)
	h.stopRuntime = context.AfterFunc(rt.Context(), cancel)
	rt.OnClose(h.Close)
	if scope := loop.executionScope(); scope != nil {
		if err := scope.AddCleanup(h.Close); err != nil {
			h.Close()
			return quickjs.Value{}, err
		}
	}

	native := rt.NewObject()
	if err := errors.Join(
		native.Set("open", h.open),
		native.Set("request", h.request),
	); err != nil {
		h.Close()
		return quickjs.Value{}, err
	}
	return native, nil
}

func (h *sqliteHost) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		<-h.closeDone
		return
	}
	h.closed = true
	h.cancel()
	if h.stopRuntime != nil {
		h.stopRuntime()
	}
	dbs := make([]*sqliteDB, 0, len(h.resources))
	for _, db := range h.resources {
		dbs = append(dbs, db)
	}
	h.mu.Unlock()

	h.wg.Wait()
	for _, db := range dbs {
		_ = db.close()
	}
	unregisterSQLiteVFS(h.vfsName)
	close(h.closeDone)
}

func (h *sqliteHost) admitWorker() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return false
	}
	h.wg.Add(1)
	return true
}

func (h *sqliteHost) ownDB(db *sqliteDB) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return false
	}
	h.nextDB++
	db.id = h.nextDB
	h.resources[db.id] = db
	return true
}

func (h *sqliteHost) getDB(id int) *sqliteDB {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.resources[id]
}

func (h *sqliteHost) releaseDB(db *sqliteDB) {
	h.mu.Lock()
	if h.resources[db.id] == db {
		delete(h.resources, db.id)
	}
	h.mu.Unlock()
}

func (h *sqliteHost) open(path string, readOnly bool, timeout int, register quickjs.Value) *quickjs.Promise {
	if path == "" || strings.ContainsRune(path, '\x00') {
		return h.rejected(&sqliteHostError{message: "path must be a nonempty string without NUL", code: "ERR_INVALID_ARG"})
	}
	if timeout < 0 || int64(timeout) > int64(math.MaxInt64)/int64(time.Millisecond) {
		return h.rejected(&sqliteHostError{message: "busy timeout must be a nonnegative millisecond value", code: "ERR_INVALID_ARG"})
	}
	isMemory := path == ":memory:"
	if !isMemory && h.backend == nil {
		return h.rejected(&sqliteHostError{message: "SQLite file access requires filesystem capability", code: "ERR_FS_DISABLED"})
	}
	if !isMemory && !readOnly && h.backend != nil {
		if err := h.backend.CheckWritable(); err != nil {
			return h.rejected(&sqliteHostError{message: err.Error(), code: "ERR_READ_ONLY", cause: err})
		}
	}
	return h.async(register, func(ctx context.Context) (any, error) {
		conn, err := h.openConnection(ctx, path, readOnly, timeout)
		if err != nil {
			return nil, err
		}
		db := &sqliteDB{host: h, conn: conn, stmts: make(map[int]*sqlite3.Stmt), readOnly: readOnly}
		if err := conn.SetAuthorizer(db.authorize); err != nil {
			_ = conn.Close()
			return nil, err
		}
		if !h.ownDB(db) {
			_ = conn.Close()
			if err := ctx.Err(); err != nil {
				return nil, sqliteContextError(ctx)
			}
			return nil, &sqliteHostError{message: "SQLite host is closed", code: "ERR_SQLITE_CLOSED"}
		}
		return sqliteOpenResult{id: db.id}, nil
	})
}

func (h *sqliteHost) request(dbID, statementID int, operation, sql string, params, register quickjs.Value) *quickjs.Promise {
	if dbID <= 0 || statementID < 0 {
		return h.rejected(&sqliteHostError{message: "invalid SQLite resource id", code: "ERR_INVALID_ARG"})
	}
	db := h.getDB(dbID)
	if db == nil {
		return h.rejected(&sqliteHostError{message: "SQLite database is closed", code: "ERR_SQLITE_CLOSED"})
	}
	var args sqliteParamSet
	switch operation {
	case "exec", "run", "get", "all", "prepare":
		if operation == "exec" || operation == "prepare" {
			if statementID != 0 {
				return h.rejected(&sqliteHostError{message: "invalid statement id for SQLite operation", code: "ERR_INVALID_ARG"})
			}
		}
		if operation == "run" || operation == "get" || operation == "all" {
			var err error
			args, err = sqliteParams(params)
			if err != nil {
				return h.rejected(err)
			}
		}
	case "finalize":
		if statementID == 0 {
			return h.rejected(&sqliteHostError{message: "invalid statement id for finalization", code: "ERR_INVALID_ARG"})
		}
	default:
		if statementID != 0 {
			return h.rejected(&sqliteHostError{message: "invalid statement id for SQLite operation", code: "ERR_INVALID_ARG"})
		}
	}
	return h.async(register, func(ctx context.Context) (any, error) {
		return db.request(ctx, statementID, operation, sql, args)
	})
}

func (h *sqliteHost) async(register quickjs.Value, work func(context.Context) (any, error)) *quickjs.Promise {
	p := h.rt.NewPromise()
	ctx, cancel := context.WithCancel(h.ctx)
	if register.IsFunction() {
		if _, err := register.Call(cancel); err != nil {
			cancel()
			h.reject(p, err)
			return p
		}
	}
	if err := h.ctx.Err(); err != nil {
		cancel()
		h.reject(p, &sqliteHostError{message: "SQLite host is closed", code: "ERR_SQLITE_CLOSED", cause: err})
		return p
	}
	if !h.admitWorker() {
		cancel()
		h.reject(p, &sqliteHostError{message: "SQLite host is closed", code: "ERR_SQLITE_CLOSED"})
		return p
	}
	h.loop.Begin()
	if err := h.loop.Go(func(context.Context) {
		defer h.loop.Done()
		defer h.wg.Done()
		value, err := work(ctx)
		if err != nil && ctx.Err() != nil {
			err = sqliteContextError(ctx)
		}
		cancel()
		h.loop.postExecution(func() {
			if err != nil {
				h.reject(p, err)
				return
			}
			result, convErr := h.value(value)
			if convErr != nil {
				h.reject(p, convErr)
				return
			}
			if err := p.Resolve(result); err != nil {
				h.reject(p, err)
			}
		})
	}); err != nil {
		h.loop.Done()
		h.wg.Done()
		cancel()
		h.reject(p, &sqliteHostError{message: err.Error(), code: "ERR_SQLITE_CLOSED", cause: err})
	}
	return p
}

func (h *sqliteHost) rejected(err error) *quickjs.Promise {
	p := h.rt.NewPromise()
	h.reject(p, err)
	return p
}

func (h *sqliteHost) openConnection(ctx context.Context, path string, readOnly bool, timeout int) (*sqlite3.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, sqliteContextError(ctx)
	}
	if path != ":memory:" && readOnly && h.backend != nil && errors.Is(h.backend.CheckWritable(), filesystem.ErrReadOnly) {
		if h.readOnlyWALDatabase(ctx, path) {
			return nil, &sqliteHostError{
				message: "WAL-mode SQLite databases cannot be opened with a read-only filesystem backend",
				code:    "ERR_READ_ONLY",
				cause:   sqlite3.READONLY,
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, sqliteContextError(ctx)
		}
	}
	uri := sqliteDatabaseURI(path, h.vfsName)
	flags := sqlite3.OPEN_URI | sqlite3.OPEN_FULLMUTEX
	if readOnly {
		flags |= sqlite3.OPEN_READONLY
	} else {
		flags |= sqlite3.OPEN_READWRITE | sqlite3.OPEN_CREATE
	}
	conn, err := sqlite3.OpenFlags(uri, flags)
	if err != nil {
		if ctx.Err() != nil {
			return nil, sqliteContextError(ctx)
		}
		return nil, err
	}
	closeWith := func(err error) (*sqlite3.Conn, error) {
		return nil, errors.Join(err, conn.Close())
	}
	if err := ctx.Err(); err != nil {
		return closeWith(sqliteContextError(ctx))
	}
	if err := conn.BusyTimeout(time.Duration(timeout) * time.Millisecond); err != nil {
		return closeWith(err)
	}
	for _, config := range []struct {
		key sqlite3.DBConfig
		val bool
	}{
		{sqlite3.DBCONFIG_ENABLE_FKEY, true},
		{sqlite3.DBCONFIG_DQS_DML, false},
		{sqlite3.DBCONFIG_DQS_DDL, false},
		{sqlite3.DBCONFIG_DEFENSIVE, true},
		{sqlite3.DBCONFIG_ENABLE_LOAD_EXTENSION, false},
	} {
		if _, err := conn.Config(config.key, config.val); err != nil {
			return closeWith(err)
		}
	}
	return conn, nil
}

func (h *sqliteHost) readOnlyWALDatabase(ctx context.Context, path string) bool {
	if h.backend == nil {
		return false
	}
	file, err := h.backend.OpenFile(ctx, path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	var header [20]byte
	n, err := file.ReadAt(header[:], 0)
	return n == len(header) && err == nil && string(header[:16]) == "SQLite format 3\x00" && header[18] == 2 && header[19] == 2
}

func (db *sqliteDB) authorize(action sqlite3.AuthorizerActionCode, third, fourth, _, _ string) sqlite3.AuthorizerReturnCode {
	switch action {
	case sqlite3.AUTH_ATTACH:
		return sqlite3.AUTH_DENY
	case sqlite3.AUTH_TRANSACTION, sqlite3.AUTH_SAVEPOINT:
		if !db.allowTx {
			return sqlite3.AUTH_DENY
		}
	case sqlite3.AUTH_PRAGMA:
		if strings.EqualFold(third, "temp_store_directory") || strings.EqualFold(third, "data_store_directory") {
			return sqlite3.AUTH_DENY
		}
	case sqlite3.AUTH_FUNCTION:
		if strings.EqualFold(fourth, "load_extension") {
			return sqlite3.AUTH_DENY
		}
	}
	return sqlite3.AUTH_OK
}

func (db *sqliteDB) request(ctx context.Context, statementID int, operation, sql string, params sqliteParamSet) (any, error) {
	db.mu.Lock()
	defer db.mu.Unlock()
	if db.closed || db.conn == nil {
		return nil, &sqliteHostError{message: "SQLite database is closed", code: "ERR_SQLITE_CLOSED"}
	}
	if err := ctx.Err(); err != nil {
		return nil, sqliteContextError(ctx)
	}
	conn := db.conn
	oldInterrupt := conn.SetInterrupt(ctx)
	defer conn.SetInterrupt(oldInterrupt)

	switch operation {
	case "exec":
		if statementID != 0 {
			return nil, &sqliteHostError{message: "exec does not accept a prepared statement id", code: "ERR_INVALID_ARG"}
		}
		if err := conn.Exec(sql); err != nil {
			return nil, err
		}
		return nil, nil
	case "prepare":
		if statementID != 0 {
			return nil, &sqliteHostError{message: "prepare does not accept a statement id", code: "ERR_INVALID_ARG"}
		}
		stmt, err := sqlitePrepareSingle(conn, sql)
		if err != nil {
			return nil, err
		}
		db.nextStmt++
		id := db.nextStmt
		db.stmts[id] = stmt
		return sqlitePrepareResult{id: id}, nil
	case "run", "get", "all":
		stmt, owned, err := db.statement(statementID, sql)
		if err != nil {
			return nil, err
		}
		if owned {
			defer stmt.Close()
		}
		result, err := sqliteRunStatement(stmt, operation, params)
		return result, err
	case "finalize":
		stmt := db.stmts[statementID]
		if stmt == nil {
			return nil, &sqliteHostError{message: "prepared statement is closed", code: "ERR_SQLITE_CLOSED"}
		}
		delete(db.stmts, statementID)
		return nil, stmt.Close()
	case "close":
		return nil, db.closeLocked()
	case "begin":
		if sql != "BEGIN DEFERRED" && sql != "BEGIN IMMEDIATE" && sql != "BEGIN EXCLUSIVE" {
			return nil, &sqliteHostError{message: "invalid transaction mode", code: "ERR_INVALID_ARG"}
		}
		return nil, db.control(sql)
	case "commit":
		return nil, db.control("COMMIT")
	case "rollback":
		if conn.GetAutocommit() {
			return nil, nil
		}
		return nil, db.control("ROLLBACK")
	default:
		return nil, &sqliteHostError{message: "unsupported SQLite operation", code: "ERR_INVALID_ARG"}
	}
}

func (db *sqliteDB) statement(statementID int, sql string) (*sqlite3.Stmt, bool, error) {
	if statementID != 0 {
		stmt := db.stmts[statementID]
		if stmt == nil {
			return nil, false, &sqliteHostError{message: "prepared statement is closed", code: "ERR_SQLITE_CLOSED"}
		}
		return stmt, false, nil
	}
	stmt, err := sqlitePrepareSingle(db.conn, sql)
	return stmt, true, err
}

func (db *sqliteDB) control(sql string) error {
	db.allowTx = true
	err := db.conn.Exec(sql)
	db.allowTx = false
	return err
}

func (db *sqliteDB) closeLocked() error {
	if db.closed {
		return nil
	}
	for id, stmt := range db.stmts {
		_ = stmt.Close()
		delete(db.stmts, id)
	}
	if db.conn != nil {
		err := db.conn.Close()
		if err != nil {
			return err
		}
		db.conn = nil
	}
	db.closed = true
	db.host.releaseDB(db)
	return nil
}

func (db *sqliteDB) close() error {
	db.mu.Lock()
	defer db.mu.Unlock()
	return db.closeLocked()
}

func sqlitePrepareSingle(conn *sqlite3.Conn, sql string) (*sqlite3.Stmt, error) {
	stmt, tail, err := conn.Prepare(sql)
	if err != nil {
		if stmt != nil {
			_ = stmt.Close()
		}
		return nil, err
	}
	if stmt == nil {
		return nil, &sqliteHostError{message: "SQL must contain one statement", code: "ERR_INVALID_ARG"}
	}
	if rest, _, tailErr := conn.Prepare(tail); tailErr != nil {
		_ = stmt.Close()
		return nil, tailErr
	} else if rest != nil {
		_ = rest.Close()
		_ = stmt.Close()
		return nil, &sqliteHostError{message: "SQL must contain exactly one statement", code: "ERR_INVALID_ARG"}
	}
	return stmt, nil
}

func sqliteRunStatement(stmt *sqlite3.Stmt, operation string, params sqliteParamSet) (any, error) {
	_ = stmt.Reset()
	if err := stmt.ClearBindings(); err != nil {
		return nil, err
	}
	if err := sqliteBindParams(stmt, params); err != nil {
		_ = stmt.Reset()
		_ = stmt.ClearBindings()
		return nil, err
	}
	switch operation {
	case "run":
		if err := stmt.Exec(); err != nil {
			return nil, err
		}
		return sqliteRunResult{changes: stmt.Conn().Changes(), rowID: stmt.Conn().LastInsertRowID()}, nil
	case "get":
		if stmt.Step() {
			row := sqliteReadRow(stmt)
			err := stmt.Reset()
			_ = stmt.ClearBindings()
			if err != nil {
				return nil, err
			}
			return row, nil
		}
		err := stmt.Err()
		_ = stmt.Reset()
		_ = stmt.ClearBindings()
		if err != nil {
			return nil, err
		}
		return nil, nil
	case "all":
		rows := make(sqliteRows, 0)
		for stmt.Step() {
			rows = append(rows, sqliteReadRow(stmt))
		}
		err := stmt.Err()
		_ = stmt.Reset()
		_ = stmt.ClearBindings()
		if err != nil {
			return nil, err
		}
		return rows, nil
	default:
		_ = stmt.Reset()
		_ = stmt.ClearBindings()
		return nil, &sqliteHostError{message: "unsupported statement operation", code: "ERR_INVALID_ARG"}
	}
}

func sqliteReadRow(stmt *sqlite3.Stmt) sqliteRow {
	count := stmt.ColumnCount()
	row := sqliteRow{columns: make([]string, count), values: make([]sqliteCell, count)}
	for i := 0; i < count; i++ {
		row.columns[i] = stmt.ColumnName(i)
		switch stmt.ColumnType(i) {
		case sqlite3.INTEGER:
			row.values[i] = sqliteCell{kind: "integer", text: strconv.FormatInt(stmt.ColumnInt64(i), 10)}
		case sqlite3.FLOAT:
			row.values[i] = sqliteCell{kind: "number", value: stmt.ColumnFloat(i)}
		case sqlite3.TEXT:
			row.values[i] = sqliteCell{kind: "text", text: stmt.ColumnText(i)}
		case sqlite3.BLOB:
			bytes := stmt.ColumnRawBlob(i)
			row.values[i] = sqliteCell{kind: "blob", bytes: append([]byte{}, bytes...)}
		default:
			row.values[i] = sqliteCell{kind: "null"}
		}
	}
	return row
}

func sqliteBindParams(stmt *sqlite3.Stmt, params sqliteParamSet) error {
	count := stmt.BindCount()
	if len(params.named) > 0 {
		if len(params.positional) > 0 {
			return &sqliteHostError{message: "cannot mix positional and named SQLite parameters", code: "ERR_INVALID_ARG"}
		}
		bound := make(map[int]struct{}, len(params.named))
		for _, param := range params.named {
			index, err := sqliteNamedIndex(stmt, param.name)
			if err != nil {
				return err
			}
			if _, exists := bound[index]; exists {
				return &sqliteHostError{message: "SQLite parameter was supplied more than once", code: "ERR_INVALID_ARG"}
			}
			if err := sqliteBindCell(stmt, index, param.value); err != nil {
				return err
			}
			bound[index] = struct{}{}
		}
		for i := 1; i <= count; i++ {
			if _, ok := bound[i]; !ok {
				return &sqliteHostError{message: "a named SQLite parameter is missing", code: "ERR_INVALID_ARG"}
			}
		}
		return nil
	}
	if len(params.positional) != count {
		return &sqliteHostError{message: "positional SQLite parameter count does not match statement", code: "ERR_INVALID_ARG"}
	}
	for i, value := range params.positional {
		if err := sqliteBindCell(stmt, i+1, value); err != nil {
			return err
		}
	}
	return nil
}

func sqliteNamedIndex(stmt *sqlite3.Stmt, name string) (int, error) {
	if name == "" {
		return 0, &sqliteHostError{message: "SQLite parameter name must not be empty", code: "ERR_INVALID_ARG"}
	}
	if strings.HasPrefix(name, ":") || strings.HasPrefix(name, "$") || strings.HasPrefix(name, "@") {
		if index := stmt.BindIndex(name); index != 0 {
			return index, nil
		}
		return 0, &sqliteHostError{message: "unknown named SQLite parameter", code: "ERR_INVALID_ARG"}
	}
	match := 0
	for i := 1; i <= stmt.BindCount(); i++ {
		candidate := stmt.BindName(i)
		if candidate == "" {
			continue
		}
		if len(candidate) > 1 && candidate[1:] == name && (candidate[0] == ':' || candidate[0] == '$' || candidate[0] == '@') {
			if match != 0 && match != i {
				return 0, &sqliteHostError{message: "bare SQLite parameter name is ambiguous", code: "ERR_INVALID_ARG"}
			}
			match = i
		}
	}
	if match == 0 {
		return 0, &sqliteHostError{message: "unknown named SQLite parameter", code: "ERR_INVALID_ARG"}
	}
	return match, nil
}

func sqliteBindCell(stmt *sqlite3.Stmt, index int, cell sqliteCell) error {
	switch cell.kind {
	case "null":
		return stmt.BindNull(index)
	case "integer":
		value, err := sqliteParseInteger(cell.text)
		if err != nil {
			return &sqliteHostError{message: "integer SQLite parameter is outside the signed 64-bit range", code: "ERR_INVALID_ARG", cause: err}
		}
		return stmt.BindInt64(index, value)
	case "number":
		if !sqliteIsFinite(cell.value) {
			return &sqliteHostError{message: "number SQLite parameter must be finite", code: "ERR_INVALID_ARG"}
		}
		return stmt.BindFloat(index, cell.value)
	case "text":
		return stmt.BindText(index, cell.text)
	case "blob":
		return stmt.BindBlob(index, cell.bytes)
	default:
		return &sqliteHostError{message: "invalid SQLite parameter cell", code: "ERR_INVALID_ARG"}
	}
}

func sqliteParams(value quickjs.Value) (sqliteParamSet, error) {
	if value.IsNullish() {
		return sqliteParamSet{}, nil
	}
	if !value.IsObject() {
		return sqliteParamSet{}, &sqliteHostError{message: "SQLite parameters are malformed", code: "ERR_INVALID_ARG"}
	}
	var out sqliteParamSet
	positional, err := value.Get("positional")
	if err != nil {
		return out, &sqliteHostError{message: "SQLite parameters are malformed", code: "ERR_INVALID_ARG", cause: err}
	}
	if !positional.IsNullish() {
		if !positional.IsArray() {
			return out, &sqliteHostError{message: "positional SQLite parameters must be an array", code: "ERR_INVALID_ARG"}
		}
		out.positional = make([]sqliteCell, positional.Len())
		for i := range out.positional {
			cell, err := positional.Index(i)
			if err != nil {
				return out, &sqliteHostError{message: "SQLite parameters are malformed", code: "ERR_INVALID_ARG", cause: err}
			}
			out.positional[i], err = sqliteReadParamCell(cell)
			if err != nil {
				return out, err
			}
		}
	}
	named, err := value.Get("named")
	if err != nil {
		return out, &sqliteHostError{message: "SQLite parameters are malformed", code: "ERR_INVALID_ARG", cause: err}
	}
	if !named.IsNullish() {
		if !named.IsArray() {
			return out, &sqliteHostError{message: "named SQLite parameters must be an array", code: "ERR_INVALID_ARG"}
		}
		out.named = make([]sqliteNamedParam, named.Len())
		for i := range out.named {
			pair, err := named.Index(i)
			if err != nil || !pair.IsArray() || pair.Len() != 2 {
				return out, &sqliteHostError{message: "named SQLite parameter entry is malformed", code: "ERR_INVALID_ARG", cause: err}
			}
			name, err := pair.Index(0)
			if err != nil || !name.IsString() {
				return out, &sqliteHostError{message: "named SQLite parameter name must be a string", code: "ERR_INVALID_ARG", cause: err}
			}
			cell, err := pair.Index(1)
			if err != nil {
				return out, &sqliteHostError{message: "named SQLite parameter entry is malformed", code: "ERR_INVALID_ARG", cause: err}
			}
			parsed, err := sqliteReadParamCell(cell)
			if err != nil {
				return out, err
			}
			out.named[i] = sqliteNamedParam{name: name.String(), value: parsed}
		}
	}
	return out, nil
}

func sqliteReadParamCell(value quickjs.Value) (sqliteCell, error) {
	if !value.IsObject() {
		return sqliteCell{}, &sqliteHostError{message: "SQLite parameter cell is malformed", code: "ERR_INVALID_ARG"}
	}
	kind, err := value.Get("kind")
	if err != nil || !kind.IsString() {
		return sqliteCell{}, &sqliteHostError{message: "SQLite parameter cell is malformed", code: "ERR_INVALID_ARG", cause: err}
	}
	cell := sqliteCell{kind: kind.String()}
	switch cell.kind {
	case "null":
	case "integer", "text":
		text, err := value.Get("text")
		if err != nil || !text.IsString() {
			return sqliteCell{}, &sqliteHostError{message: "SQLite parameter cell is malformed", code: "ERR_INVALID_ARG", cause: err}
		}
		cell.text = text.String()
	case "number":
		number, err := value.Get("value")
		if err != nil || number.Kind() != quickjs.KindNumber {
			return sqliteCell{}, &sqliteHostError{message: "SQLite parameter cell is malformed", code: "ERR_INVALID_ARG", cause: err}
		}
		cell.value = number.Float()
		if !sqliteIsFinite(cell.value) {
			return sqliteCell{}, &sqliteHostError{message: "number SQLite parameter must be finite", code: "ERR_INVALID_ARG"}
		}
	case "blob":
		bytes, err := value.Get("bytes")
		if err != nil {
			return sqliteCell{}, &sqliteHostError{message: "SQLite parameter cell is malformed", code: "ERR_INVALID_ARG", cause: err}
		}
		buffer, ok := bytes.Bytes()
		if !ok {
			return sqliteCell{}, &sqliteHostError{message: "blob SQLite parameter must be a Uint8Array", code: "ERR_INVALID_ARG"}
		}
		cell.bytes = append([]byte{}, buffer...)
	default:
		return sqliteCell{}, &sqliteHostError{message: "unknown SQLite parameter cell kind", code: "ERR_INVALID_ARG"}
	}
	return cell, nil
}

func (h *sqliteHost) value(value any) (quickjs.Value, error) {
	switch result := value.(type) {
	case nil:
		return h.rt.Get("undefined")
	case sqliteOpenResult:
		o := h.rt.NewObject()
		if err := o.Set("id", result.id); err != nil {
			return quickjs.Value{}, err
		}
		return o, nil
	case sqlitePrepareResult:
		o := h.rt.NewObject()
		if err := o.Set("statementID", result.id); err != nil {
			return quickjs.Value{}, err
		}
		return o, nil
	case sqliteRunResult:
		o := h.rt.NewObject()
		changes, err := h.cellValue(sqliteCell{kind: "integer", text: strconv.FormatInt(result.changes, 10)})
		if err != nil {
			return quickjs.Value{}, err
		}
		rowID, err := h.cellValue(sqliteCell{kind: "integer", text: strconv.FormatInt(result.rowID, 10)})
		if err != nil {
			return quickjs.Value{}, err
		}
		if err := errors.Join(o.Set("changes", changes), o.Set("lastInsertRowid", rowID)); err != nil {
			return quickjs.Value{}, err
		}
		return o, nil
	case sqliteRow:
		return h.rowValue(result)
	case sqliteRows:
		items := make([]any, len(result))
		for i := range result {
			row, err := h.rowValue(result[i])
			if err != nil {
				return quickjs.Value{}, err
			}
			items[i] = row
		}
		return h.rt.NewArray(items...)
	default:
		return quickjs.Value{}, fmt.Errorf("unsupported SQLite result %T", value)
	}
}

func (h *sqliteHost) rowValue(row sqliteRow) (quickjs.Value, error) {
	columns := make([]any, len(row.columns))
	for i := range row.columns {
		columns[i] = row.columns[i]
	}
	columnValues, err := h.rt.NewArray(columns...)
	if err != nil {
		return quickjs.Value{}, err
	}
	values := make([]any, len(row.values))
	for i := range row.values {
		cell, err := h.cellValue(row.values[i])
		if err != nil {
			return quickjs.Value{}, err
		}
		values[i] = cell
	}
	rowValues, err := h.rt.NewArray(values...)
	if err != nil {
		return quickjs.Value{}, err
	}
	o := h.rt.NewObject()
	if err := errors.Join(o.Set("columns", columnValues), o.Set("values", rowValues)); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

func (h *sqliteHost) cellValue(cell sqliteCell) (quickjs.Value, error) {
	o := h.rt.NewObject()
	if err := o.Set("kind", cell.kind); err != nil {
		return quickjs.Value{}, err
	}
	switch cell.kind {
	case "integer", "text":
		if err := o.Set("text", cell.text); err != nil {
			return quickjs.Value{}, err
		}
	case "number":
		if err := o.Set("value", cell.value); err != nil {
			return quickjs.Value{}, err
		}
	case "blob":
		if err := o.Set("bytes", h.rt.NewBytes(cell.bytes)); err != nil {
			return quickjs.Value{}, err
		}
	}
	return o, nil
}

func (h *sqliteHost) reject(p *quickjs.Promise, err error) {
	_ = p.Reject(h.errorValue(err))
}

func (h *sqliteHost) errorValue(err error) quickjs.Value {
	value := h.rt.NewError("Error", err.Error())
	code := ""
	if hostErr, ok := err.(*sqliteHostError); ok {
		code = hostErr.code
	}
	if code == "" {
		code = sqliteSymbolicError(err)
	}
	if code != "" {
		_ = value.Set("code", code)
	}
	if primary, extended, ok := sqliteNumericError(err); ok {
		_ = value.Set("sqliteCode", int(primary))
		_ = value.Set("sqliteExtendedCode", int(extended))
	}
	return value
}

func sqliteSymbolicError(err error) string {
	primary, _, ok := sqliteNumericError(err)
	if !ok {
		return ""
	}
	if name, exists := sqliteErrorNames[primary]; exists {
		return name
	}
	return "SQLITE_ERROR"
}

func sqliteNumericError(err error) (int32, int32, bool) {
	var sqlErr *sqlite3.Error
	if errors.As(err, &sqlErr) {
		return int32(sqlErr.Code()), int32(sqlErr.ExtendedCode()), true
	}
	var extended sqlite3.ExtendedErrorCode
	if errors.As(err, &extended) {
		return int32(extended.Code()), int32(extended), true
	}
	var primary sqlite3.ErrorCode
	if errors.As(err, &primary) {
		return int32(primary), int32(primary), true
	}
	return 0, 0, false
}

var sqliteErrorNames = map[int32]string{
	1: "SQLITE_ERROR", 2: "SQLITE_INTERNAL", 3: "SQLITE_PERM", 4: "SQLITE_ABORT",
	5: "SQLITE_BUSY", 6: "SQLITE_LOCKED", 7: "SQLITE_NOMEM", 8: "SQLITE_READONLY",
	9: "SQLITE_INTERRUPT", 10: "SQLITE_IOERR", 11: "SQLITE_CORRUPT", 12: "SQLITE_NOTFOUND",
	13: "SQLITE_FULL", 14: "SQLITE_CANTOPEN", 15: "SQLITE_PROTOCOL", 16: "SQLITE_EMPTY",
	17: "SQLITE_SCHEMA", 18: "SQLITE_TOOBIG", 19: "SQLITE_CONSTRAINT", 20: "SQLITE_MISMATCH",
	21: "SQLITE_MISUSE", 22: "SQLITE_NOLFS", 23: "SQLITE_AUTH", 24: "SQLITE_FORMAT",
	25: "SQLITE_RANGE", 26: "SQLITE_NOTADB", 27: "SQLITE_NOTICE", 28: "SQLITE_WARNING",
	29: "SQLITE_ROW", 30: "SQLITE_DONE",
}

func sqliteIsFinite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func sqliteParseInteger(value string) (int64, error) { return strconv.ParseInt(value, 10, 64) }

func sqliteContextError(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}
	return &sqliteHostError{message: "SQLite operation was aborted", code: "ABORT_ERR", cause: ctx.Err()}
}

var _ = strings.Builder{}
var _ = time.Millisecond
