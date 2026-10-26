package jsexec

import (
	"path/filepath"
	"testing"
)

func TestImportMetaFileURL(t *testing.T) {
	// Slash paths exercise URL construction independently of the host OS;
	// filepath.FromSlash/ToSlash supply native separators on Windows.
	for _, tc := range []struct{ name, path, want string }{
		{"local", "/work/main.mjs", "file:///work/main.mjs"},
		{"escaping", "/work/a b#?%/日本語.mjs", "file:///work/a%20b%23%3F%25/%E6%97%A5%E6%9C%AC%E8%AA%9E.mjs"},
		{"drive", "C:/work/a b#%.mjs", "file:///C:/work/a%20b%23%25.mjs"},
		{"UNC", "//server/share/a b#%.mjs", "file://server/share/a%20b%23%25.mjs"},
		{"synthetic", "/work/<virtual-entry>.mjs", "file:///work/%3Cvirtual-entry%3E.mjs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fileModuleURL(filepath.FromSlash(tc.path)); got != tc.want {
				t.Fatalf("fileModuleURL(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

func TestImportMetaModuleIdentity(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"/work/a b.mjs", "file:///work/a%20b.mjs"},
		{"https://example.com/a%2Fb.js?b=2&a=1#part", "https://example.com/a%2Fb.js?b=2&a=1#part"},
		{"file:///work/a%20b.mjs", "file:///work/a%20b.mjs"},
		{"memory:library", "memory:library"},
		{"owner:module", "owner:module"},
		{"host module/日本語", "noe:module/host%20module%2F%E6%97%A5%E6%9C%AC%E8%AA%9E"},
	} {
		if got := moduleIDURL(tc.id); got != tc.want {
			t.Errorf("moduleIDURL(%q) = %q, want %q", tc.id, got, tc.want)
		}
	}
}
