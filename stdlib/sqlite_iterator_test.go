package stdlib_test

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const sqliteRangeSQL = "WITH RECURSIVE data(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM data WHERE n<513) SELECT n,printf('row-%d',n) AS text,CAST(n AS BLOB) AS bytes FROM data"

func TestSQLiteIterateRowsAndBindings(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, fmt.Sprintf(`
await using db=await open();
for(const batchSize of [1,7,128,512,9007199254740991]) {
 const rows=db.iterate(%q,[],{batchSize});
 check(!rows.closed&&rows[Symbol.asyncIterator]()===rows,"iterator initial state");
 let count=0,prior;
 for await(const value of rows) {
  count++;
  check(value.n===count&&value.text==="row-"+count&&Object.getPrototypeOf(value)===null,"row order/types");
  check(new TextDecoder().decode(value.bytes)===String(count),"BLOB values");
  if(prior)check(prior!==value.bytes,"BLOB results have independent storage");
  prior=value.bytes;value.bytes.fill(88);
 }
 check(count===513&&rows.closed&&(await rows.next()).done,"complete iteration and EOF");
 await rows.close();await rows.close();
}
const bytes=new Uint8Array([0,255,128]);
const values={big:9007199254740993n,bytes,optional:undefined};
const iterator=db.iterate('SELECT :big AS "__proto__",:bytes AS bytes,:optional AS optional',values,{batchSize:1});
values.big=1;bytes.fill(0);
const value=(await iterator.next()).value;
check(value.__proto__===9007199254740993n&&value.bytes.join()==="0,255,128"&&value.optional===null,"creation snapshots bound parameters");
await iterator.return();check(iterator.closed,"explicit return closes");
check((await db.iterate("SELECT 1 WHERE 0").next()).done,"empty query");
const lazy=db.iterate("not valid SQL");await lazy.close();check((await lazy.next()).done,"close before first read skips SQL");
for(const batchSize of [0,-1,1.5,NaN,Infinity,"2",2n]) {
 let error;try{db.iterate("SELECT 1",[],{batchSize})}catch(value){error=value}
 check(error?.code==="ERR_INVALID_ARG","batch size validation");
}
const invalid=db.iterate("SELECT 1; SELECT 2");
await rejects(()=>invalid.next(),"ERR_INVALID_ARG");check(invalid.closed&&(await invalid.next()).done,"SQL error closes iterator");
`, sqliteRangeSQL))
}

func TestSQLiteIterateStatementReuseAndInterleaving(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await using statement=await db.prepare("SELECT ? AS n UNION ALL SELECT ?");
const rows=statement.iterate([1,2],{batchSize:1});
check((await rows.next()).value.n===1,"prepared first row");
check((await db.get("SELECT 42 AS n")).n===42,"connection can run other SQL between batches");
await rejects(()=>statement.get([3,4]),"ERR_SQLITE_BUSY");
const other=statement.iterate([3,4]);await rejects(()=>other.next(),"ERR_SQLITE_BUSY");
await rows.return();check((await statement.get([5,6])).n===5,"prepared statement reusable after close");
for await(const row of statement.iterate([7,8],{batchSize:1})) {check(row.n===7,"break row");break;}
check((await statement.get([9,10])).n===9,"break releases statement");
{await using scoped=statement.iterate([11,12],{batchSize:1});check((await scoped.next()).value.n===11,"async disposal first row");}
check((await statement.get([13,14])).n===13,"async disposal releases statement");
const closing=statement.iterate([15,16],{batchSize:1});await closing.next();await statement.close();
check(closing.closed&&(await closing.next()).done,"statement close closes active iterator");
const batched=db.iterate("SELECT 1 AS n UNION ALL SELECT 2 UNION ALL SELECT 3",[],{batchSize:1});
const results=await Promise.all([batched.next(),batched.next(),batched.next(),batched.next()]);
check(results.map(value=>value.done?"end":value.value.n).join()==="1,2,3,end","overlapping next calls serialize");
const untouched=db.iterate("CREATE TABLE must_not_run(n)");const first=untouched.next(),returned=untouched.return("stop");
check((await first).done&&(await returned).value==="stop","return before queued SQL starts");
check((await db.get("SELECT COUNT(*) AS n FROM sqlite_master WHERE name='must_not_run'")).n===0,"closed queued iterator executes no SQL");
`)
}

func TestSQLitePreparedRowColumnsFollowSchemaChanges(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();
await db.exec("CREATE TABLE data(id INTEGER, label TEXT); INSERT INTO data VALUES(1,'first'),(2,'second')");
await using statement=await db.prepare("SELECT * FROM data ORDER BY id");
check((await statement.all())[1].label==="second","initial prepared columns");
await db.exec("ALTER TABLE data RENAME COLUMN label TO name; ALTER TABLE data ADD COLUMN extra DEFAULT 42");
const rows=await statement.all();
check(rows.length===2&&rows[0].name==="first"&&rows[1].extra===42&&!("label" in rows[0]),"all after reprepare");
await db.exec("ALTER TABLE data RENAME COLUMN name TO title");
await using iterator=statement.iterate([],{batchSize:1});
const first=(await iterator.next()).value;
check(first.title==="first"&&first.extra===42&&!("name" in first),"cursor first batch after reprepare");
await db.get("SELECT 'unrelated' AS title");
const second=(await iterator.next()).value;
check(second.title==="second"&&second.extra===42&&first!==second,"cursor columns survive between batches");
check((await iterator.next()).done,"cursor EOF");
await db.exec("ALTER TABLE data RENAME COLUMN title TO label");
check((await statement.get()).label==="first","get after cursor reuse and reprepare");
check((await db.all("SELECT * FROM data WHERE 0")).length===0,"empty all");
`)
}

func TestSQLiteBulkRowColumnsStayPrivate(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();
await using statement=await db.prepare(
 "SELECT 1 AS duplicate, 2 AS duplicate, 3 AS __proto__, 4 AS constructor, X'01' AS bytes " +
 "UNION ALL SELECT 5, 6, 7, 8, X'01'");
const map=Array.prototype.map;
let leaked=false;
Array.prototype.map=function(...args) {
 for(let i=0;i<this.length;i++) if(Object.hasOwn(this[i],"columns")) leaked=true;
 return Reflect.apply(map,this,args);
};
Object.defineProperty(Object.prototype,"columns",{configurable:true,set(){leaked=true;}});
Object.defineProperty(Object.prototype,"then",{configurable:true,get(){
 if(Object.hasOwn(this,"columns")) leaked=true;
}});
try {
 function verify(first,second) {
  check(Object.getPrototypeOf(first)===null&&Object.getPrototypeOf(second)===null,"null row prototypes");
  check(Object.keys(first).join()==="duplicate,__proto__,constructor,bytes","ordered own columns");
  check(first.duplicate===2&&first.__proto__===3&&first.constructor===4,"duplicate and prototype column names");
  check(second.duplicate===6&&second.__proto__===7&&second.constructor===8,"second row values");
  first.duplicate=0;first.bytes[0]=9;
  check(first!==second&&second.duplicate===6&&second.bytes[0]===1,"independent rows and BLOBs");
 }
 const all=await statement.all();
 verify(all[0],all[1]);
 for(const batchSize of [1,2]) {
  await using iterator=statement.iterate([],{batchSize});
  verify((await iterator.next()).value,(await iterator.next()).value);
  check((await iterator.next()).done,"iterator EOF");
 }
 check(!leaked,"column metadata did not escape through row arrays or inherited setters");
} finally {
 Array.prototype.map=map;
 delete Object.prototype.columns;
 delete Object.prototype.then;
}
`)
}

func TestSQLiteIterateTransactionLifetimeAndFailure(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await db.exec("CREATE TABLE data(n UNIQUE)");let escaped,prepared,preparedRows;
await db.transaction(async tx=>{
 await tx.run("INSERT INTO data VALUES(1),(2),(3)");
 escaped=tx.iterate("SELECT n FROM data ORDER BY n",[],{batchSize:1});
 check((await escaped.next()).value.n===1,"transaction first row");
 check((await tx.get("SELECT COUNT(*) AS n FROM data")).n===3,"transaction SQL between batches");
 prepared=await tx.prepare("SELECT n FROM data ORDER BY n");preparedRows=prepared.iterate([],{batchSize:1});await preparedRows.next();
});
check(escaped.closed&&preparedRows.closed&&prepared.closed,"transaction exit closes owned iterators/statements");
check((await escaped.next()).done&&(await preparedRows.next()).done,"escaped iterators cannot yield buffered data");
let iterator;
const failure=await rejects(()=>db.transaction(async tx=>{
 await tx.run("INSERT INTO data VALUES(4)");
 iterator=tx.iterate("SELECT missing FROM data");await rejects(()=>iterator.next());
}));
check(String(failure.code).startsWith("SQLITE_ERROR")&&iterator.closed,"caught cursor SQL failure rolls back");
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===3,"failed cursor transaction rolled back");
await db.transaction(async tx=>{
 for await(const value of tx.iterate("SELECT n FROM data",[],{batchSize:1})) {await tx.get("SELECT 1");break;}
 await tx.run("INSERT INTO data VALUES(5)");
});
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===4,"normal break does not roll back");
for(const preAborted of [false,true]) {
 const reason={preAborted},controller=new AbortController();if(preAborted)controller.abort(reason);
 check(await rejects(()=>db.transaction(async tx=>{
  await tx.run("INSERT INTO data VALUES(6)");
  const rows=tx.iterate("SELECT n FROM data",[],{signal:controller.signal});
  if(!preAborted){await rows.next();controller.abort(reason);}
  check(await rejects(()=>rows.next())===reason,"transaction iterator abort reason");
 }))===reason,"caught idle/pre-aborted iterator cancellation rolls back");
 check((await db.get("SELECT COUNT(*) AS n FROM data WHERE n=6")).n===0,"cancelled iterator transaction rolled back");
}
await db.transaction(async tx=>{
 const controller=new AbortController();
 const completed=tx.iterate("SELECT 1",[],{signal:controller.signal});for await(const row of completed)void row;
 controller.abort("after EOF");
 await tx.run("INSERT INTO data VALUES(7)");
});
check((await db.get("SELECT COUNT(*) AS n FROM data WHERE n=7")).n===1,"EOF does not fail transaction");
const reason={duringCleanup:true};let aborting;
check(await rejects(()=>db.transaction(async tx=>{
 await tx.run("INSERT INTO data VALUES(8)");
 const first=tx.iterate("SELECT 1 UNION ALL SELECT 2",[],{batchSize:1});await first.next();
 const controller=new AbortController();
 const second=tx.iterate("SELECT 1 UNION ALL SELECT 2",[],{batchSize:1,signal:controller.signal});await second.next();
 aborting=(async()=>{
  while(!first.closed)await Promise.resolve();
  check(!second.closed,"second iterator remains live during first cursor cleanup");
  controller.abort(reason);
 })();
}))===reason,"iterator cancellation during transaction cleanup prevents commit");
await aborting;
check((await db.get("SELECT COUNT(*) AS n FROM data WHERE n=8")).n===0,"cleanup-window abort rolls back");
`)
}

func TestSQLiteIterateReturningAndQueuedAbort(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
await using db=await open();await db.exec("CREATE TABLE data(n)");
for await(const row of db.iterate("INSERT INTO data VALUES(1),(2),(3) RETURNING n",[],{batchSize:1})) {check(row.n===1,"RETURNING first row");break;}
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===3,"early close does not undo writes");
const reason={rollback:true};
check(await rejects(()=>db.transaction(async tx=>{
 for await(const row of tx.iterate("INSERT INTO data VALUES(4),(5) RETURNING n",[],{batchSize:1})) {check(row.n===4,"transaction RETURNING");break;}
 throw reason;
}))===reason,"transaction callback rollback");
check((await db.get("SELECT COUNT(*) AS n FROM data")).n===3,"transaction rolls back RETURNING writes");
let release,started;const gate=new Promise(resolve=>release=resolve),ready=new Promise(resolve=>started=resolve);
const transaction=db.transaction(async tx=>{started();await gate;});await ready;
const controller=new AbortController();
const queued=db.iterate("INSERT INTO data VALUES(99) RETURNING n",[],{signal:controller.signal});
const reading=queued.next();controller.abort(reason);
const count=db.get("SELECT COUNT(*) AS n FROM data");
const closing=db.close();release();
check(await rejects(()=>reading)===reason,"queued iterator abort reason");
await transaction;await queued.close();await closing;
check(queued.closed&&(await count).n===3,"close drains queued cursor cancellation and skips SQL");
`)
}

func TestSQLiteIterateAbortAndClose(t *testing.T) {
	executeSQLite(t, stdlib.Profile{}, `
const db=await open(),reason={cancelled:true},controller=new AbortController();
const rows=db.iterate("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<1000000000) SELECT sum(n) FROM t",[],{signal:controller.signal});
const reading=rows.next();setTimeout(()=>controller.abort(reason),1);
check(await rejects(()=>reading)===reason,"running iterator preserves abort reason");
await rows.close();check(rows.closed&&(await rows.next()).done,"aborted iterator cleanup");
check((await db.get("SELECT 42 AS n")).n===42,"connection usable after abort");
await using statement=await db.prepare("SELECT 1 AS n UNION ALL SELECT 2");
const bufferedController=new AbortController();
const buffered=statement.iterate([],{batchSize:128,signal:bufferedController.signal});await buffered.next();
bufferedController.abort(reason);check(await rejects(()=>buffered.next())===reason,"abort discards buffered rows");
await buffered.close();check((await statement.get()).n===1,"statement reusable after buffered abort");
const before=new AbortController();before.abort(reason);
const skipped=db.iterate("CREATE TABLE must_not_run(n)",[],{signal:before.signal});
check(await rejects(()=>skipped.next())===reason,"pre-aborted lazy iterator");
check((await db.get("SELECT COUNT(*) AS n FROM sqlite_master WHERE name='must_not_run'")).n===0,"aborted cursor executes no SQL");
const pending=db.iterate("SELECT 1 AS n UNION ALL SELECT 2",[],{batchSize:1});await pending.next();
await db.close();check(pending.closed&&(await pending.next()).done,"database close discards cursor remainder");
await pending.close();await db.close();
`)
}

func TestSQLiteIterateWorker(t *testing.T) {
	worker := sqlitePrelude + `import {parentPort} from "node:worker_threads";
await using db=await open();let count=0;
for await(const row of db.iterate("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<257) SELECT n FROM t",[],{batchSize:7}))check(row.n===++count,"Worker row order");
parentPort.postMessage(count);parentPort.close();`
	executeSQLite(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, fmt.Sprintf(`
import {Worker} from "node:worker_threads";const worker=new Worker(%q,{eval:true,type:"module"});
check(await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)})===257,"Worker iteration");await worker.terminate();
`, worker))
}

func TestSQLiteIterateCSVExampleAndReadOnlyFile(t *testing.T) {
	root := t.TempDir()
	profile := stdlib.Profile{Environment: stdlib.Environment{Cwd: "/"}, Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}}
	executeSQLite(t, profile, `
await using db=await open("notes.db");await db.exec("CREATE TABLE notes(id INTEGER PRIMARY KEY,text TEXT NOT NULL)");
await db.run("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<150) INSERT INTO notes SELECT n,? FROM t",['a"b,\n日本語']);
`)
	source, err := os.ReadFile("../examples/sqlite/export-csv.js")
	if err != nil {
		t.Fatal(err)
	}
	profile.Environment.Args = []string{"noe", "export-csv.js", "notes.db", "notes.csv"}
	profile.Modules.ProcessInfo = true
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "[SQLite CSV example]", Virtual: true, Module: true, Code: string(source)}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	data, err := os.ReadFile(filepath.Join(root, "notes.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(rows) != 151 || strings.Join(rows[0], ",") != "id,text" || rows[150][0] != "150" || rows[150][1] != "a\"b,\n日本語" {
		t.Fatalf("CSV output: rows=%d error=%v", len(rows), err)
	}
	profile.Modules.FS.ReadOnly = true
	executeSQLite(t, profile, `
await using db=await open("notes.db",{readOnly:true});let count=0;
for await(const row of db.iterate("SELECT id,text FROM notes ORDER BY id",[],{batchSize:7}))check(row.id===++count,"read-only cursor order");
check(count===150,"read-only cursor count");
const writes=db.iterate("INSERT INTO notes(text) VALUES('blocked') RETURNING id");await rejects(()=>writes.next());
check(writes.closed,"read-only cursor failure closes");
check((await db.get("SELECT COUNT(*) AS n FROM notes")).n===150,"read-only iterator did not change data");
`)
}

func TestSQLiteIterateExecutionCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	ready := make(chan struct{})
	runtime, err := stdlib.NewRuntime(ctx, stdlib.Profile{Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
		return nil, bindings.Runtime().Set("ready", func() { close(ready) })
	}})
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
	_, err = runtime.Run(stdlib.Source{Name: "[SQLite iterator cancellation]", Virtual: true, Module: true, Code: sqlitePrelude + `
const db=await open(),rows=db.iterate("WITH RECURSIVE t(n) AS (SELECT 1 UNION ALL SELECT n+1 FROM t WHERE n<1000000000) SELECT sum(n) FROM t");ready();await rows.next();`})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("runtime cancellation: %v", err)
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
}
