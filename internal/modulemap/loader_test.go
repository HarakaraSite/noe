package modulemap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoaderCachesSuccessfulSourceByCanonicalID(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "module.js")
	if err := os.WriteFile(modulePath, []byte(`export const value = "first";`), 0o600); err != nil {
		t.Fatal(err)
	}
	mapData := []byte(`{"imports":{"alias":"./module.js","alias-two":"./sub/../module.js"}}`)
	snapshot, err := Parse(mapData, dir)
	if err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(snapshot, "", "")
	first, id, err := loader.Load("alias", filepath.Join(dir, "main.js"))
	if err != nil {
		t.Fatal(err)
	}
	if id != modulePath {
		t.Fatalf("resolved ID = %q, want %q", id, modulePath)
	}
	if err := os.WriteFile(modulePath, []byte(`throw new Error("changed");`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	for _, referrer := range []string{filepath.Join(dir, "a.js"), filepath.Join(dir, "sub", "b.js")} {
		alias := "alias-two"
		got, gotID, err := loader.Load(alias, referrer)
		if err != nil {
			t.Fatalf("Load after deletion from %q: %v", referrer, err)
		}
		if gotID != id || got != first {
			t.Errorf("Load after deletion = (%q, %q), want cached source and ID", got, gotID)
		}
	}
}

func TestLoaderDoesNotCacheReadFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "later.js")
	snapshot, err := Parse([]byte(`{"imports":{"later":"./later.js"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	loader := NewLoader(snapshot, "", "")
	if _, _, err := loader.Load("later", filepath.Join(dir, "main.js")); err == nil {
		t.Fatal("missing source unexpectedly loaded")
	}
	if err := os.WriteFile(path, []byte(`export const ready = true;`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, _, err := loader.Load("later", filepath.Join(dir, "main.js"))
	if err != nil {
		t.Fatalf("retry after source appears: %v", err)
	}
	if !strings.Contains(got, "ready") {
		t.Fatalf("retry loaded %q", got)
	}
}

func TestLoadUsesInMemoryEntrySource(t *testing.T) {
	dir := t.TempDir()
	entryPath := filepath.Join(dir, "entry.mjs")
	if err := os.WriteFile(entryPath, []byte(`throw new Error("disk entry used");`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Parse([]byte(`{"imports":{"entry":"./entry.mjs"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		source string
	}{
		{"nonempty", `export const value = 42;`},
		{"empty", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entryName := filepath.Join(dir, "nested") + string(filepath.Separator) + ".." + string(filepath.Separator) + "entry.mjs"
			loader := NewLoader(snapshot, entryName, tc.source)
			entryID, err := loader.EntryID()
			if err != nil || entryID != entryPath {
				t.Fatalf("EntryID = %q, %v; want %q", entryID, err, entryPath)
			}
			for i := 0; i < 2; i++ {
				got, id, err := loader.Load("entry", filepath.Join(dir, "other", "main.js"))
				if err != nil {
					t.Fatal(err)
				}
				if id != entryPath || got != tc.source {
					t.Fatalf("entry loader returned (%q, %q), want (%q, %q)", got, id, tc.source, entryPath)
				}
			}
		})
	}
}

func TestLoadRejectsNonESMExtension(t *testing.T) {
	loader := NewLoader(nil, "", "")
	for _, path := range []string{"./module", "./module.cjs", "./module.ts"} {
		if _, _, err := loader.Load(path, filepath.Join(t.TempDir(), "main.js")); err == nil || !strings.Contains(err.Error(), ".js or .mjs") {
			t.Errorf("Load(%q) error = %v", path, err)
		}
	}
}

func TestLoaderInstancesHaveIndependentSourceCaches(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "shared.js")
	if err := os.WriteFile(path, []byte(`export const version = 1;`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Parse([]byte(`{"imports":{"shared":"./shared.js"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	firstLoader := NewLoader(snapshot, "", "")
	first, _, err := firstLoader.Load("shared", filepath.Join(dir, "a.js"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`export const version = 2;`), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := NewLoader(snapshot, "", "").Load("shared", filepath.Join(dir, "b.js"))
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.Contains(second, "version = 2") {
		t.Fatalf("independent loader source = %q, first = %q", second, first)
	}
}

func TestLocalLoaderRejectsHTTPWithoutFilesystemRead(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		parse := Parse
		base := t.TempDir()
		localID := filepath.Join(base, "local.mjs")
		if virtual {
			parse = ParseVirtual
			base = "/workspace"
			localID = "/workspace/local.mjs"
		}
		snapshot, err := parse([]byte(`{"imports":{"remote":"https://example.invalid/module","local":"./local.mjs"}}`), base)
		if err != nil {
			t.Fatal(err)
		}
		var reads []string
		loader := NewReaderLoader(snapshot, "", "", func(name string) ([]byte, error) {
			reads = append(reads, name)
			return []byte("export const value = 1;"), nil
		})
		for _, spec := range []string{"https://example.invalid/module.js", "http://example.invalid/no-extension", "remote", "./dep.js", "/dep.js"} {
			_, _, err := loader.Load(spec, "https://example.invalid/main.js")
			if err == nil || !strings.Contains(err.Error(), "HTTP/HTTPS module loading is unsupported") {
				t.Fatalf("virtual=%v Load(%q) error = %v", virtual, spec, err)
			}
		}
		if len(reads) != 0 {
			t.Fatalf("virtual=%v URL IDs reached filesystem: %v", virtual, reads)
		}
		// A remote referrer does not remove the selected filesystem's local
		// capability; explicit file URLs and local map targets still use it.
		for _, spec := range []string{fileURL(t, localID), "local"} {
			_, id, err := loader.Load(spec, "https://example.invalid/main.js")
			if err != nil || id != localID {
				t.Fatalf("virtual=%v Load(%q) = %q, %v; want %q", virtual, spec, id, err, localID)
			}
		}
		if len(reads) != 1 || reads[0] != localID {
			t.Fatalf("local reads = %v; want [%s]", reads, localID)
		}
		// Local extension validation still applies when the importer is remote.
		if _, _, err := loader.Load(fileURL(t, strings.TrimSuffix(localID, ".mjs")+".ts"), "https://example.invalid/main.js"); err == nil || !strings.Contains(err.Error(), ".js or .mjs") {
			t.Fatalf("remote to local extension error = %v", err)
		}
	}
}

func TestLocalLoaderRejectsHTTPEntryName(t *testing.T) {
	for _, snapshot := range []*Snapshot{nil, {virtual: true}} {
		loader := NewReaderLoader(snapshot, "https://example.invalid/entry.js", "export {};", func(name string) ([]byte, error) {
			t.Fatalf("HTTP entry reached filesystem as %q", name)
			return nil, nil
		})
		if id, err := loader.EntryID(); err == nil || id != "" || !strings.Contains(err.Error(), "HTTP/HTTPS entry URLs are unsupported") {
			t.Fatalf("EntryID = %q, %v", id, err)
		}
	}
}
