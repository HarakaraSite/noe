// Run with NOE. Exercises real rg/grep output and automatic missing-rg fallback.
import {mkdtempSync, writeFileSync, mkdirSync, rmSync, copyFileSync, chmodSync, existsSync} from "node:fs";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import process from "node:process";
import {execFile} from "node:child_process";
import {grep} from "./search.js";

const root = mkdtempSync(join(tmpdir(), "noe-search-"));
let checks = 0;
function check(ok, label) {
  if (!ok) throw new Error(label);
  checks++;
}
async function rejects(operation, label, backend) {
  let error;
  try { await operation(); } catch (caught) { error = caught; }
  check(error && (!backend || error.backend === backend), label);
}
try {
  const files = join(root, "files");
  mkdirSync(files);
  writeFileSync(join(files, "a.js"), 'alpha alpha\nbeta\nALPHA\nquotes " \\ 日本語 😀\na.b\naxb\n-e\n');
  writeFileSync(join(files, "b.txt"), "alpha\n");
  writeFileSync(join(files, "odd:name\nfile.js"), "alpha\n");
  for (const backend of ["rg", "grep"]) {
    const run = options => grep({paths: [files], backend, pattern: "alpha", ...options});
    const lines = await run({glob: ["a.js"]});
    check(lines.complete && lines.results.length === 1 && lines.results[0].line === 1 && lines.results[0].text === "alpha alpha", `${backend}: line format`);
    check((await run({pattern: "a.b", regex: false, glob: ["a.js"]})).results.length === 1, `${backend}: explicit literal`);
    check((await run({pattern: "a.b", glob: ["a.js"]})).results.length === 2, `${backend}: default regex`);
    check((await run({pattern: "alpha|beta", glob: ["a.js"]})).results.length === 2, `${backend}: alternation without regex option`);
    check((await run({pattern: "alpha|beta", regex: false, glob: ["a.js"]})).results.length === 0, `${backend}: explicit literal keeps pipe literal`);
    check((await run({ignoreCase: true, glob: ["a.js"]})).results.length === 2, `${backend}: ignoreCase`);
    check((await run({pattern: "-e", glob: ["a.js"]})).results[0].text === "-e", `${backend}: dash pattern`);
    check((await run({mode: "files"})).results.length === 3, `${backend}: filenames including newline/colon`);
    const odd = (await run({})).results.find(row => row.path.endsWith("odd:name\nfile.js"));
    check(odd?.line === 1 && odd.text === "alpha", `${backend}: line result with newline/colon in filename`);
    check((await run({mode: "files", glob: ["*.js", "!a.js"]})).results.length === 1, `${backend}: glob exclusion`);
    const counts = await run({mode: "count"});
    check(counts.results.length === 3 && counts.results.every(row => row.count === 1), `${backend}: count lines, not occurrences`);
    const none = await run({pattern: "no-match"});
    check(none.complete && none.results.length === 0, `${backend}: no-match is not error`);
    const exact = await run({mode: "files", maxResults: 3});
    check(exact.complete && exact.results.length === 3, `${backend}: exact limit is complete`);
    const limited = await run({mode: "files", maxResults: 1});
    check(!limited.complete && limited.limitedBy === "maxResults" && limited.results.length === 1, `${backend}: global result limit`);
    await rejects(() => run({pattern: "["}), `${backend}: invalid default regex throws without backend switch`, backend);
    await rejects(() => run({paths: [join(root, "does-not-exist")]}), `${backend}: missing path throws`, backend);
    const long = join(root, "long.txt");
    writeFileSync(long, "日本語😀\"\\".repeat(20000) + "\n");
    const bounded = await run({pattern: "日本語", paths: [long], maxBytes: 650});
    check(!bounded.complete && bounded.limitedBy === "maxBytes" && bounded.results[0]?.textTruncated === true && bounded.results[0].line === 1, `${backend}: long line retains location`);
    check(new TextEncoder().encode(JSON.stringify(bounded)).length <= 650 && !bounded.results[0].text.includes("�"), `${backend}: escaped UTF-8 JSON fits byte cap`);
    const binary = join(root, "binary.dat");
    writeFileSync(binary, "alpha\0alpha\n");
    const binaryResult = await run({paths: [binary]});
    check(binaryResult.complete && binaryResult.results.length === 0, `${backend}: binary notice is not a text line`);
  }
  // Actual exec lookup failure, not a forced backend or simulated rg exit.
  const bin = join(root, "bin");
  mkdirSync(bin);
  const grepPath = process.env.PATH.split(":").map(dir => join(dir, "grep")).find(path => existsSync(path));
  copyFileSync(grepPath, join(bin, "grep"));
  chmodSync(join(bin, "grep"), 0o755);
  const library = resolve(".noe/lib/search/search.js");
  const code = `import {grep} from ${JSON.stringify(library)}; console.log(JSON.stringify(await grep({pattern:"alpha|beta",paths:[${JSON.stringify(files)}],mode:"files"})));`;
  const fallback = JSON.parse((await execFile(resolve(process.argv[0]), ["eval", code], {env: {PATH: bin}})).stdout);
  check(fallback.backend === "grep" && fallback.complete && fallback.results.length === 3 && fallback.warnings.length === 1, "automatic fallback with missing rg");
  const zero = await grep({pattern: "no-match", paths: [files], mode: "count", backend: "grep"});
  check(zero.complete && zero.results.length === 0, "grep zero-count records omitted");
  await rejects(() => grep({pattern: "x", maxBytes: 1}), "insufficient metadata budget");
  console.log(`PASS: ${checks} checks`);
} finally {
  rmSync(root, {recursive: true, force: true});
}
