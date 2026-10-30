package stdlib_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

var mkdtempAPIs = []struct{ name, module, call string }{
	{"sync", "node:fs", "fs.mkdtempSync(prefix)"},
	{"promise", "node:fs/promises", "await fs.mkdtemp(prefix)"},
}

func TestFilesMkdtempPrefix(t *testing.T) {
	for _, rooted := range []bool{false, true} {
		for _, api := range mkdtempAPIs {
			for _, name := range []string{"absolute", "relative", "bare", "trailing-separator", "empty", "dot"} {
				t.Run(fmt.Sprintf("root=%t/%s/%s", rooted, api.name, name), func(t *testing.T) {
					work := t.TempDir()
					cwd := filepath.Join(work, "workdir")
					if err := os.MkdirAll(filepath.Join(cwd, "nested"), 0o755); err != nil {
						t.Fatal(err)
					}
					cfg := &stdlib.FS{Cwd: cwd}
					scriptCwd := cwd
					if rooted {
						cfg.Root, cfg.Cwd = work, "/workdir"
						scriptCwd = "/workdir"
					}
					prefix, parent, base := "", scriptCwd, ""
					switch name {
					case "absolute":
						parent = filepath.Join(scriptCwd, "nested")
						prefix, base = filepath.Join(parent, "run-"), "run-"
					case "relative":
						prefix, parent, base = "nested/run-", filepath.Join(scriptCwd, "nested"), "run-"
					case "bare":
						prefix, base = "run-", "run-"
					case "trailing-separator":
						prefix, parent = "nested/", filepath.Join(scriptCwd, "nested")
					case "dot":
						prefix, base = ".", "."
					}
					out, _ := run(t, stdlib.Config{FS: cfg}, fmt.Sprintf(`
						;(async () => {
							const fs = (await import(%q)).default
							const prefix = %q
							console.log(%s)
						})().catch(e => console.log("ERROR: " + e.message))
					`, api.module, filepath.ToSlash(prefix), api.call))
					if strings.HasPrefix(out, "ERROR:") || out == "" {
						t.Fatalf("mkdtemp(%q): %s", prefix, out)
					}
					hostPath := filepath.FromSlash(out)
					if rooted {
						hostPath = filepath.Join(work, filepath.FromSlash(strings.TrimPrefix(out, "/")))
					}
					// Also clean up the old implementation's misplaced directory when
					// this regression test runs against it.
					defer os.Remove(hostPath)
					if filepath.Clean(filepath.Dir(filepath.FromSlash(out))) != filepath.Clean(parent) ||
						!strings.HasPrefix(filepath.Base(out), base) || len(filepath.Base(out)) <= len(base) {
						t.Fatalf("mkdtemp(%q) = %q; want a new directory in %q with prefix %q", prefix, out, parent, base)
					}
					if info, err := os.Stat(hostPath); err != nil || !info.IsDir() {
						t.Fatalf("created directory %q: %v, %v", hostPath, info, err)
					}
				})
			}
		}
	}
}

func TestFilesMkdtempPolicy(t *testing.T) {
	for _, api := range mkdtempAPIs {
		for _, name := range []string{"readonly", "missing-parent", "file-parent", "escape", "outward-link", "linked-parent"} {
			t.Run(api.name+"/"+name, func(t *testing.T) {
				work := t.TempDir()
				root := filepath.Join(work, "root")
				outside := filepath.Join(work, "outside")
				for _, dir := range []string{root, outside, filepath.Join(root, "nested")} {
					if err := os.Mkdir(dir, 0o755); err != nil {
						t.Fatal(err)
					}
				}
				cfg := &stdlib.FS{Root: root}
				prefix := "/nested/run-"
				switch name {
				case "readonly":
					cfg.ReadOnly = true
				case "missing-parent":
					prefix = "/missing/run-"
				case "file-parent":
					if err := os.WriteFile(filepath.Join(root, "file"), nil, 0o644); err != nil {
						t.Fatal(err)
					}
					prefix = "/file/run-"
				case "escape":
					prefix = "../outside/run-"
				case "outward-link", "linked-parent":
					target := outside
					if name == "linked-parent" {
						target = filepath.Join(root, "nested")
					}
					if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
					prefix = "/link/run-"
				}
				out, _ := run(t, stdlib.Config{FS: cfg}, fmt.Sprintf(`
					;(async () => {
						const fs = (await import(%q)).default
						const prefix = %q
						try {
							const result = %s
							console.log("created: " + result)
						} catch (e) { console.log("refused: " + e.message) }
					})().catch(e => console.log("ERROR: " + e.message))
				`, api.module, prefix, api.call))
				if name == "linked-parent" {
					if !strings.HasPrefix(out, "created: /nested/run-") {
						t.Fatalf("linked parent: %q", out)
					}
				} else if !strings.HasPrefix(out, "refused:") {
					t.Fatalf("mkdtemp(%q) = %q; want refusal", prefix, out)
				}
				if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
					t.Fatalf("outside directory changed: %v, %v", entries, err)
				}
			})
		}
	}
}
