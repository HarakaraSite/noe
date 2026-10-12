// ./noe run --timeout 10s examples/download-file.js URL output-file
if (noe.args.length !== 2) {
  console.error("Usage: noe run examples/download-file.js URL output-file");
  noe.exit(2);
}

const response = await fetch(noe.args[0]);
if (!response.ok) {
  throw new Error(`HTTP ${response.status}`);
}
const bytes = await response.bytes();
await noe.writeFile(noe.args[1], bytes);
console.log("Saved", bytes.length, "bytes to", noe.args[1]);
