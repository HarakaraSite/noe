import { fetch } from "noe:http";
import { writeFile } from "fs/promises";

const args = process.argv.slice(2);
if (args.length !== 2) {
  console.error("Usage: noe run examples/http/fetch-json.js URL output.json");
  process.exit(2);
}

const response = await fetch(args[0]);
if (!response.ok) {
  throw new Error("HTTP " + response.status);
}
const data = await response.json();
await writeFile(args[1], JSON.stringify(data, null, 2) + "\n");
console.log("Saved", args[1]);
