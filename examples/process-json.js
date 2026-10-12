// Copy inputs into a work directory first: this example moves processed files.
// ./noe run examples/process-json.js <input-directory> <output-directory>
if (noe.args.length !== 2) {
  console.error("Usage: noe run examples/process-json.js <input-directory> <output-directory>");
  noe.exit(2);
}

const [inputDirectory, outputDirectory] = noe.args;
const entries = await noe.readDir(inputDirectory);
const files = await Promise.all(entries
  .filter(entry => entry.isFile && entry.name.endsWith(".json"))
  .map(async entry => {
    const path = `${inputDirectory}/${entry.name}`;
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
const report = { files, count, total, average: count === 0 ? null : total / count };

await noe.mkdir(`${outputDirectory}/processed`);
const marker = `${outputDirectory}/.processing`;
await noe.writeTextFile(marker, "NOE processing\n");
await noe.writeTextFile(`${outputDirectory}/report.json.tmp`, JSON.stringify(report, null, 2) + "\n");
await noe.rename(`${outputDirectory}/report.json.tmp`, `${outputDirectory}/report.json`);
for (const file of files) {
  await noe.rename(`${inputDirectory}/${file.name}`, `${outputDirectory}/processed/${file.name}`);
}
await noe.remove(marker);
console.log(JSON.stringify(report));
