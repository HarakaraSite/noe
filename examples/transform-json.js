import { transform } from "./lib/transform.js";

if (noe.args.length !== 2) {
  console.error("Usage: noe run examples/transform-json.js input.json output.json");
  noe.exit(2);
}

const data = JSON.parse(await noe.readTextFile(noe.args[0]));
await noe.writeTextFile(noe.args[1], JSON.stringify(transform(data), null, 2) + "\n");
console.log("Saved", noe.args[1]);
