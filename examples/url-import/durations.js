// Run with: ./noe run examples/url-import/durations.js
// Official ESM build, pinned to a version; no npm install or bundling needed.
import { parse, format } from "https://cdn.jsdelivr.net/npm/@lukeed/ms@2.0.2/dist/index.mjs";

for (const input of ["5s", "2 minutes", "2h", "1 day"]) {
  const milliseconds = parse(input);
  console.log(
    `${input} => ${milliseconds} ms => ${format(milliseconds)} (${format(milliseconds, true)})`,
  );
}
