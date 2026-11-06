package stdlib

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	sqlite3 "github.com/ncruces/go-sqlite3"
	"github.com/ncruces/go-sqlite3/vfs"
)

func TestSQLiteNativeVFSUsesBackendCwdAndRejectsRootEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink privileges vary on Windows")
	}
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "data"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "data", "escape")); err != nil {
		t.Fatal(err)
	}
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/data"})
	if err != nil {
		t.Fatal(err)
	}
	name := "noe_sqlite_native_test"
	wrapped := newSQLiteSandboxVFS(backend)
	vfs.Register(name, wrapped)
	t.Cleanup(func() { vfs.Unregister(name) })

	conn, err := sqlite3.OpenFlags(sqliteDatabaseURI("notes draft.db", name), sqlite3.OPEN_URI|sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE notes(value TEXT); INSERT INTO notes VALUES('ok')"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "data", "notes draft.db")); err != nil {
		t.Fatalf("database was not placed at backend Cwd: %v", err)
	}
	if _, err := wrapped.FullPathname("../../outside.db"); err == nil {
		t.Fatal("path traversal escaped the filesystem root")
	}
	if _, err := wrapped.FullPathname("escape/other.db"); err == nil {
		t.Fatal("symlink traversal escaped the filesystem root")
	}
	if _, err := wrapped.validatePhysical(filepath.Join(outside, "other.db")); err == nil {
		t.Fatal("canonical physical path outside the root was accepted")
	}
}

func TestSQLiteNativeVFSAllowsMemoryWithoutFilesystem(t *testing.T) {
	wrapped := newSQLiteSandboxVFS(nil)
	if path, err := wrapped.FullPathname(":memory:"); err != nil || path != ":memory:" {
		t.Fatalf("memory FullPathname=%q, err=%v", path, err)
	}
	if _, err := wrapped.FullPathname("database.db"); err == nil {
		t.Fatal("file path was accepted without filesystem capability")
	}
}

func TestSQLiteNativeReadOnlyWALHeaderUsesFilesystemBackend(t *testing.T) {
	root := t.TempDir()
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/"})
	if err != nil {
		t.Fatal(err)
	}
	name := "noe_sqlite_native_wal_test"
	vfs.Register(name, newSQLiteSandboxVFS(backend))
	t.Cleanup(func() { vfs.Unregister(name) })
	conn, err := sqlite3.OpenFlags(sqliteDatabaseURI("notes.db", name), sqlite3.OPEN_URI|sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("PRAGMA journal_mode=WAL; CREATE TABLE notes(value)"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	readOnlyBackend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	host := &sqliteHost{backend: readOnlyBackend}
	if !host.readOnlyWALDatabase(context.Background(), "/notes.db") {
		t.Fatal("WAL database was not recognized by its SQLite header")
	}
	if err := os.WriteFile(filepath.Join(root, "not-sqlite.db"), []byte("SQLite format 3\x00"), 0o600); err != nil {
		t.Fatal(err)
	}
	if host.readOnlyWALDatabase(context.Background(), "/not-sqlite.db") {
		t.Fatal("short or non-WAL file was misidentified as a WAL database")
	}
	fakeHeader := make([]byte, 20)
	fakeHeader[18], fakeHeader[19] = 2, 2
	if err := os.WriteFile(filepath.Join(root, "fake-header.db"), fakeHeader, 0o600); err != nil {
		t.Fatal(err)
	}
	if host.readOnlyWALDatabase(context.Background(), "/fake-header.db") {
		t.Fatal("file without the SQLite header was misidentified as a WAL database")
	}
}

func TestSQLiteNativeReadOnlyRollbackJournalFile(t *testing.T) {
	root := t.TempDir()
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/"})
	if err != nil {
		t.Fatal(err)
	}
	writableName := "noe_sqlite_native_ro_writer"
	vfs.Register(writableName, newSQLiteSandboxVFS(backend))
	t.Cleanup(func() { vfs.Unregister(writableName) })
	conn, err := sqlite3.OpenFlags(sqliteDatabaseURI("notes.db", writableName), sqlite3.OPEN_URI|sqlite3.OPEN_READWRITE|sqlite3.OPEN_CREATE)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Exec("CREATE TABLE notes(value TEXT); INSERT INTO notes VALUES('ok')"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}

	readOnlyBackend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	readOnlyName := "noe_sqlite_native_ro_reader"
	vfs.Register(readOnlyName, newSQLiteSandboxVFS(readOnlyBackend))
	t.Cleanup(func() { vfs.Unregister(readOnlyName) })
	readOnly, err := sqlite3.OpenFlags(sqliteDatabaseURI("/notes.db", readOnlyName), sqlite3.OPEN_URI|sqlite3.OPEN_READONLY)
	if err != nil {
		t.Fatal(err)
	}
	stmt, tail, err := readOnly.Prepare("SELECT value FROM notes")
	if err != nil || tail != "" || stmt == nil {
		t.Fatalf("prepare read-only query: stmt=%v tail=%q err=%v", stmt, tail, err)
	}
	if !stmt.Step() || stmt.ColumnText(0) != "ok" {
		t.Fatalf("read-only query failed: %v", stmt.Err())
	}
	if err := stmt.Close(); err != nil {
		t.Fatal(err)
	}
	if err := readOnly.Close(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.db" {
		t.Fatalf("read-only rollback-journal open changed directory entries: %v", entries)
	}
}

func TestSQLiteNativeRollbackAfterAutomaticRollbackIsNoOp(t *testing.T) {
	conn, err := sqlite3.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.Exec("CREATE TABLE values_table(value INTEGER UNIQUE); INSERT INTO values_table VALUES(1)"); err != nil {
		t.Fatal(err)
	}
	db := &sqliteDB{conn: conn, stmts: make(map[int]*sqlite3.Stmt)}
	if err := conn.SetAuthorizer(db.authorize); err != nil {
		t.Fatal(err)
	}
	if err := db.control("BEGIN DEFERRED"); err != nil {
		t.Fatal(err)
	}
	stmt, err := sqlitePrepareSingle(conn, "INSERT OR ROLLBACK INTO values_table VALUES(1)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sqliteRunStatement(stmt, "run", sqliteParamSet{}); err == nil {
		t.Fatal("duplicate insert unexpectedly succeeded")
	}
	_ = stmt.Close()
	if !conn.GetAutocommit() {
		t.Fatal("INSERT OR ROLLBACK did not abort its transaction")
	}
	if _, err := db.request(context.Background(), 0, "rollback", "", sqliteParamSet{}); err != nil {
		t.Fatalf("cleanup rollback returned an error after SQLite already rolled back: %v", err)
	}
	if err := conn.Exec("INSERT INTO values_table VALUES(2)"); err != nil {
		t.Fatalf("connection was not usable after automatic rollback cleanup: %v", err)
	}
}
