package modulemap

import (
	"strings"
	"testing"
)

func TestVirtualLoaderKeepsWindowsRootNamespaceIndependentOfHostVolume(t *testing.T) {
	snapshot, err := ParseVirtual([]byte(`{"imports":{"entry":"./entry.mjs","lib":"/library/main.mjs"}}`), "/workspace")
	if err != nil {
		t.Fatal(err)
	}
	var reads []string
	loader := NewReaderLoader(snapshot, "/workspace/entry.mjs", `entry in memory`, func(name string) ([]byte, error) {
		reads = append(reads, name)
		return []byte(`export const value=1`), nil
	})
	for _, specifier := range []string{"entry", "./entry.mjs", `file:///workspace/entry.mjs`} {
		source, id, err := loader.Load(specifier, `\workspace\entry.mjs`)
		if err != nil || id != "/workspace/entry.mjs" || source != "entry in memory" {
			t.Fatalf("%s→%s %q err=%v", specifier, id, source, err)
		}
	}
	if len(reads) != 0 {
		t.Fatalf("memory entry read filesystem: %v", reads)
	}
	_, id, err := loader.Load("lib", "/workspace/entry.mjs")
	if err != nil || id != "/library/main.mjs" || strings.Join(reads, ",") != "/library/main.mjs" {
		t.Fatalf("virtual module id=%q reads=%v err=%v", id, reads, err)
	}
	_, id, err = loader.Load(`.\part.mjs`, "/library/main.mjs")
	if err != nil || id != "/library/part.mjs" {
		t.Fatalf("virtual relative id=%q err=%v", id, err)
	}
	for _, specifier := range []string{`C:\host\secret.mjs`, `file://remote/share/secret.mjs`, `file:///x.mjs?query`, `file:///x.mjs?`, `file:///x.mjs#`, `file:///C:/host/secret.mjs`} {
		if _, _, err := loader.Load(specifier, "/workspace/entry.mjs"); err == nil {
			t.Fatalf("accepted host path %q", specifier)
		}
	}
}
