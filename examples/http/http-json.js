// Use a JSON API URL of your choice; PUT/PATCH/DELETE update remote data.
// ./noe run --timeout 5s examples/http/http-json.js METHOD URL [JSON-body]
const args = process.argv.slice(2);
if (args.length < 2 || args.length > 3) {
  console.error("Usage: noe run examples/http/http-json.js METHOD URL [JSON-body]");
  process.exit(2);
}

const [method, url, input] = args;
const options = { method };
if (input !== undefined) {
  options.body = JSON.stringify(JSON.parse(input));
  options.headers = { "Content-Type": "application/json" };
}

const response = await fetch(url, options);
const text = await response.text();
if (!response.ok) {
  throw new Error(`HTTP ${response.status}: ${text}`);
}
const body = text === "" ? null : JSON.parse(text);
console.log(JSON.stringify({ status: response.status, ok: response.ok, body }));
