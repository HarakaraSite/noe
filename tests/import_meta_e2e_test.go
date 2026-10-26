//go:build e2e

package e2e_test

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportMetaURLThroughCLI(t *testing.T) {
	work := t.TempDir()
	mainPath := filepath.Join(work, "app", "entry.mjs")
	libraryPath := filepath.Join(work, "lib files#% 日本語", "module.mjs")
	writeFixture(t, libraryPath, `export const url=import.meta.url;
export const asset=new URL("./data.txt",import.meta.url).href;`)
	writeFixture(t, mainPath, `import {url,asset} from "../lib files#% 日本語/module.mjs";
console.log(JSON.stringify({entry:import.meta.url,library:url,asset}));`)
	fileURL := func(name string) string {
		path := filepath.ToSlash(name)
		if !strings.HasPrefix(path, "/") {
			path = "/" + path
		}
		return (&url.URL{Scheme: "file", Path: path}).String()
	}
	result := runCLI(t, work, nil, "run", mainPath)
	requireExit(t, result, 0)
	var got map[string]string
	if err := json.Unmarshal([]byte(result.stdout), &got); err != nil {
		t.Fatalf("decode %q: %v; stderr=%s", result.stdout, err, result.stderr)
	}
	want := map[string]string{
		"entry": fileURL(mainPath), "library": fileURL(libraryPath),
		"asset": fileURL(filepath.Join(filepath.Dir(libraryPath), "data.txt")),
	}
	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s=%q, want %q", key, got[key], value)
		}
	}
	if result.stderr != "" {
		t.Errorf("stderr=%q", result.stderr)
	}

	evaluated := runCLI(t, work, nil, "eval", `console.log(import.meta.url);`)
	requireExit(t, evaluated, 0)
	if evaluated.stdout != fileURL(filepath.Join(work, "<eval>.js"))+"\n" || evaluated.stderr != "" {
		t.Fatalf("eval metadata=%+v", evaluated)
	}
}
