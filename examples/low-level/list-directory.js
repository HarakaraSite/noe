import {iterateDir} from "noe:fs";

const path = process.argv[2] ?? ".";
await using entries = iterateDir(path, {batchSize: 64});
for await (const entry of entries) {
  const kind = entry.isDirectory() ? "directory" : entry.isSymbolicLink() ? "symlink" : "file";
  console.log(`${kind}\t${entry.name}`);
}
