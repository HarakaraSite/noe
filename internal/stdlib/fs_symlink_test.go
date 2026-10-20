package stdlib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

// The same entries are reachable directly and through a linked parent. Link
// operations must preserve only the final component, not stop resolving parents.
func filesLinkFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "actual")
	for _, name := range []string{"target-dir", "empty-dir"} {
		if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, text := range map[string]string{
		"target.txt": "target", "other.txt": "other", "replacement.txt": "replacement",
		"target-dir/child.txt": "child",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, target := range map[string]string{
		"actual/file-link": "target.txt", "actual/dest-link": "other.txt",
		"actual/dir-link": "target-dir", "actual/empty-link": "empty-dir", "parent": "actual",
	} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	return root
}

func filesLinkContent(t *testing.T, path, want string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil || string(b) != want {
		t.Errorf("ReadFile(%s) = %q, %v; want %q", path, b, err, want)
	}
}

func filesLinkTarget(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.Readlink(path)
	if err != nil || got != want {
		t.Errorf("Readlink(%s) = %q, %v; want %q", path, got, err, want)
	}
}

func filesLinkMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Errorf("Lstat(%s) = %v; want not-exist", path, err)
	}
}

func filesLinkScript(module, path, code string) string {
	return fmt.Sprintf(`
		;(async () => {
			const fs = (await import(%q)).default
			const p = %q
			%s
			console.log("ok")
		})().catch(e => console.log("ERROR: " + e.message))
	`, module, filepath.ToSlash(path), code)
}

func TestFilesLinkEntries(t *testing.T) {
	cases := []struct {
		name, code, output string
		missing            []string
		links              map[string]string
		contents           map[string]string
	}{
		{name: "lstat", code: `console.log((await fs.lstat$(p + "/file-link")).isSymbolicLink())`, output: "true\n"},
		{name: "readlink", code: `console.log(await fs.readlink$(p + "/file-link"))`, output: "target.txt\n"},
		{name: "follow", code: `
			console.log((await fs.stat$(p + "/file-link")).isFile())
			console.log(await fs.readFile$(p + "/file-link", "utf8"))
			await fs.writeFile$(p + "/file-link", "updated")
		`, output: "true\ntarget\n", contents: map[string]string{"target.txt": "updated"}, links: map[string]string{"file-link": "target.txt"}},
		{name: "unlink", code: `await fs.unlink$(p + "/file-link")`, missing: []string{"file-link"}},
		{name: "rm", code: `await fs.rm$(p + "/file-link")`, missing: []string{"file-link"}},
		{name: "rm-recursive", code: `await fs.rm$(p + "/dir-link", {recursive: true})`, missing: []string{"dir-link"}},
		{name: "rmdir", code: `await fs.rmdir$(p + "/empty-link")`, missing: []string{"empty-link"}},
		{name: "rename-source", code: `await fs.rename$(p + "/file-link", p + "/moved-link")`,
			missing: []string{"file-link"}, links: map[string]string{"moved-link": "target.txt"}},
		{name: "rename-destination", code: `await fs.rename$(p + "/replacement.txt", p + "/dest-link")`,
			missing: []string{"replacement.txt"}, contents: map[string]string{"dest-link": "replacement"}},
		{name: "rename-both", code: `await fs.rename$(p + "/file-link", p + "/dest-link")`,
			missing: []string{"file-link"}, links: map[string]string{"dest-link": "target.txt"}},
	}
	for _, rooted := range []bool{true, false} {
		for _, parent := range []string{"actual", "parent"} {
			for _, api := range []string{"sync", "promises"} {
				for _, tc := range cases {
					t.Run(fmt.Sprintf("root=%t/%s/%s/%s", rooted, parent, api, tc.name), func(t *testing.T) {
						root := filesLinkFixture(t)
						cfg := &stdlib.FS{}
						path := filepath.Join(root, parent)
						if rooted {
							cfg.Root, path = root, "/"+parent
						}
						module, suffix := "fs", "Sync"
						if api == "promises" {
							module, suffix = "fs/promises", ""
						}
						out, _ := run(t, stdlib.Config{FS: cfg}, filesLinkScript(module, path, strings.ReplaceAll(tc.code, "$", suffix)))
						if want := tc.output + "ok"; out != want {
							t.Errorf("output = %q; want %q", out, want)
						}
						dir := filepath.Join(root, "actual")
						target := "target"
						if tc.name == "follow" {
							target = "updated"
						}
						filesLinkContent(t, filepath.Join(dir, "target.txt"), target)
						filesLinkContent(t, filepath.Join(dir, "other.txt"), "other")
						filesLinkContent(t, filepath.Join(dir, "target-dir/child.txt"), "child")
						if info, err := os.Stat(filepath.Join(dir, "empty-dir")); err != nil || !info.IsDir() {
							t.Errorf("empty target directory was not preserved: %v", err)
						}
						for _, name := range tc.missing {
							filesLinkMissing(t, filepath.Join(dir, name))
						}
						for name, target := range tc.links {
							filesLinkTarget(t, filepath.Join(dir, name), target)
						}
						for name, text := range tc.contents {
							filesLinkContent(t, filepath.Join(dir, name), text)
						}
						filesLinkTarget(t, filepath.Join(root, "parent"), "actual")
					})
				}
			}
		}
	}
}

func TestFilesLinkEntriesRefused(t *testing.T) {
	operations := []struct{ name, code string }{
		{"lstat", `await fs.lstat$(p)`},
		{"readlink", `await fs.readlink$(p)`},
		{"unlink", `await fs.unlink$(p)`},
		{"rm", `await fs.rm$(p)`},
		{"rm-recursive", `await fs.rm$(p, {recursive: true, force: true})`},
		{"rmdir", `await fs.rmdir$(p)`},
		{"rename-source", `await fs.rename$(p, "/actual/moved")`},
		{"rename-destination", `await fs.rename$("/actual/replacement.txt", p)`},
	}
	for _, readonly := range []bool{false, true} {
		for _, api := range []string{"sync", "promises"} {
			for _, tc := range operations {
				if readonly && (tc.name == "lstat" || tc.name == "readlink") {
					continue // Read-only must still permit the two non-mutating operations.
				}
				paths := []string{"/outside-file", "/outside-dir", "/outside-dir/secret.txt", "/outside-dir/back-link"}
				if readonly {
					paths = []string{"/actual/file-link", "/parent/file-link"}
				}
				for _, path := range paths {
					t.Run(fmt.Sprintf("readonly=%t/%s/%s%s", readonly, api, tc.name, path), func(t *testing.T) {
						root := filesLinkFixture(t)
						outside := t.TempDir()
						if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o644); err != nil {
							t.Fatal(err)
						}
						// Full-target validation alone must not let entry operations
						// escape through a parent and then point back into the root.
						backTarget := filepath.Join(root, "actual/target.txt")
						if err := os.Symlink(backTarget, filepath.Join(outside, "back-link")); err != nil {
							t.Skipf("symlinks unavailable: %v", err)
						}
						for name, target := range map[string]string{"outside-file": filepath.Join(outside, "secret.txt"), "outside-dir": outside} {
							if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
								t.Skipf("symlinks unavailable: %v", err)
							}
						}
						module, suffix := "fs", "Sync"
						if api == "promises" {
							module, suffix = "fs/promises", ""
						}
						out, _ := run(t, stdlib.Config{FS: &stdlib.FS{Root: root, ReadOnly: readonly}}, filesLinkScript(module, path, strings.ReplaceAll(tc.code, "$", suffix)))
						want := "outside the permitted root"
						if readonly {
							want = "read-only"
						}
						if !strings.HasPrefix(out, "ERROR: ") || !strings.Contains(out, want) {
							t.Errorf("output = %q; want refusal containing %q", out, want)
						}
						filesLinkTarget(t, filepath.Join(outside, "back-link"), backTarget)
						filesLinkContent(t, filepath.Join(outside, "secret.txt"), "secret")
						filesLinkContent(t, filepath.Join(root, "actual/target.txt"), "target")
						filesLinkContent(t, filepath.Join(root, "actual/replacement.txt"), "replacement")
						filesLinkTarget(t, filepath.Join(root, "actual/file-link"), "target.txt")
						filesLinkTarget(t, filepath.Join(root, "outside-file"), filepath.Join(outside, "secret.txt"))
						filesLinkTarget(t, filepath.Join(root, "outside-dir"), outside)
						filesLinkMissing(t, filepath.Join(root, "actual/moved"))
					})
				}
			}
		}
	}
}

func TestFilesLinkEntriesReadOnlyReads(t *testing.T) {
	for _, api := range []string{"sync", "promises"} {
		t.Run(api, func(t *testing.T) {
			root := filesLinkFixture(t)
			module, suffix := "fs", "Sync"
			if api == "promises" {
				module, suffix = "fs/promises", ""
			}
			// The configured root itself may also be a symbolic link.
			cfg := &stdlib.FS{Root: filepath.Join(root, "parent"), ReadOnly: true}
			code := "console.log((await fs.lstat$(p)).isSymbolicLink()); console.log(await fs.readlink$(p))"
			out, _ := run(t, stdlib.Config{FS: cfg}, filesLinkScript(module, "/file-link", strings.ReplaceAll(code, "$", suffix)))
			if want := "true\ntarget.txt\nok"; out != want {
				t.Errorf("output = %q; want %q", out, want)
			}
		})
	}
}
