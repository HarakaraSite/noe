package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const sqlitePrelude = `import {open} from "noe:sqlite";
function check(ok,message){if(!ok)throw Error(message)}
async function rejects(fn,code){let error;try{await fn()}catch(caught){error=caught}check(error!==undefined,"expected rejection");if(code!==undefined)check(error.code===code,"error code "+error.code+" expected "+code);return error}
`

func executeSQLite(t *testing.T, profile stdlib.Profile, code string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[SQLite]", Virtual: true, Module: true, Code: sqlitePrelude + code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("code=%d error=%s", result.Code, stdlib.Diagnostic(err))
	}
}

func TestSQLiteMemoryTypesBindingsAndPreparedReuse(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();
check(db.path===":memory:"&&!db.readOnly&&!db.closed,"database metadata");
await db.exec("CREATE TABLE data(id INTEGER PRIMARY KEY,value TEXT,n INTEGER,b BLOB,optional REAL)");
const statement=await db.prepare("INSERT INTO data(value,n,b,optional) VALUES(:value,:n,:b,:optional)");
const bytes=new Uint8Array([0,255,128]);
const pending=statement.run({value:"日本語",n:9007199254740993n,b:bytes,optional:null});
bytes[0]=42;
const result=await pending;
check(result.changes===1&&result.lastInsertRowid===1,"run metadata");
await statement.run({":value":"second",":n":-9223372036854775808n,":b":new Uint8Array(0),":optional":1.25});
const rows=await db.all("SELECT * FROM data ORDER BY id");
check(rows.length===2&&rows[0].value==="日本語"&&rows[0].n===9007199254740993n,"text and bigint");
check(rows[0].b instanceof Uint8Array&&rows[0].b.join(",")==="0,255,128"&&rows[0].optional===null,"blob snapshot and null");
check(rows[1].n===-9223372036854775808n&&rows[1].optional===1.25&&rows[1].b.length===0,"int64 min, real and empty blob");
check(await db.get("SELECT * FROM data WHERE id=99")===undefined,"missing row");
const aliases=await db.get('SELECT 7 AS "__proto__", 8 AS constructor');
check(Object.getPrototypeOf(aliases)===null&&aliases.__proto__===7&&aliases.constructor===8,"row property names");
const query=await db.prepare("SELECT ? AS value,? AS bool,? AS optional");
check((await query.get(["before",true,undefined])).bool===1,"boolean and undefined binding");
check((await query.get(["after",false,null])).value==="after","prepared reuse");
await rejects(()=>query.get([1]),"ERR_INVALID_ARG");
check((await query.get(["recovered",false,null])).value==="recovered","recovery after binding error");
await rejects(()=>db.get("SELECT :x,$x",{x:1}),"ERR_INVALID_ARG");
check((await db.get("SELECT :x AS a,$x AS b",{":x":1,"$x":2})).b===2,"explicit named prefixes");
await query.close();await query.close();check(query.closed,"statement close");
await rejects(()=>query.get([1,2,3]),"ERR_SQLITE_CLOSED");
await statement.close();
await rejects(()=>db.get("SELECT ?",[9007199254740992]),"ERR_INVALID_ARG");
await rejects(()=>db.get("SELECT ?",[9223372036854775808n]),"ERR_INVALID_ARG");
await rejects(()=>db.get("SELECT ?",[Infinity]),"ERR_INVALID_ARG");
await rejects(()=>db.get("SELECT ?",[{}]),"ERR_INVALID_ARG");
await rejects(()=>db.prepare("SELECT 1; SELECT 2"),"ERR_INVALID_ARG");
const constraint=await rejects(()=>db.run("INSERT INTO data(id) VALUES(1)"));
check(String(constraint.code).startsWith("SQLITE_CONSTRAINT"),"SQLite symbolic error code");
`)
}

func TestSQLiteTransactionsReserveConnectionAndRollback(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await db.exec("CREATE TABLE data(value INTEGER UNIQUE)");
let release,entered;const gate=new Promise(resolve=>{release=resolve}),started=new Promise(resolve=>{entered=resolve});
const transaction=db.transaction(async tx=>{
 await tx.run("INSERT INTO data VALUES(?)",[1]);entered();await gate;
 await tx.run("INSERT INTO data VALUES(?)",[2]);return "committed";
});
await started;const unrelated=db.run("INSERT INTO data VALUES(?)",[3]);release();
check(await transaction==="committed","transaction return value");await unrelated;
check((await db.all("SELECT value FROM data ORDER BY rowid")).map(row=>row.value).join(",")==="1,2,3","outside operation interleaved transaction");
const reason={marker:"callback"};
try{await db.transaction(async tx=>{await tx.run("INSERT INTO data VALUES(4)");throw reason})}catch(error){check(error===reason,"callback reason lost")}
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===3,"callback rollback");
await rejects(()=>db.transaction(async tx=>{
 await tx.run("INSERT INTO data VALUES(5)");
 await rejects(()=>tx.run("INSERT INTO data VALUES(1)"));
}));
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===3,"caught SQL failure must roll back transaction");
let escaped,statement;
await db.transaction(async tx=>{escaped=tx;statement=await tx.prepare("INSERT INTO data VALUES(?)");await statement.run([6])},{mode:"immediate"});
await rejects(()=>escaped.get("SELECT 1"),"ERR_SQLITE_CLOSED");
await rejects(()=>statement.run([7]),"ERR_SQLITE_CLOSED");await statement.close();
await rejects(()=>db.exec("BEGIN"));
await rejects(()=>db.exec("ATTACH ':memory:' AS other"));
await db.transaction(async tx=>{check((await tx.get("SELECT COUNT(*) AS n FROM data")).n===4,"transaction after rollback")});
`)
}

func TestSQLiteAutomaticRollbackStopsLaterTransactionSQL(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await db.exec("CREATE TABLE data(value INTEGER UNIQUE)");
for(const prequeue of [false,true]){
 let firstFailure;
 const transaction=db.transaction(async tx=>{
  const statement=await tx.prepare("INSERT INTO data VALUES(?)");
  await statement.run([1]);
  if(prequeue){
   const failure=tx.run("INSERT OR ROLLBACK INTO data VALUES(1)");
   const later=tx.run("INSERT INTO data VALUES(2)");
   firstFailure=await rejects(()=>failure);await rejects(()=>later);
  }else{
   firstFailure=await rejects(()=>tx.run("INSERT OR ROLLBACK INTO data VALUES(1)"));
   await rejects(()=>tx.run("INSERT INTO data VALUES(2)"));
  }
  // Resource cleanup remains admitted after transaction SQL has failed.
  await statement.close();
 });
 const failure=await rejects(()=>transaction);
 check(failure===firstFailure&&String(failure.code).startsWith("SQLITE_CONSTRAINT"),"automatic rollback lost original SQL failure");
 check((await db.get("SELECT COUNT(*) AS n FROM data")).n===0,"SQL escaped rolled-back transaction");
}
await db.transaction(async tx=>{await tx.run("INSERT INTO data VALUES(3)")});
check((await db.get("SELECT value FROM data")).value===3,"connection not usable after automatic rollback");
`)
}

func TestSQLiteCancellationKeepsEventLoopAndConnectionUsable(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();const controller=new AbortController(),reason={cancelled:true};let ticked=false;
const task=db.get("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<1000000000) SELECT sum(n) FROM t",[],{signal:controller.signal});
setTimeout(()=>{ticked=true;controller.abort(reason)},1);
try{await task;throw Error("long query completed")}catch(error){check(error===reason,"abort reason lost")}
check(ticked,"SQL blocked the timer");
check((await db.get("SELECT 42 AS value")).value===42,"connection not recovered after interrupt");
const transactionController=new AbortController();let started;
const ready=new Promise(resolve=>{started=resolve});
const transaction=db.transaction(async tx=>{await tx.exec("CREATE TABLE rolled_back(value)");started();await new Promise(()=>{})},{signal:transactionController.signal});
await ready;transactionController.abort("transaction cancelled");
try{await transaction;throw Error("transaction not cancelled")}catch(error){check(error==="transaction cancelled","transaction abort reason")}
check((await db.get("SELECT COUNT(*) AS n FROM sqlite_master WHERE name='rolled_back'")).n===0,"cancelled callback did not release reservation and roll back");
`)
}

func TestSQLiteFilePersistenceRootReadOnlyAndMemoryAuthority(t *testing.T) {
	root := t.TempDir()
	profile := stdlib.Profile{Environment: stdlib.Environment{Cwd: "/"}, Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}}
	executeSQLite(t, profile, `
const db=await open("notes.db");
await db.exec("CREATE TABLE notes(value TEXT)");
await db.run("INSERT INTO notes VALUES(?)",["persisted"]);
// Execution shutdown also closes a database whose caller omitted close().
`)
	data, err := os.ReadFile(filepath.Join(root, "notes.db"))
	if err != nil || !strings.HasPrefix(string(data), "SQLite format 3") {
		t.Fatalf("database file: bytes=%d err=%v", len(data), err)
	}
	profile.Modules.FS.ReadOnly = true
	executeSQLite(t, profile, `
await using db=await open("/notes.db",{readOnly:true});
check((await db.get("SELECT value FROM notes")).value==="persisted","file reopen");
await db.transaction(async tx=>{check((await tx.get("SELECT COUNT(*) AS n FROM notes")).n===1,"read-only transaction")});
await rejects(()=>db.run("INSERT INTO notes VALUES('blocked')"));
await rejects(()=>open("/new.db"),"ERR_READ_ONLY");
await using memory=await open();await memory.exec("CREATE TABLE allowed(value)");
`)
	executeSQLite(t, stdlib.Profile{}, `await using db=await open();await rejects(()=>open("forbidden.db"),"ERR_FS_DISABLED");`)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	profile.Modules.FS.ReadOnly = false
	executeSQLite(t, profile, `
await rejects(()=>open("/escape/other.db"));
await using db=await open("notes.db");
await rejects(()=>db.exec("ATTACH '/escape/attached.db' AS other"));
await rejects(()=>db.exec("VACUUM INTO '/escape/vacuum.db'"));
await rejects(()=>db.exec("PRAGMA temp_store_directory='/escape'"));
`)
	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatalf("outside root files=%v err=%v", entries, err)
	}
}

func TestSQLiteCloseDrainsQueuedOperationsAndAsyncDisposal(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
const db=await open();await db.exec("CREATE TABLE data(value)");
const first=db.run("INSERT INTO data VALUES(1)"),second=db.get("SELECT COUNT(*) AS n FROM data");
const closing=db.close();check(db.closed,"close did not reject new work immediately");
await rejects(()=>db.get("SELECT 1"),"ERR_SQLITE_CLOSED");
await first;check((await second).n===1,"close overtook queued operations");await closing;await db.close();
const other=await open();let query;
{await using stmt=await other.prepare("SELECT 1");query=stmt;check((await stmt.get())["1"]===1,"prepared read")}
await rejects(()=>query.get(),"ERR_SQLITE_CLOSED");await other.close();
`)
}

func TestSQLiteQueuedCancellationSkipsSQL(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await db.exec("CREATE TABLE data(value)");
let entered,release;const ready=new Promise(resolve=>{entered=resolve}),gate=new Promise(resolve=>{release=resolve});
const transaction=db.transaction(async tx=>{await tx.run("INSERT INTO data VALUES(1)");entered();await gate});
await ready;const controller=new AbortController(),reason={queued:true};
const queued=db.run("INSERT INTO data VALUES(2)",[],{signal:controller.signal});controller.abort(reason);
try{await queued;throw Error("queued operation completed")}catch(error){check(error===reason,"queued abort reason")}
release();await transaction;
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===1,"cancelled queued SQL executed");
`)
}

func TestSQLiteWALAndReadOnlyFilesystemCreateNoSharedMemory(t *testing.T) {
	root := t.TempDir()
	profile := stdlib.Profile{Environment: stdlib.Environment{Cwd: "/"}, Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}}
	executeSQLite(t, profile, `
await using db=await open("wal.db");await db.exec("PRAGMA journal_mode=WAL;CREATE TABLE data(value);INSERT INTO data VALUES(42)");
check((await db.get("SELECT value FROM data")).value===42,"WAL read/write");
`)
	profile.Modules.FS.ReadOnly = true
	before, err := os.ReadFile(filepath.Join(root, "wal.db"))
	if err != nil {
		t.Fatal(err)
	}
	executeSQLite(t, profile, `
// The first slice refuses WAL shared memory on a read-only filesystem.
await rejects(()=>open("wal.db",{readOnly:true}),"ERR_READ_ONLY");
`)
	after, err := os.ReadFile(filepath.Join(root, "wal.db"))
	if err != nil || string(before) != string(after) {
		t.Fatalf("read-only WAL database changed: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 || entries[0].Name() != "wal.db" {
		t.Fatalf("read-only WAL side effects: entries=%v err=%v", entries, err)
	}
}

func TestSQLiteWorkerAndExecutionCancellation(t *testing.T) {
	profile := stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}
	worker := sqlitePrelude + `await using db=await open();await db.exec("CREATE TABLE data(value)");await db.run("INSERT INTO data VALUES(42)");import {parentPort} from "node:worker_threads";parentPort.postMessage((await db.get("SELECT value FROM data")).value);parentPort.close();`
	executeSQLite(t, profile, fmt.Sprintf(`
import {Worker} from "node:worker_threads";const worker=new Worker(%q,{eval:true});
check(await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)})===42,"worker result");await worker.terminate();`, worker))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	profile = stdlib.Profile{Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
		return nil, bindings.Runtime().Set("ready", func() { close(ready) })
	}}
	runtime, err := stdlib.NewRuntime(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}
	defer runtime.Close()
	go func() {
		select {
		case <-ready:
			runtime.Cancel()
		case <-ctx.Done():
		}
	}()
	_, err = runtime.Run(stdlib.Source{Name: "[SQLite cancellation]", Virtual: true, Module: true, Code: sqlitePrelude + `const db=await open();ready();await db.get("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<1000000000) SELECT sum(n) FROM t");`})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
