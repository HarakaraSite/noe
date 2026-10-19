import { fetch } from "noe:http";
import { writeFile } from "fs/promises";

// ./noe run --timeout 10s examples/http/download-file.js URL output-file
const args = process.argv.slice(2);
if (args.length !== 2) {
  console.error("Usage: noe run examples/http/download-file.js URL output-file");
  process.exit(2);
}

const response = await fetch(args[0]);
if (!response.ok) {
  throw new Error(`HTTP ${response.status}`);
}
const bytes = await response.bytes();
await writeFile(args[1], bytes);
console.log("Saved", bytes.length, "bytes to", args[1]);
