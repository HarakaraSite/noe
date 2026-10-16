import { lstat, mkdir, readFile, readdir, rename, rm, writeFile } from "fs/promises";
import { join } from "path";

// Copy inputs into a work directory first: this example moves processed files.
// ./noe run examples/process-json.js <input-directory> <output-directory>
const args = process.argv.slice(2);
if (args.length !== 2) {
  console.error("Usage: noe run examples/process-json.js <input-directory> <output-directory>");
  process.exit(2);
}

const [inputDirectory, outputDirectory] = args;
const names = await readdir(inputDirectory);
const files = (await Promise.all(names
  .filter(name => name.endsWith(".json"))
  .map(async name => {
    const path = join(inputDirectory, name);
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
const report = { files, count, total, average: count === 0 ? null : total / count };

await mkdir(join(outputDirectory, "processed"), { recursive: true });
const marker = join(outputDirectory, ".processing");
await writeFile(marker, "NOE processing\n");
await writeFile(join(outputDirectory, "report.json.tmp"), JSON.stringify(report, null, 2) + "\n");
await rename(join(outputDirectory, "report.json.tmp"), join(outputDirectory, "report.json"));
for (const file of files) {
  await rename(join(inputDirectory, file.name), join(outputDirectory, "processed", file.name));
}
await rm(marker);
console.log(JSON.stringify(report));
