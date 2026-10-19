package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestStdlibFilesystemPromises(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "before.txt")
	if err := os.WriteFile(file, []byte("日本語"), 0600); err != nil {
		t.Fatal(err)
	}

	source := `import fs from "node:fs";
import {mkdir, readdir, rename, rm, stat} from "node:fs/promises";
const root = ` + jsString(t, dir) + `;
const before = ` + jsString(t, file) + `;
const after = ` + jsString(t, filepath.Join(dir, "after.txt")) + `;
const created = ` + jsString(t, filepath.Join(dir, "created", "nested")) + `;
await mkdir(created, {recursive:true});
const names = await readdir(root);
if (!names.includes("before.txt") || !names.includes("created")) throw Error("readdir");
const info = await stat(before);
if (!info.isFile() || info.size !== 9 || fs.statSync(before).size !== 9) throw Error("stat");
await rename(before, after);
if (await fs.promises.readFile(after, "utf8") !== "日本語") throw Error("rename");
await rm(after);
await rm(` + jsString(t, filepath.Join(dir, "created")) + `, {recursive:true});
if (fs.existsSync(before) || fs.existsSync(after) || fs.existsSync(` + jsString(t, filepath.Join(dir, "created")) + `)) throw Error("remove");
console.log("filesystem ready");`
	if out := runStdlib(t, source, Options{Cwd: dir}); out != "filesystem ready\n" {
		t.Fatalf("output=%q", out)
	}
}

func jsString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}
