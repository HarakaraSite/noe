package modulemap

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestParseBarePrefixMappingsChooseLongestAndExactFirst(t *testing.T) {
	base := t.TempDir()
	snapshot, err := Parse([]byte(`{"imports":{
		"pkg/":"./lib/",
		"pkg/deep/":"./deep/",
		"pkg/deep/exact":"./exact.data",
		"pkg/deep/both/":"./nested/",
		"pkg/deep/both/item":"./exact-item.bin"
	}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ specifier, want string }{
		{"pkg/readme.txt", filepath.Join(base, "lib", "readme.txt")},
		{"pkg/deep/template.data", filepath.Join(base, "deep", "template.data")},
		{"pkg/deep/exact", filepath.Join(base, "exact.data")},
		{"pkg/deep/both/item", filepath.Join(base, "exact-item.bin")},
		{"pkg/deep/both/child/asset", filepath.Join(base, "nested", "child", "asset")},
	} {
		got, err := snapshot.Resolve(test.specifier, "")
		if err != nil || got != test.want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", test.specifier, got, err, test.want)
		}
	}

	for _, specifier := range []string{"pkg/../outside", "pkg/deep/../../outside"} {
		if _, err := snapshot.Resolve(specifier, ""); err == nil || !strings.Contains(err.Error(), "does not contain suffix") {
			t.Errorf("Resolve(%q) error = %v, want a prefix containment error", specifier, err)
		}
	}
	if _, err := snapshot.Resolve("pkgx/file", ""); err == nil {
		t.Fatal("nonmatching bare specifier unexpectedly resolved")
	}
}

func TestPrefixTargetSeparatorsAndLocalPercentNames(t *testing.T) {
	base := t.TempDir()
	snapshot, err := Parse([]byte(`{"imports":{"files/":"./data/"}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Resolve("files/%2e%2e.txt", "")
	want := filepath.Join(base, "data", "%2e%2e.txt")
	if err != nil || got != want {
		t.Fatalf("local percent filename = %q, %v; want %q", got, err, want)
	}

	if _, err := Parse([]byte(`{"imports":{"files/":"./data"}}`), base); err == nil || !strings.Contains(err.Error(), "prefix target must end in") {
		t.Fatalf("prefix target without a trailing separator error = %v", err)
	}
	if snapshot.Imports()["files/"] != filepath.Join(base, "data")+string(filepath.Separator) {
		t.Fatalf("prefix target lost its directory separator: %q", snapshot.Imports()["files/"])
	}

	if runtime.GOOS == "windows" {
		nativeTarget := filepath.Join(base, "native", "lib") + string(filepath.Separator)
		nativeScope := filepath.Join(base, "app") + string(filepath.Separator)
		document, err := json.Marshal(map[string]any{
			"imports": map[string]string{"native/": nativeTarget},
			"scopes":  map[string]map[string]string{nativeScope: {"scoped/": filepath.Join(base, "scoped") + string(filepath.Separator)}},
		})
		if err != nil {
			t.Fatal(err)
		}
		native, err := Parse(document, base)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := native.Imports()["native/"], nativeTarget; got != want {
			t.Fatalf("native prefix target = %q, want %q", got, want)
		}
		got, err = native.Resolve("native/readme.data", "")
		if want := filepath.Join(base, "native", "lib", "readme.data"); err != nil || got != want {
			t.Fatalf("native prefix resolution = %q, %v; want %q", got, err, want)
		}
		got, err = native.Resolve("scoped/file.bin", filepath.Join(base, "app", "main.js"))
		if want := filepath.Join(base, "scoped", "file.bin"); err != nil || got != want {
			t.Fatalf("native scope prefix resolution = %q, %v; want %q", got, err, want)
		}

		relative, err := Parse([]byte(`{"imports":{"relative/":".\\lib\\"}}`), base)
		if err != nil {
			t.Fatalf("parse native relative prefix: %v", err)
		}
		wire, err := json.Marshal(relative)
		if err != nil {
			t.Fatal(err)
		}
		reparsed, err := Parse(wire, filepath.Join(base, "different-base"))
		if err != nil {
			t.Fatalf("reparse marshaled native relative prefix: %v", err)
		}
		got, err = reparsed.Resolve("relative/file.bin", "")
		if want := filepath.Join(base, "lib", "file.bin"); err != nil || got != want {
			t.Fatalf("native relative prefix after MarshalJSON = %q, %v; want %q", got, err, want)
		}
	}
}

func TestHTTPPrefixSuffixUsesURLResolutionAndStaysWithinBase(t *testing.T) {
	snapshot, err := Parse([]byte(`{"imports":{
		"remote/":"https://cdn.test/pkg/",
		"query/":"https://cdn.test/pkg/?q=/"
	}}`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	got, err := snapshot.Resolve("remote/module.js?version=2", "")
	if err != nil || got != "https://cdn.test/pkg/module.js?version=2" {
		t.Fatalf("remote query suffix = %q, %v", got, err)
	}
	got, err = snapshot.Resolve("remote/?version=2", "")
	if err != nil || got != "https://cdn.test/pkg/?version=2" {
		t.Fatalf("remote query-only suffix = %q, %v", got, err)
	}
	got, err = snapshot.Resolve("remote/#part", "")
	if err != nil || got != "https://cdn.test/pkg/#part" {
		t.Fatalf("remote fragment suffix = %q, %v", got, err)
	}
	for _, suffix := range []string{
		"../outside.js",
		"%2e%2e/outside.js",
		"%2E%2e/outside.js",
		".%2e/outside.js",
		"%2e./outside.js",
		"a/%2e%2e/%2e%2e/outside.js",
		"/outside.js",
		"https://other.test/outside.js",
		"data:text/plain,remote",
	} {
		if _, err := snapshot.Resolve("remote/"+suffix, ""); err == nil {
			t.Errorf("HTTP prefix accepted escaping suffix %q", suffix)
		}
	}
	if _, err := snapshot.Resolve("query/module.js", ""); err == nil {
		t.Fatal("query-bearing prefix target consumed a suffix into the query")
	}

	// Explicit HTTP IDs keep their preexisting percent-escape representation.
	got, err = (*Snapshot)(nil).Resolve("https://cdn.test/pkg/%2e%2e/module.js", "")
	if err != nil || got != "https://cdn.test/pkg/%2e%2e/module.js" {
		t.Fatalf("explicit encoded-dot URL = %q, %v", got, err)
	}
}

func TestScopesUseExactThenMostSpecificDirectoryAndFallback(t *testing.T) {
	base := t.TempDir()
	snapshot, err := Parse([]byte(`{"imports":{
		"global":"./global.data",
		"pkg/":"./global-pkg/"
	},"scopes":{
		"./src/":{"entry":"./dir-entry.data","fallback":"./dir-fallback.data","pkg/":"./scope-pkg/"},
		"./src/nested/":{"specific":"./nested.data"},
		"./src/main.js":{"entry":"./exact-entry.data"}
	}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ specifier, referrer, want string }{
		{"entry", filepath.Join(base, "src", "main.js"), filepath.Join(base, "exact-entry.data")},
		{"entry", filepath.Join(base, "src", "other.js"), filepath.Join(base, "dir-entry.data")},
		{"fallback", filepath.Join(base, "src", "nested", "main.js"), filepath.Join(base, "dir-fallback.data")},
		{"specific", filepath.Join(base, "src", "nested", "main.js"), filepath.Join(base, "nested.data")},
		{"pkg/child.json", filepath.Join(base, "src", "nested", "main.js"), filepath.Join(base, "scope-pkg", "child.json")},
		{"global", filepath.Join(base, "elsewhere.js"), filepath.Join(base, "global.data")},
	} {
		got, err := snapshot.Resolve(test.specifier, test.referrer)
		if err != nil || got != test.want {
			t.Errorf("Resolve(%q from %q) = %q, %v; want %q", test.specifier, test.referrer, got, err, test.want)
		}
	}
}

func TestHTTPReferrerScopesUseCanonicalIDs(t *testing.T) {
	snapshot, err := Parse([]byte(`{"imports":{"remote/":"https://cdn.test/global/"},"scopes":{
		"https://EXAMPLE.test:443/app/":{"remote/":"https://cdn.test/scoped/"},
		"https://example.test/app/main.js":{"remote/":"https://cdn.test/exact/"}
	}}`), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ referrer, want string }{
		{"https://example.test/app/main.js", "https://cdn.test/exact/module.mjs"},
		{"https://EXAMPLE.test:443/app/other.js", "https://cdn.test/scoped/module.mjs"},
		{"https://example.test/outside.js", "https://cdn.test/global/module.mjs"},
	} {
		got, err := snapshot.Resolve("remote/module.mjs", test.referrer)
		if err != nil || got != test.want {
			t.Errorf("Resolve remote module from %q = %q, %v; want %q", test.referrer, got, err, test.want)
		}
	}
}

func TestScopesAndMarshalJSONAreIndependentRoundTrip(t *testing.T) {
	base := t.TempDir()
	snapshot, err := Parse([]byte(`{"imports":{"pkg/":"./lib/","data":"./data.bin"},"scopes":{"./src/":{"pkg/":"./vendor/","entry":"./entry.ts"}}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	imports := snapshot.Imports()
	scopes := snapshot.Scopes()
	scopeKey := filepath.Join(base, "src") + string(filepath.Separator)
	imports["pkg/"] = "changed"
	scopes[scopeKey]["entry"] = "changed"
	scopes["new"] = map[string]string{"entry": "changed"}
	if got, err := snapshot.Resolve("pkg/file", ""); err != nil || got != filepath.Join(base, "lib", "file") {
		t.Fatalf("mutating Imports copy changed snapshot: %q, %v", got, err)
	}
	if got, err := snapshot.Resolve("entry", filepath.Join(base, "src", "main.js")); err != nil || got != filepath.Join(base, "entry.ts") {
		t.Fatalf("mutating Scopes copy changed snapshot: %q, %v", got, err)
	}

	wire, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Imports map[string]string            `json:"imports"`
		Scopes  map[string]map[string]string `json:"scopes"`
	}
	if err := json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(decoded.Imports["pkg/"], string(filepath.Separator)) || !strings.HasSuffix(decoded.Scopes[scopeKey]["pkg/"], string(filepath.Separator)) {
		t.Fatalf("MarshalJSON lost directory separators: imports=%q scopes=%q", decoded.Imports["pkg/"], decoded.Scopes[scopeKey]["pkg/"])
	}
	reparsed, err := Parse(wire, filepath.Join(base, "different-base"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.Imports(), reparsed.Imports()) || !reflect.DeepEqual(snapshot.Scopes(), reparsed.Scopes()) {
		t.Fatalf("MarshalJSON round-trip differs:\nimports: %#v != %#v\nscopes: %#v != %#v", snapshot.Imports(), reparsed.Imports(), snapshot.Scopes(), reparsed.Scopes())
	}
}

func TestReservedBuiltinsBypassGlobalAndScopedPrefixes(t *testing.T) {
	base := t.TempDir()
	snapshot, err := Parse([]byte(`{"imports":{"dns/":"./fake-dns/","node":"./fake-node/","noe":"./fake-noe/"},"scopes":{"./app/":{"dns/":"./scoped-dns/"}}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ specifier, referrer string }{
		{"dns/promises", filepath.Join(base, "app", "main.js")},
		{"dns/promises", filepath.Join(base, "outside.js")},
		{"node:fs", filepath.Join(base, "outside.js")},
		{"noe:agent", filepath.Join(base, "outside.js")},
	} {
		if got, err := snapshot.Resolve(test.specifier, test.referrer); err == nil || strings.Contains(got, "fake-") {
			t.Errorf("reserved built-in %q was remapped to %q (error %v)", test.specifier, got, err)
		}
	}
}

func TestSnapshotScopesReturnsNilWhenSnapshotIsNil(t *testing.T) {
	if got := (*Snapshot)(nil).Scopes(); got != nil {
		t.Fatalf("nil Snapshot.Scopes() = %#v, want nil", got)
	}
}
