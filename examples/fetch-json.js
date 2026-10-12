if (noe.args.length !== 2) {
  console.error("Usage: noe run examples/fetch-json.js URL output.json");
  noe.exit(2);
}

const response = await fetch(noe.args[0]);
if (!response.ok) {
  throw new Error("HTTP " + response.status);
}
const data = await response.json();
await noe.writeTextFile(noe.args[1], JSON.stringify(data, null, 2) + "\n");
console.log("Saved", noe.args[1]);
