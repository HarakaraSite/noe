package modulemap

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalHTTPURL(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{"HTTP://EXAMPLE.COM:80", "http://example.com/"},
		{"https://EXAMPLE.COM:443/a/./b/../module", "https://example.com/a/module"},
		{"https://EXAMPLE.COM:8443/Module.TS?b=2&a=%2f+%20#Part%2f", "https://example.com:8443/Module.TS?b=2&a=%2f+%20#Part%2f"},
		{"http://[2001:DB8::1]:80", "http://[2001:db8::1]/"},
		{"https://example.com/a/%2e/%2E%2e/%2f/%41", "https://example.com/a/%2e/%2E%2e/%2f/%41"},
		{"https://example.com/a//b", "https://example.com/a//b"},
		{"https://example.com/a/.", "https://example.com/a/"},
		{"https://example.com?b=2&a=1", "https://example.com/?b=2&a=1"},
		{"https://example.com?a=1&b=2", "https://example.com/?a=1&b=2"},
		{"https://example.com/#one", "https://example.com/#one"},
		{"https://example.com/#two", "https://example.com/#two"},
		{"https://example.com/#", "https://example.com/"},
		{"https://example.com/?", "https://example.com/?"},
	} {
		t.Run(test.raw, func(t *testing.T) {
			if !IsHTTPURL(test.raw) {
				t.Fatal("HTTP URL not recognized")
			}
			got, err := CanonicalHTTPURL(test.raw)
			if err != nil || got != test.want {
				t.Fatalf("CanonicalHTTPURL = %q, %v; want %q", got, err, test.want)
			}
			again, err := CanonicalHTTPURL(got)
			if err != nil || again != got {
				t.Fatalf("canonicalizing a module ID again = %q, %v; want %q", again, err, got)
			}
		})
	}
}

func TestHTTPURLValidation(t *testing.T) {
	for _, raw := range []string{
		"http:module.js", "https:/module.js", "http:///module.js",
		"https://:443/module.js", "https://example.com:bad/module",
		"https://[::1/module", "https://example.com/%zz",
	} {
		t.Run(raw, func(t *testing.T) {
			if !IsHTTPURL(raw) {
				t.Fatal("malformed HTTP URL should still be classified by scheme")
			}
			if _, err := CanonicalHTTPURL(raw); err == nil {
				t.Fatal("invalid HTTP URL accepted")
			}
			for _, parse := range []func([]byte, string) (*Snapshot, error){Parse, ParseVirtual} {
				data, _ := json.Marshal(map[string]any{"imports": map[string]string{"alias": raw}})
				if _, err := parse(data, "/workspace"); err == nil || !strings.Contains(err.Error(), "key \"alias\" target") {
					t.Fatalf("map target validation = %v", err)
				}
			}
			for _, s := range []*Snapshot{nil, {virtual: true}} {
				if _, err := s.Resolve(raw, "/main.js"); err == nil {
					t.Fatal("invalid HTTP specifier accepted")
				}
			}
		})
	}
	for _, raw := range []string{"ftp://example.com/m.js", "file:///m.js", "./m.js"} {
		if IsHTTPURL(raw) {
			t.Errorf("IsHTTPURL(%q) = true", raw)
		}
		if _, err := CanonicalHTTPURL(raw); err == nil {
			t.Errorf("CanonicalHTTPURL(%q) unexpectedly succeeded", raw)
		}
	}
}

func TestResolveRemoteReferences(t *testing.T) {
	referrer := "HTTPS://EXAMPLE.COM:443/pkg/nested/main?old=1#old"
	cases := []struct{ spec, ref, want string }{
		{"./dep", referrer, "https://example.com/pkg/nested/dep"},
		{"../dep.ts?b=2&a=1#Part", referrer, "https://example.com/pkg/dep.ts?b=2&a=1#Part"},
		{"/shared/./dep", referrer, "https://example.com/shared/dep"},
		{"//OTHER.EXAMPLE:443/dep", referrer, "https://other.example/dep"},
		{"?b=%2f&a=2", referrer, "https://example.com/pkg/nested/main?b=%2f&a=2"},
		{"?", referrer, "https://example.com/pkg/nested/main?"},
		{"#Part%2f", referrer, "https://example.com/pkg/nested/main?old=1#Part%2f"},
		{"#", referrer, "https://example.com/pkg/nested/main?old=1"},
		{"#new", "https://example.com/main?#old", "https://example.com/main?#new"},
		{"./%2e%2e/dep%2Fname", referrer, "https://example.com/pkg/nested/%2e%2e/dep%2Fname"},
		{"HTTP://OTHER.EXAMPLE:80", referrer, "http://other.example/"},
		{"https://OTHER.EXAMPLE:443", "/local/main.js", "https://other.example/"},
	}
	for _, s := range []*Snapshot{nil, {imports: map[string]string{}}, {virtual: true}} {
		for _, test := range cases {
			got, err := s.Resolve(test.spec, test.ref)
			if err != nil || got != test.want {
				t.Errorf("virtual=%v Resolve(%q, %q) = %q, %v; want %q",
					s != nil && s.virtual, test.spec, test.ref, got, err, test.want)
			}
		}
		for _, spec := range []string{"unmapped", "dep.js"} {
			if _, err := s.Resolve(spec, referrer); err == nil || !strings.Contains(err.Error(), "unmapped bare") {
				t.Errorf("remote bare import %q error = %v", spec, err)
			}
		}
		for _, spec := range []string{"ftp://example.com/module.js", "data:text/javascript,export{}", "./%zz", "//"} {
			if _, err := s.Resolve(spec, referrer); err == nil {
				t.Errorf("invalid/unsupported remote import %q accepted", spec)
			}
		}
	}
}

func TestURLMapTargetsStayRemoteAndRelativeTargetsStayLocal(t *testing.T) {
	for _, virtual := range []bool{false, true} {
		base := t.TempDir()
		parse := Parse
		if virtual {
			base = "/workspace/maps"
			parse = ParseVirtual
		}
		snapshot, err := parse([]byte(`{"imports":{"remote":"HTTPS://EXAMPLE.COM:443/pkg/../module?b=2&a=%2f#Part","local":"./local.mjs"}}`), base)
		if err != nil {
			t.Fatal(err)
		}
		wantRemote := "https://example.com/module?b=2&a=%2f#Part"
		wantLocal := filepath.Join(base, "local.mjs")
		if virtual {
			wantLocal = base + "/local.mjs"
		}
		for spec, want := range map[string]string{"remote": wantRemote, "local": wantLocal} {
			got, err := snapshot.Resolve(spec, "https://remote.example/dependency")
			if err != nil || got != want {
				t.Fatalf("virtual=%v Resolve(%q) = %q, %v; want %q", virtual, spec, got, err, want)
			}
		}
		entries := snapshot.Imports()
		entries["remote"] = "https://changed.example/"
		got, _ := snapshot.Resolve("remote", "")
		if got != wantRemote {
			t.Fatal("Imports mutation changed snapshot")
		}
		data, err := json.Marshal(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		transported, err := parse(data, base)
		if err != nil {
			t.Fatal(err)
		}
		got, err = transported.Resolve("remote", "")
		if err != nil || got != wantRemote {
			t.Fatalf("transported URL target = %q, %v", got, err)
		}
		if _, err := snapshot.Resolve("remote/sub", "https://example.com/main"); err == nil {
			t.Fatal("URL target unexpectedly introduced prefix mapping")
		}
	}
}

func TestRemoteReferrerExplicitFileURLIsLocal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local file#%.mjs")
	got, err := (*Snapshot)(nil).Resolve(fileURL(t, path), "https://example.com/main")
	if err != nil || got != path {
		t.Fatalf("remote to file URL = %q, %v; want %q", got, err, path)
	}
	virtual := &Snapshot{virtual: true}
	got, err = virtual.Resolve("file:///workspace/local%20file%23%25.mjs", "https://example.com/main")
	if err != nil || got != "/workspace/local file#%.mjs" {
		t.Fatalf("virtual remote to file URL = %q, %v", got, err)
	}
	for _, s := range []*Snapshot{nil, virtual} {
		for _, raw := range []string{"file://remote/m.js", "file:///m.js?x", "file:///m.js#", "file:///m.js?"} {
			if _, err := s.Resolve(raw, "https://example.com/main"); err == nil {
				t.Errorf("remote referrer bypassed local file URL restriction: %q", raw)
			}
		}
	}
}
