import { lstat, readFile, readdir } from "fs/promises";
import { join } from "path";

// ./noe run examples/json/collect-json.js examples/json/data/numbers
const args = process.argv.slice(2);
if (args.length !== 1) {
  console.error("Usage: noe run examples/json/collect-json.js <directory>");
  process.exit(2);
}

const directory = args[0];
const names = await readdir(directory);
const files = (await Promise.all(names
  .filter(name => name.endsWith(".json"))
  .map(async name => {
    const path = join(directory, name);
    const info = await lstat(path);
    if (!info.isFile()) return null;
    const text = await readFile(path, "utf8");
    const values = JSON.parse(text);
    if (!Array.isArray(values) || !values.every(value => typeof value === "number" && Number.isFinite(value))) {
      throw new TypeError(`${name} must contain a JSON array of finite numbers`);
    }
    return {
      name,
      bytes: info.size,
      count: values.length,
      total: values.reduce((sum, value) => sum + value, 0),
    };
  }))).filter(file => file !== null);

const count = files.reduce((sum, file) => sum + file.count, 0);
const total = files.reduce((sum, file) => sum + file.total, 0);
console.log(JSON.stringify({ files, count, total, average: count === 0 ? null : total / count }));
