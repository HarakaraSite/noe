import { summarizeSales, formatReport } from "./lib/sales.js";
import { readFile, writeFile } from "fs/promises";

const args = process.argv.slice(2);
const outputPrefix = args[0] ?? "/tmp/noe-sales-report";
const inputFiles = args.length > 1 ? args.slice(1) : [
  "examples/data/sales-day-1.json",
  "examples/data/sales-day-2.json",
];
const title = process.env.REPORT_TITLE ?? "NOE sample sales report";

// Read independent files concurrently, then combine their orders.
const batches = await Promise.all(inputFiles.map(async file => {
  const orders = JSON.parse(await readFile(file, "utf8"));
  console.log(`Loaded ${file}: ${orders.length} orders`);
  return orders;
}));

const report = summarizeSales(batches.flat());
const markdown = formatReport(title, report);

await Promise.all([
  writeFile(outputPrefix + ".json", JSON.stringify({ title, ...report }, null, 2) + "\n"),
  writeFile(outputPrefix + ".md", markdown),
]);

console.log("\n" + markdown);
console.log("Saved", outputPrefix + ".json", "and", outputPrefix + ".md");
