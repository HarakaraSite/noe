// Run: ./noe run .noe/lib/find/test.js (fd, GNU find, and ln required).
import {mkdtempSync, writeFileSync, mkdirSync, rmSync, copyFileSync, chmodSync, existsSync} from "node:fs";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import process from "node:process";
import {execFile} from "node:child_process";
import {find} from "./find.js";

const root = mkdtempSync(join(tmpdir(), "noe-find-"));
let checks = 0;
function check(ok, label) {
  if (!ok) throw new Error(label);
  checks++;
}
function same(actual, expected) {
  return JSON.stringify([...actual].sort()) === JSON.stringify([...expected].sort());
}
async function rejects(operation, label, backend) {
  let error;
  try { await operation(); } catch (caught) { error = caught; }
  check(error && (!backend || error.backend === backend), label);
}
try {
  const files = join(root, "-files with spaces");
  mkdirSync(files);
  for (const dir of ["url-import", "ignored-dir", ".git"]) mkdirSync(join(files, dir));
  const odd = 'odd:name\n"日本語😀.js';
  const expectedJS = ["a.js", "ignored.js", ".hidden.js", "-flag.js", odd,
    "url-import/main.js", "ignored-dir/build.js"];
  for (const name of [...expectedJS, "A.JS", ".git/config.js"]) writeFileSync(join(files, name), "");
  writeFileSync(join(files, ".gitignore"), "ignored.js\nignored-dir/\n");
  writeFileSync(join(files, ".ignore"), "a.js\n");
  writeFileSync(join(files, ".fdignore"), "-flag.js\n");
  const outside = join(root, "outside");
  mkdirSync(outside);
  writeFileSync(join(outside, "secret.js"), "");
  await execFile("ln", ["-s", outside, join(files, "linked-dir")]);
  await execFile("ln", ["-s", join(files, "a.js"), join(files, "linked-file")]);

  for (const backend of ["fd", "find"]) {
    const run = options => find(files, {backend, ...options});
    const js = await run({name: "*.js", type: "file"});
    check(js.backend === backend && js.path === files && js.complete && js.limitedBy === null, `${backend}: result metadata`);
    check(same(js.results, expectedJS), `${backend}: basename glob, hidden/ignored files, .git exclusion, and no symlink descent`);
    check(same((await run({name: "*import*", type: "directory"})).results, ["url-import"]), `${backend}: directory names`);
    check(same((await run({name: "*.js", type: "file", maxDepth: 1})).results, expectedJS.filter(name => !name.includes("/"))), `${backend}: maxDepth`);
    check(same((await run({name: "*.js", type: "file", ignoreCase: true})).results, [...expectedJS, "A.JS"]), `${backend}: explicit case-insensitive glob`);
    check(same((await run({name: "-flag.js"})).results, ["-flag.js"]), `${backend}: leading dash is not an option`);
    check(same((await run({name: "[a].?s"})).results, ["a.js"]), `${backend}: character class and question mark`);
    check(same((await run({type: "symlink"})).results, ["linked-dir", "linked-file"]), `${backend}: symlinks themselves are searchable`);
    const all = await run({});
    check(all.complete && !all.results.some(name => name === ".git" || name.startsWith(".git/")) && all.results.includes(odd), `${backend}: all types, NUL framing, relative paths`);
    const none = await run({name: "no-match"});
    check(none.complete && none.results.length === 0, `${backend}: no matches`);
    const exact = await run({name: "*.js", type: "file", maxResults: expectedJS.length});
    check(exact.complete && same(exact.results, expectedJS), `${backend}: exact result limit is complete`);
    const limited = await run({name: "*.js", maxResults: 1});
    check(!limited.complete && limited.limitedBy === "maxResults" && limited.results.length === 1, `${backend}: count limit`);
    const bounded = await run({maxBytes: 256});
    check(!bounded.complete && bounded.limitedBy === "maxBytes", `${backend}: byte limit`);
    check(new TextEncoder().encode(JSON.stringify(bounded)).length <= 256 && bounded.results.every(name => !name.includes("�")), `${backend}: UTF-8 and escaped JSON fit cap`);
    await rejects(() => run({maxBytes: 1}), `${backend}: metadata budget`);
  }
  const auto = await find(files, {name: "*import*", type: "directory"});
  check(auto.backend === "fd" && same(auto.results, ["url-import"]), "auto prefers fd");

  // An actual missing executable, not just a forced backend.
  const bin = join(root, "bin");
  mkdirSync(bin);
  const findPath = process.env.PATH.split(":").map(dir => join(dir, "find")).find(path => existsSync(path));
  copyFileSync(findPath, join(bin, "find"));
  chmodSync(join(bin, "find"), 0o755);
  const library = resolve(".noe/lib/find/find.js");
  const code = `import {find} from ${JSON.stringify(library)}; console.log(JSON.stringify(await find(${JSON.stringify(files)},{name:"*.js",type:"file"})));`;
  const fallback = JSON.parse((await execFile(resolve(process.argv[0]), ["eval", code], {env: {PATH: bin}})).stdout);
  check(fallback.backend === "find" && fallback.complete && same(fallback.results, expectedJS), "automatic fallback with missing fd");

  // An installed fd reporting an error must NOT be hidden by the fallback.
  writeFileSync(join(bin, "fd"), "#!/bin/sh\necho deliberate-fd-failure >&2\nexit 2\n");
  chmodSync(join(bin, "fd"), 0o755);
  const errorCode = `import {find} from ${JSON.stringify(library)}; try { await find(${JSON.stringify(files)}); throw new Error("unexpected success"); } catch(e) { console.log(JSON.stringify({backend:e.backend,exitCode:e.exitCode,message:e.message})); }`;
  const failure = JSON.parse((await execFile(resolve(process.argv[0]), ["eval", errorCode], {env: {PATH: bin}})).stdout);
  check(failure.backend === "fd" && failure.exitCode === 2 && failure.message.includes("deliberate-fd-failure"), "fd search errors are not fallback triggers");

  await rejects(() => find(join(root, "missing")), "missing directory throws");
  await rejects(() => find(join(files, "a.js")), "file root rejected");
  await rejects(() => find(join(files, "linked-dir")), "symlink root rejected");
  for (const options of [{name: "**/*.js"}, {name: "a/b"}, {name: "{a,b}"}, {name: "a\0b"},
    {maxResults: 0}, {maxDepth: 0}, {type: "unknown"}, {type: ["file"]}, {ignoreCase: "yes"}, {backend: "unknown"}]) {
    await rejects(() => find(files, options), `invalid options: ${JSON.stringify(options)}`);
  }
  console.log(`PASS: ${checks} checks`);
} finally {
  rmSync(root, {recursive: true, force: true});
}
