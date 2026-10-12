package runtime

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestLocalModules(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "lib"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{
		"shared.js":     `export const value = 20;`,
		"lib/helper.js": `import {value} from "../shared.js"; export const result = await Promise.resolve(value + 2);`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	var stdout bytes.Buffer
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "main.js"), `import {result} from "./lib/helper.js"; console.log(result);`}, Options{Stdout: &stdout})
	if err != nil || code != 0 || stdout.String() != "22\n" {
		t.Fatalf("code=%d err=%v out=%q", code, err, &stdout)
	}
	for _, spec := range []string{"pkg", "node:fs", "https://example.invalid/a.js", "./shared", "./missing.js"} {
		_, err := Execute(context.Background(), Source{filepath.Join(dir, "main.js"), `import "` + spec + `";`}, Options{})
		if err == nil {
			t.Fatalf("import %q unexpectedly succeeded", spec)
		}
	}
}

func TestModuleCompletion(t *testing.T) {
	for _, tc := range []struct{ name, source, out, errorText string }{
		{"await host", `console.log(await later("ready", false));`, "ready\n", ""},
		{"remaining host work", `later("ready", false).then(console.log);`, "ready\n", ""},
		{"host rejection", `await later("host failed", true);`, "", "host failed"},
		{"late rejection", `later("ready", false).then(() => { throw new Error("late failed"); });`, "", "late failed"},
		{"never settles", `await new Promise(() => {});`, "", "nothing is left to settle"},
		{"root failure", `throw new Error("root failed");`, "", "root failed"},
		{"handled rejection", `later("caught", true).catch(e => console.log(e.message));`, "caught\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			h, err := newHost(Options{Stdout: &stdout})
			if err != nil {
				t.Fatal(err)
			}
			defer h.rt.Close()
			if err := h.rt.Set("later", func(value string, fail bool) quickjs.Value {
				return h.rt.Go(func(ctx context.Context) (any, error) {
					timer := time.NewTimer(5 * time.Millisecond)
					defer timer.Stop()
					select {
					case <-timer.C:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
					if fail {
						return nil, errors.New(value)
					}
					return value, nil
				})
			}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "main.js"), tc.source})
			if stdout.String() != tc.out {
				t.Fatalf("out=%q want=%q", &stdout, tc.out)
			}
			if tc.errorText == "" {
				if err != nil || code != 0 {
					t.Fatalf("code=%d err=%v", code, err)
				}
			} else if code != 1 || err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("code=%d err=%v want=%q", code, err, tc.errorText)
			}
			if tc.name == "root failure" && len(h.unhandled) != 0 {
				t.Fatalf("root error reported twice: %v", h.unhandled)
			}
			if tc.name == "late rejection" && !strings.Contains(Diagnostic(err), "main.js") {
				t.Fatalf("late rejection lost source location: %v", err)
			}
		})
	}
}
