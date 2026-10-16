import { transform } from "./lib/transform.js";

import { readFile, writeFile } from "fs/promises";

const args = process.argv.slice(2);
if (args.length !== 2) {
  console.error("Usage: noe run examples/transform-json.js input.json output.json");
  process.exit(2);
}

const data = JSON.parse(await readFile(args[0], "utf8"));
await writeFile(args[1], JSON.stringify(transform(data), null, 2) + "\n");
console.log("Saved", args[1]);
