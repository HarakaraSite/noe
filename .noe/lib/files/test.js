// Run with NOE; tests exercise real filesystem reads and directory traversal.
import {mkdtempSync, mkdirSync, writeFileSync, rmSync} from "node:fs";
import {readdir} from "node:fs/promises";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import {execFile} from "node:child_process";
import {ls, read_file, write_file, edit_file} from "./files.js";

const root = mkdtempSync(join(tmpdir(), "noe-files-"));
let checks = 0;
function check(ok, label) {
  if (!ok) throw new Error(label);
  checks++;
}
async function rejects(operation, label) {
  let error;
  try { await operation(); } catch (caught) { error = caught; }
  check(error, label);
}
try {
  mkdirSync(join(root, "dir"));
  mkdirSync(join(root, "dir", "nested"));
  mkdirSync(join(root, "empty"));
  writeFileSync(join(root, ".hidden"), "secret");
  writeFileSync(join(root, ".gitignore"), "ignored.txt\n");
  writeFileSync(join(root, "ignored.txt"), "ignored");
  const content = "one\r\n\n日本語😀\nlast";
  writeFileSync(join(root, "a.txt"), content);
  writeFileSync(join(root, "dir", "b.txt"), "b\n");
  writeFileSync(join(root, "dir", "nested", "c.txt"), "c\n");
  // A cycle would recurse indefinitely if directory symlinks were followed.
  await execFile("ln", ["-s", root, join(root, "dir", "back")]);

  const direct = await ls(root);
  check(direct.path === root && direct.total === 6 && direct.entries.every(row => row.depth === 1), "default only reads direct children");
  check(direct.entries.map(row => row.path).join(",") === ".gitignore,.hidden,a.txt,dir,empty,ignored.txt", "sorted, hidden, ignored, and empty entries");
  check(direct.entries.find(row => row.path === "a.txt").bytes === new TextEncoder().encode(content).length, "file byte size");
  check((await ls()).path === resolve(".") && (await ls()).total === (await readdir(".")).length, "default path is cwd");
  const tree = await ls(root, {tree: true, depth: 2});
  check(tree.total === 9 && !tree.entries.some(row => row.path.endsWith("c.txt")), "tree depth bounds traversal");
  check(tree.entries.find(row => row.path === join("dir", "back")).type === "symlink", "symlink is listed without following cycle");
  check((await ls(root, {tree: true, depth: 3})).total === 10, "deeper tree reads requested level");
  let offset = 0;
  const pages = [];
  do {
    const page = await ls(root, {tree: true, depth: 2, offset, limit: 2});
    pages.push(...page.entries);
    offset = page.nextOffset;
  } while (offset !== undefined);
  check(JSON.stringify(pages) === JSON.stringify(tree.entries), "pages reconstruct tree without skips or duplicates");
  const empty = await ls(join(root, "empty"));
  check(empty.total === 0 && empty.entries.length === 0 && empty.nextOffset === undefined, "empty directory");
  check((await ls(root, {offset: 999})).entries.length === 0, "directory offset beyond EOF");

  const pagedRoot = join(root, "paged");
  mkdirSync(pagedRoot);
  for (let i = 0; i < 201; i++) writeFileSync(join(pagedRoot, `${String(i).padStart(3, "0")}.txt`), "");
  const defaultPage = await ls(pagedRoot);
  check(defaultPage.entries.length === 200 && defaultPage.total === 201 && defaultPage.nextOffset === 200, "default page size is 200");
  const lastPage = await ls(pagedRoot, {offset: defaultPage.nextOffset});
  check(lastPage.entries.length === 1 && lastPage.entries[0].path === "200.txt" && lastPage.nextOffset === undefined, "default pagination retrieves remaining entries");
  check((await ls(pagedRoot, {limit: 100})).entries.length === 100, "explicit limit overrides new default");

  const full = await read_file(join(root, "a.txt"));
  check(full.text === content && full.lines === 4 && full.totalLines === 4 && full.nextOffset === undefined, "full read preserves CRLF and missing final newline");
  const first = await read_file(join(root, "a.txt"), {limit: 2});
  const rest = await read_file(join(root, "a.txt"), {offset: first.nextOffset});
  check(first.text === "one\r\n\n" && first.nextOffset === 3 && rest.offset === 3 && rest.lines === 2 && rest.totalLines === 4 && first.text + rest.text === content, "line paging reconstructs file");
  const beyond = await read_file(join(root, "a.txt"), {offset: 99, limit: 2});
  check(beyond.text === "" && beyond.lines === 0 && beyond.nextOffset === undefined, "line offset beyond EOF");
  for (const [text, count] of [["", 0], ["\n", 1], ["one\n", 1], ["one\n\n", 2]]) {
    writeFileSync(join(root, "line.txt"), text);
    const result = await read_file(join(root, "line.txt"));
    check(result.text === text && result.totalLines === count, "empty file and final-newline line counts");
  }
  const large = "日本語😀\n".repeat(2000);
  writeFileSync(join(root, "large.txt"), large);
  check((await read_file(join(root, "large.txt"))).text === large, "full read has no legacy 10 KiB cap");
  await rejects(() => read_file(join(root, "missing")), "missing file throws");
  await rejects(() => ls(join(root, "a.txt")), "listing file throws");
  await rejects(() => read_file(join(root, "a.txt"), {offset: 0}), "line offsets start at one");
  await rejects(() => read_file(join(root, "a.txt"), {limit: 1.5}), "fractional line limit rejected");
  await rejects(() => ls(root, {limit: 0}), "empty page cannot conceal continuation");
  await rejects(() => ls(root, {offset: -1}), "negative page offset rejected");

  const target = join(root, "new", "deep", "edit.txt");
  const created = await write_file(target, "日本語\r\nalpha\r\nend");
  check(created.bytes === new TextEncoder().encode("日本語\r\nalpha\r\nend").length && (await read_file(target)).text === "日本語\r\nalpha\r\nend", "write creates parents and reports UTF-8 bytes");
  const changed = await edit_file(target, "alpha", "beta\nnew");
  check(changed.replacements === 1 && (await read_file(target)).text === "日本語\r\nbeta\nnew\r\nend", "edit changes only exact target and preserves surrounding line endings");
  await edit_file(target, "beta\nnew", "");
  check((await read_file(target)).text === "日本語\r\n\r\nend", "edit supports deletion");
  await write_file(target, "same same\n");
  await rejects(() => edit_file(target, "same", "other"), "ambiguous edit rejected");
  await rejects(() => edit_file(target, "missing", "other"), "missing target rejected");
  await rejects(() => edit_file(target, "", "other"), "empty target rejected");
  check((await read_file(target)).text === "same same\n", "failed edits leave file unchanged");
  const all = await edit_file(target, "same", "$&\\literal", {all: true});
  check(all.replacements === 2 && (await read_file(target)).text === "$&\\literal $&\\literal\n", "explicit all uses literal replacement strings");
  await write_file(target, "");
  check((await read_file(target)).text === "", "write overwrites and supports empty file");
  await rejects(() => edit_file(join(root, "missing"), "old", "new"), "edit does not create missing files");
  console.log(`PASS: ${checks} checks`);
} finally {
  rmSync(root, {recursive: true, force: true});
}
