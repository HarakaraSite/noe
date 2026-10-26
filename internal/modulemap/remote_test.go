package modulemap

import (
	"testing"
)

func TestRemoteLoaderRetainsFinalSourceAndRequestAliases(t *testing.T) {
	const final = "https://example.invalid/pkg/module?q=1#part"
	requests := 0
	loader := NewRemoteLoader(nil, "", "", nil, func(id string, retained func(string) bool) (string, string, error) {
		requests++
		if requests == 1 {
			if retained(final) {
				t.Fatal("source retained before its first successful read")
			}
			return "first source", final, nil
		}
		if !retained(final) {
			t.Fatal("remote reader cannot recognize retained final ID")
		}
		// Even a remote reader returning changed bytes cannot replace the
		// runtime's source when an unknown redirect reaches the same ID.
		return "changed source", final, nil
	})
	for _, spec := range []string{
		"https://example.invalid/redirect",
		"https://example.invalid/redirect",
		"https://example.invalid/other-redirect",
		final,
	} {
		source, id, err := loader.Load(spec, "/entry.js")
		if err != nil || id != final || source != "first source" {
			t.Fatalf("Load(%q)=(%q,%q,%v)", spec, source, id, err)
		}
	}
	if requests != 2 {
		t.Fatalf("remote reads=%d want first and unknown redirect only", requests)
	}
}

func TestRemoteLoaderLocalEntryCheckDoesNotDisableDependencies(t *testing.T) {
	snapshot, err := Parse([]byte(`{"imports":{"entry":"https://example.invalid/entry"}}`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	loader := NewRemoteLoader(snapshot, "", "", nil, func(id string, _ func(string) bool) (string, string, error) {
		reads++
		return "export {};", id, nil
	})
	for _, spec := range []string{"entry", "https://example.invalid/entry"} {
		if _, _, err := loader.LoadLocal(spec, "/worker.mjs"); err == nil {
			t.Fatal("URL entry accepted")
		}
	}
	if reads != 0 {
		t.Fatal("local entry check performed remote I/O")
	}
	if _, _, err := loader.Load("https://example.invalid/dependency", "/worker.mjs"); err != nil || reads != 1 {
		t.Fatalf("dependency load error=%v reads=%d", err, reads)
	}
}
