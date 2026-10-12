import { summarizeSales, formatReport } from "./lib/sales.js";

const outputPrefix = noe.args[0] ?? "/tmp/noe-sales-report";
const inputFiles = noe.args.length > 1 ? noe.args.slice(1) : [
  "examples/data/sales-day-1.json",
  "examples/data/sales-day-2.json",
];
const title = noe.env.get("REPORT_TITLE") ?? "NOE sample sales report";

// Read independent files concurrently, then combine their orders.
const batches = await Promise.all(inputFiles.map(async file => {
  const orders = JSON.parse(await noe.readTextFile(file));
  console.log(`Loaded ${file}: ${orders.length} orders`);
  return orders;
}));

const report = summarizeSales(batches.flat());
const markdown = formatReport(title, report);

await Promise.all([
  noe.writeTextFile(outputPrefix + ".json", JSON.stringify({ title, ...report }, null, 2) + "\n"),
  noe.writeTextFile(outputPrefix + ".md", markdown),
]);

console.log("\n" + markdown);
console.log("Saved", outputPrefix + ".json", "and", outputPrefix + ".md");
