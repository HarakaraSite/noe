// Usage: noe run examples/log-analysis/compare.js [options] FILE...
import { readFileSync } from "node:fs";
import process from "node:process";
import { analyzeJSONL, toMarkdown } from "./analyze.js";

const usage = "Usage: noe run examples/log-analysis/compare.js [--format json|markdown] [--below 0..1] [--] FILE...";
try {
  let format = "markdown";
  let below = 0.5;
  let options = true;
  const files = [];
  const args = process.argv.slice(2);
  for (let index = 0; index < args.length; index++) {
    const arg = args[index];
    if (options && arg === "--") options = false;
    else if (options && arg === "--help") {
      console.log(usage);
      process.exit(0);
    } else if (options && arg === "--format") {
      format = args[++index];
      if (!["json", "markdown"].includes(format)) throw new Error("--format requires json or markdown");
    } else if (options && arg === "--below") {
      const value = args[++index];
      below = value?.trim() ? Number(value) : NaN;
      if (!Number.isFinite(below) || below < 0 || below > 1) throw new Error("--below requires a number from 0 to 1");
    } else if (options && arg.startsWith("-")) throw new Error(`Unknown option: ${arg}`);
    else files.push(arg);
  }
  if (!files.length) throw new Error("At least one JSONL file is required");
  const reports = files.map((file) => analyzeJSONL(readFileSync(file, "utf8"), file, { below }));
  console.log(format === "json" ? JSON.stringify(reports, null, 2) : toMarkdown(reports).trimEnd());
} catch (error) {
  console.error(`${error.message}\n${usage}`);
  process.exit(1);
}
