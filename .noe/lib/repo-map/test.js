import {mkdtempSync, mkdirSync, writeFileSync, rmSync} from "node:fs";
import {tmpdir} from "node:os";
import {join, resolve} from "node:path";
import process from "node:process";
import {execFile} from "node:child_process";
import {repoMap} from "./repo-map.js";

const root = mkdtempSync(join(tmpdir(), "noe-repo-map-"));
let checks = 0;
function check(ok, label) { if (!ok) throw new Error(label); checks++; }
async function rejects(operation, label) {
  let rejected = false;
  try { await operation(); } catch { rejected = true; }
  check(rejected, label);
}
try {
  const project = join(root, "project");
  mkdirSync(project);
  for (const name of ["a", "b", "c"]) {
    mkdirSync(join(project, name));
    for (let i = 0; i < 6; i++) writeFileSync(join(project, name, `file-${i}.custom`), "content is not parsed\n");
  }
  writeFileSync(join(project, "README.md"), "# Arbitrary language\n");
  const plain = await repoMap({path: project});
  check(plain.backend === "fs" && plain.files === 19 && plain.directories === 3 && plain.complete, "non-Git filesystem fallback and counts");
  check(plain.warnings.length === 1 && plain.tree.includes("file-5.custom"), "filesystem behavior is explicit; no language assumption");
  const shallow = await repoMap({path: project, depth: 1});
  check(shallow.complete && shallow.tree.includes("a/ (6 files; collapsed)") && !shallow.tree.includes("file-0"), "depth collapses intentionally without claiming a budget overflow");
  const zoom = await repoMap({path: join(project, "b"), depth: 1});
  check(zoom.files === 6 && zoom.tree.includes("file-0.custom") && !zoom.tree.includes("README.md"), "subdirectory zoom");
  const balanced = await repoMap({path: project, maxEntries: 3});
  check(!balanced.complete && balanced.limitedBy.includes("maxEntries") && ["a/", "b/", "c/"].every(name => balanced.tree.includes(name)) && !balanced.tree.includes("file-0"), "breadth-first budget preserves top-level branches");
  const bounded = await repoMap({path: project, maxBytes: 600});
  check(!bounded.complete && bounded.limitedBy.includes("maxBytes") && new TextEncoder().encode(JSON.stringify(bounded)).length <= 600, "full JSON byte budget");
  const partial = await repoMap({path: project, maxFiles: 2});
  check(partial.files === 2 && !partial.scanComplete && !partial.complete && partial.limitedBy.includes("maxFiles") && partial.tree.includes("2+ files"), "scan limit reports lower-bound counts");
  writeFileSync(join(project, "c", '日本語😀"\\name\nfile'), "");
  const escaped = await repoMap({path: join(project, "c")});
  check(escaped.tree.includes('\\nfile') && escaped.tree.includes("日本語😀"), "unusual filenames do not inject tree lines");
  await execFile("git", ["init", "-q", project]);
  writeFileSync(join(project, ".gitignore"), "ignored.txt\n");
  writeFileSync(join(project, "ignored.txt"), "ignored\n");
  writeFileSync(join(project, "deleted.txt"), "deleted\n");
  await execFile("git", ["-C", project, "add", "."]);
  rmSync(join(project, "deleted.txt"));
  writeFileSync(join(project, "untracked.txt"), "untracked\n");
  const git = await repoMap({path: project, depth: 2});
  check(git.backend === "git" && git.complete && git.files === 22 && !git.tree.includes("ignored.txt") && !git.tree.includes("deleted.txt") && git.tree.includes("untracked.txt") && !git.tree.includes(".git/"), "Git ignores, untracked files, and tracked deletions");
  const subtree = await repoMap({path: join(project, "a")});
  check(subtree.backend === "git" && subtree.files === 6 && !subtree.tree.includes("b/"), "Git search stays within requested root");
  const gitLimit = await repoMap({path: project, maxFiles: 2});
  check(gitLimit.backend === "git" && !gitLimit.scanComplete && gitLimit.files === 2, "Git stream stops at scan cap");
  // Missing executable fallback, using an empty PATH in a real child NOE.
  const emptyBin = join(root, "empty-bin");
  mkdirSync(emptyBin);
  const library = resolve(".noe/lib/repo-map/repo-map.js");
  const code = `import {repoMap} from ${JSON.stringify(library)};console.log(JSON.stringify(await repoMap({path:${JSON.stringify(join(project, "a"))}})));`;
  const fallback = JSON.parse((await execFile(resolve(process.argv[0]), ["eval", code], {env: {PATH: emptyBin}})).stdout);
  check(fallback.backend === "fs" && fallback.files === 6 && fallback.complete, "missing Git executable fallback");
  await rejects(() => repoMap({path: project, maxBytes: 1}), "budget must fit root metadata");
  await rejects(() => repoMap({path: project, depth: -1}), "invalid depth");
  await rejects(() => repoMap({path: join(project, "README.md")}), "file root is rejected");
  console.log(`PASS: ${checks} checks`);
} finally { rmSync(root, {recursive: true, force: true}); }
