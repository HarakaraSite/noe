package modulemap

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseUsesMapDirectoryAndMarshalsAbsoluteTargets(t *testing.T) {
	mapDir := filepath.Join(t.TempDir(), "maps")
	if err := os.MkdirAll(mapDir, 0o700); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Parse([]byte(`{"imports":{"mcp/client":"../vendor/mcp-client.mjs","app/helper":"./lib/helper.js"}}`), mapDir)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"mcp/client": filepath.Join(filepath.Dir(mapDir), "vendor", "mcp-client.mjs"),
		"app/helper": filepath.Join(mapDir, "lib", "helper.js"),
	} {
		got, err := snapshot.Resolve(key, "ignored.js")
		if err != nil {
			t.Fatalf("Resolve(%q): %v", key, err)
		}
		if got != want {
			t.Errorf("Resolve(%q) = %q, want %q", key, got, want)
		}
	}

	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var wire struct {
		Imports map[string]string `json:"imports"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"mcp/client": filepath.Join(filepath.Dir(mapDir), "vendor", "mcp-client.mjs"),
		"app/helper": filepath.Join(mapDir, "lib", "helper.js"),
	} {
		if got := wire.Imports[key]; got != want || !filepath.IsAbs(got) {
			t.Errorf("MarshalJSON target %q = %q, want absolute %q", key, got, want)
		}
	}
}

func TestLoadUsesMapFileParent(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "config", "import-map.json")
	if err := os.MkdirAll(filepath.Dir(mapPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapPath, []byte(`{"imports":{"x":"../lib/x.js"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Load(mapPath)
	if err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Resolve("x", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "lib", "x.js")
	if got != want {
		t.Fatalf("Load target = %q, want %q", got, want)
	}
}

func TestResolveLocalFormsWithoutSnapshot(t *testing.T) {
	dir := t.TempDir()
	referrer := filepath.Join(dir, "nested", "main.js")
	file := filepath.Join(dir, "nested", "dep.mjs")
	abs := filepath.Join(dir, "dep.js")
	for _, snapshot := range []*Snapshot{nil, {imports: map[string]string{}}} {
		t.Run(map[bool]string{true: "nil", false: "empty"}[snapshot == nil], func(t *testing.T) {
			cases := []struct{ spec, ref, want string }{
				{"./dep.mjs", referrer, file},
				{abs, referrer, abs},
				{fileURL(t, filepath.Join(dir, "local file#%日本.js")), referrer, filepath.Join(dir, "local file#%日本.js")},
			}
			for _, test := range cases {
				got, err := snapshot.Resolve(test.spec, test.ref)
				if err != nil {
					t.Errorf("Resolve(%q): %v", test.spec, err)
					continue
				}
				if got != test.want {
					t.Errorf("Resolve(%q) = %q, want %q", test.spec, got, test.want)
				}
			}
		})
	}
}

func TestResolveReferrerAndFileURLNormalization(t *testing.T) {
	dir := t.TempDir()
	module := filepath.Join(dir, "unicode # % 日本.mjs")
	refURL := fileURL(t, filepath.Join(dir, "nested", "main.js"))
	got, err := (*Snapshot)(nil).Resolve("../unicode # % 日本.mjs", refURL)
	if err != nil {
		t.Fatal(err)
	}
	if got != module {
		t.Fatalf("relative import from file URL = %q, want %q", got, module)
	}

	for _, spec := range []string{
		"file:///tmp/module.js?x=1",
		"file:///tmp/module.js#part",
		"file://remote-host/tmp/module.js",
		"ftp://example.invalid/module.js",
	} {
		if _, err := (*Snapshot)(nil).Resolve(spec, ""); err == nil {
			t.Errorf("Resolve(%q) unexpectedly succeeded", spec)
		}
	}

	if runtime.GOOS == "windows" {
		got, err := (*Snapshot)(nil).Resolve("file:///C:/some%20dir/module.mjs", "")
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(got) || !strings.HasPrefix(strings.ToLower(got), `c:\`) {
			t.Fatalf("Windows file URL resolved to %q", got)
		}
		if _, err := (*Snapshot)(nil).Resolve(`C:relative\module.js`, ""); err == nil || !strings.Contains(err.Error(), "drive-relative") {
			t.Fatalf("drive-relative path error = %v", err)
		}
		got, err = (*Snapshot)(nil).Resolve(`.\dep.js`, `C:\work\nested\main.js`)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join(`C:\work\nested`, "dep.js"); got != want {
			t.Fatalf("Windows relative path = %q, want %q", got, want)
		}
		got, err = (*Snapshot)(nil).Resolve(`\\server\share\module.mjs`, "")
		if err != nil {
			t.Fatalf("native UNC path: %v", err)
		}
		if !filepath.IsAbs(got) || !strings.HasPrefix(got, `\\server\share`) {
			t.Fatalf("UNC path resolved to %q", got)
		}
	} else {
		posixDoubleSlash := "//" + strings.TrimLeft(filepath.Join(dir, "double-slash.js"), "/")
		got, err := (*Snapshot)(nil).Resolve(posixDoubleSlash, "")
		if err != nil {
			t.Fatalf("POSIX double-slash absolute path: %v", err)
		}
		if got != filepath.Join(dir, "double-slash.js") {
			t.Fatalf("POSIX double-slash path = %q", got)
		}
	}
}

func TestParseRejectsInvalidMapsWithContext(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"duplicate imports", `{"imports":{},"imports":{}}`, `duplicate JSON key "imports"`},
		{"duplicate key", `{"imports":{"x":"./one.js","x":"./two.js"}}`, `duplicate JSON key "x"`},
		{"syntax", `{"imports":`, `invalid import map JSON`},
		{"root type", `[]`, `must be a JSON object`},
		{"unsupported field", `{"scopes":{}}`, `unsupported import map field "scopes"`},
		{"imports type", `{"imports":[]}`, `field "imports" must be an object`},
		{"target type", `{"imports":{"x":null}}`, `key "x" target must be a string`},
		{"empty key", `{"imports":{"":"./x.js"}}`, `key ""`},
		{"prefix key", `{"imports":{"x/":"./x.js"}}`, `prefix mappings are unsupported`},
		{"relative key", `{"imports":{"./x":"./x.js"}}`, `path and URL remapping is unsupported`},
		{"builtin key", `{"imports":{"node:fs":"./fs.js"}}`, `node: module names are reserved`},
		{"noe key", `{"imports":{"noe:agent":"./agent.js"}}`, `noe: module names are reserved`},
		{"bare target", `{"imports":{"x":"y"}}`, `target must be a relative path`},
		{"extension", `{"imports":{"x":"./x.ts"}}`, `must end in .js or .mjs`},
		{"unsupported target URL", `{"imports":{"x":"ftp://example.invalid/x.js"}}`, `unsupported target URL scheme`},
		{"trailing value", `{} {}`, `trailing JSON value`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := Parse([]byte(test.json), t.TempDir())
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Parse error = %v, want substring %q", err, test.want)
			}
		})
	}
}

func TestParseDoesNotReadTargets(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "unused.js")
	snapshot, err := Parse([]byte(`{"imports":{"unused":"./unused.js"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := snapshot.Resolve("unused", ""); err != nil {
		t.Fatalf("resolving an unused target should only canonicalize it: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target should not be created or read: stat error = %v", err)
	}
}

func fileURL(t *testing.T, path string) string {
	t.Helper()
	urlPath := filepath.ToSlash(path)
	if runtime.GOOS == "windows" && len(urlPath) >= 2 && urlPath[1] == ':' {
		urlPath = "/" + urlPath
	}
	u := url.URL{Scheme: "file", Path: urlPath}
	return u.String()
}
