// ./noe run examples/collect-json.js examples/data/numbers
if (noe.args.length !== 1) {
  console.error("Usage: noe run examples/collect-json.js <directory>");
  noe.exit(2);
}

const directory = noe.args[0];
const entries = await noe.readDir(directory);
const files = await Promise.all(entries
  .filter(entry => entry.isFile && entry.name.endsWith(".json"))
  .map(async entry => {
    const path = `${directory}/${entry.name}`;
    const [text, info] = await Promise.all([noe.readTextFile(path), noe.stat(path)]);
    const values = JSON.parse(text);
    if (!Array.isArray(values) || !values.every(value => typeof value === "number" && Number.isFinite(value))) {
      throw new TypeError(`${entry.name} must contain a JSON array of finite numbers`);
    }
    return {
      name: entry.name,
      bytes: info.size,
      count: values.length,
      total: values.reduce((sum, value) => sum + value, 0),
    };
  }));

const count = files.reduce((sum, file) => sum + file.count, 0);
const total = files.reduce((sum, file) => sum + file.total, 0);
console.log(JSON.stringify({ files, count, total, average: count === 0 ? null : total / count }));
