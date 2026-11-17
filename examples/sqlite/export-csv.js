import {open as openFile} from "noe:fs";
import {open} from "noe:sqlite";

const [databasePath, outputPath] = process.argv.slice(2);
if (!databasePath || !outputPath) {
  throw new Error("Usage: noe run examples/sqlite/export-csv.js DATABASE OUTPUT.csv");
}

await using db = await open(databasePath, {readOnly: true});
await using output = await openFile(outputPath, {flags: "w"});
const csv = value => `"${String(value).replaceAll('"', '""')}"`;
await output.write("id,text\r\n");
let count = 0;
for await (const note of db.iterate("SELECT id,text FROM notes ORDER BY id", [], {batchSize: 64})) {
  await output.write([note.id, note.text].map(csv).join(",") + "\r\n");
  count++;
}
console.log(`Exported ${count} notes to ${outputPath}`);
