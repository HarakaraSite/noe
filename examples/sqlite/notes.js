import {open} from "noe:sqlite";

const path = process.argv[2] ?? ":memory:";
await using db = await open(path);
await db.exec("CREATE TABLE IF NOT EXISTS notes(id INTEGER PRIMARY KEY, text TEXT NOT NULL)");

const inserted = await db.transaction(async tx => {
  const statement = await tx.prepare("INSERT INTO notes(text) VALUES(?)");
  const first = await statement.run(["hello"]);
  const second = await statement.run(["日本語"]);
  return [first.lastInsertRowid, second.lastInsertRowid];
});

const notes = await db.all("SELECT id,text FROM notes ORDER BY id");
console.log(JSON.stringify({inserted, notes}));
