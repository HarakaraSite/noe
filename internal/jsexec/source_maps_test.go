package jsexec

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type sourceMapTestFS struct {
	files    map[string][]byte
	readErr  error
	readPath []string
}

func (f *sourceMapTestFS) ReadFile(ctx context.Context, path string) ([]byte, error) {
	f.readPath = append(f.readPath, path)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if f.readErr != nil {
		return nil, f.readErr
	}
	data, ok := f.files[path]
	if !ok {
		return nil, fs.ErrNotExist
	}
	return append([]byte(nil), data...), nil
}

func (*sourceMapTestFS) WriteFileAtomic(context.Context, string, []byte) error { return nil }
func (*sourceMapTestFS) CheckWritable() error                                  { return nil }
func (*sourceMapTestFS) Stat(context.Context, string) (fs.FileInfo, error)     { return nil, nil }
func (*sourceMapTestFS) PhysicalCwd() string                                   { return "/work" }

func TestReadSourceMapInlineDataURLs(t *testing.T) {
	ctx := context.Background()
	json := []byte(`{"version":3,"sources":[],"mappings":""}`)
	base64URL := "data:application/json;charset=utf-8;base64," + base64.StdEncoding.EncodeToString(json)
	percentURL := "data:application/json," + url.PathEscape(string(json))
	wantLocalBase := fileModuleURL("/work/dist/app.js")
	for _, test := range []struct {
		name, script, reference, wantBase string
	}{
		{"base64 local", "/work/dist/app.js", base64URL, wantLocalBase},
		{"percent local", "/work/dist/app.js", percentURL, wantLocalBase},
		{"opaque script", "memory:module", percentURL, "memory:module"},
		{"remote script", "https://example.test/app.js", base64URL, "https://example.test/app.js"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, base, err := readSourceMap(ctx, nil, test.script, test.reference)
			if err != nil || string(data) != string(json) || base != test.wantBase {
				t.Fatalf("readSourceMap = %q, %q, %v; want inline JSON and base %q", data, base, err, test.wantBase)
			}
		})
	}
	if _, _, err := readSourceMap(ctx, nil, "/work/app.js", "data:text/plain,%7B%7D"); err == nil {
		t.Fatal("non-JSON data URL was accepted")
	}
	if _, _, err := readSourceMap(ctx, nil, "/work/app.js", "data:application/json;base64,not-base64"); err == nil {
		t.Fatal("invalid Base64 data URL was accepted")
	}
	if _, _, err := readSourceMap(ctx, nil, "/work/app.js", "app.map"); err == nil {
		t.Fatal("sidecar was accepted without a selected filesystem")
	}
	malformedJSON := "data:application/json,%7Bbroken"
	data, _, err := readSourceMap(ctx, nil, "/work/app.js", malformedJSON)
	if err != nil || string(data) != "{broken" {
		t.Fatalf("map JSON was validated eagerly: %q, %v", data, err)
	}
}

func TestReadSourceMapResolvesRelativeAndEscapedPaths(t *testing.T) {
	files := &sourceMapTestFS{files: map[string][]byte{
		"/work/maps/sub/app map.json": []byte(`{"sources":["../../src/a.ts"],"mappings":""}`),
		"/work/dist/maps/%2F.map":     []byte(`malformed but returned unchanged`),
		"/rooted/map.map":             []byte(`{}`),
	}}
	ctx := context.Background()
	data, base, err := readSourceMap(ctx, files, "/work/dist/app.mjs", "../maps/sub/app%20map.json")
	if err != nil || !strings.Contains(string(data), `"sources"`) {
		t.Fatalf("relative sidecar read = %q, %v", data, err)
	}
	if want := "file:///work/maps/sub/app%20map.json"; base != want {
		t.Fatalf("map base = %q, want %q", base, want)
	}
	if got := strings.Join(files.readPath, ","); got != "/work/maps/sub/app map.json" {
		t.Fatalf("filesystem paths = %q", got)
	}
	_, base, err = readSourceMap(ctx, files, "file:///work/dist/app.mjs", "../maps/sub/app%20map.json")
	if err != nil || base != "file:///work/maps/sub/app%20map.json" {
		t.Fatalf("local file URL script base = %q, %v", base, err)
	}
	if got := files.readPath[len(files.readPath)-1]; got != "/work/maps/sub/app map.json" {
		t.Fatalf("local file URL filesystem path = %q", got)
	}

	data, base, err = readSourceMap(ctx, files, "/work/dist/app.mjs", "maps/%252F.map")
	if err != nil || string(data) != "malformed but returned unchanged" {
		t.Fatalf("escaped sidecar read = %q, %v", data, err)
	}
	if want := "file:///work/dist/maps/%252F.map"; base != want {
		t.Fatalf("escaped map base = %q, want %q", base, want)
	}
	if got := files.readPath[len(files.readPath)-1]; got != "/work/dist/maps/%2F.map" {
		t.Fatalf("escaped path was not decoded exactly once: %q", got)
	}

	_, base, err = readSourceMap(ctx, files, "/work/dist/app.mjs", "/rooted/map.map")
	if err != nil || base != "file:///rooted/map.map" {
		t.Fatalf("rooted map URL = %q, %v", base, err)
	}
}

func TestReadSourceMapUsesRootedReadOnlyFilesystemView(t *testing.T) {
	root := t.TempDir()
	virtualMaps := filepath.Join(root, "virtual", "maps", "sub")
	if err := os.MkdirAll(virtualMaps, 0o700); err != nil {
		t.Fatal(err)
	}
	mapData := []byte(`{"version":3,"sources":["../../../src/original.ts"],"sourcesContent":["source"],"mappings":"AAAA"}`)
	if err := os.WriteFile(filepath.Join(virtualMaps, "app.map"), mapData, 0o600); err != nil {
		t.Fatal(err)
	}
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: "/virtual", ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	view := filesystem.WithContext(ctx, backend)
	data, base, err := readSourceMap(ctx, view, "/virtual/dist/app.js", "../maps/sub/app.map")
	if err != nil || string(data) != string(mapData) {
		t.Fatalf("read-only rooted map read = %q, %v", data, err)
	}
	if base != "file:///virtual/maps/sub/app.map" {
		t.Fatalf("rooted map base = %q", base)
	}

	outside := t.TempDir()
	outsideMap := filepath.Join(outside, "escape.map")
	if err := os.WriteFile(outsideMap, []byte(`{"version":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("symlink privilege is platform-configured on Windows")
	}
	if err := os.Symlink(outsideMap, filepath.Join(root, "virtual", "maps", "escape.map")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readSourceMap(ctx, view, "/virtual/dist/app.js", "../maps/escape.map"); err == nil {
		t.Fatal("filesystem view followed an outward map symlink")
	}
}

func TestReadSourceMapRejectsExternalAuthorityWithoutReading(t *testing.T) {
	files := &sourceMapTestFS{files: map[string][]byte{"/work/app.map": []byte(`{}`)}}
	ctx := context.Background()
	for _, script := range []string{"memory:entry", "owner:module", "https://example.test/app.js", "file://other-host/app.js"} {
		if _, _, err := readSourceMap(ctx, files, script, "app.map"); err == nil {
			t.Errorf("sidecar accepted for script ID %q", script)
		}
	}
	for _, reference := range []string{"https://cdn.test/app.map", "file:///work/app.map", "//cdn.test/app.map"} {
		if _, _, err := readSourceMap(ctx, files, "/work/app.js", reference); err == nil {
			t.Errorf("external map URL %q was accepted", reference)
		}
	}
	if len(files.readPath) != 0 {
		t.Fatalf("rejected URLs caused filesystem reads: %q", files.readPath)
	}
	inline := "data:application/json,%7B%7D"
	if _, base, err := readSourceMap(ctx, files, "https://example.test/app.js", inline); err != nil || base != "https://example.test/app.js" {
		t.Fatalf("inline map on a remote script = %q, %v", base, err)
	}
	if len(files.readPath) != 0 {
		t.Fatalf("inline remote map read a sidecar: %q", files.readPath)
	}
}

func TestReadSourceMapRejectsEncodedWindowsUNCWithoutReading(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path interpretation is platform-specific")
	}
	files := &sourceMapTestFS{files: map[string][]byte{}}
	for _, reference := range []string{
		"/%5Cserver/share/map.map",
		"/%2F%5Cserver/share/map.map",
	} {
		if _, _, err := readSourceMap(context.Background(), files, `C:\work\dist\app.js`, reference); err == nil {
			t.Errorf("encoded UNC map path %q was accepted", reference)
		}
	}
	if len(files.readPath) != 0 {
		t.Fatalf("encoded UNC map paths caused filesystem reads: %q", files.readPath)
	}
}

func TestReadSourceMapContextBindingsAndMissingSidecar(t *testing.T) {
	files := &sourceMapTestFS{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := readSourceMap(ctx, files, "/work/app.js", "app.map"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled helper error = %v", err)
	}
	if len(files.readPath) != 0 {
		t.Fatalf("cancelled helper read a sidecar: %q", files.readPath)
	}

	active := &Bindings{ctx: context.Background(), fs: files}
	if _, _, err := active.ReadSourceMap("/work/app.js", "missing.map"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing sidecar error = %v", err)
	}
	if err := active.Close(); err != nil {
		t.Fatal(err)
	}
	before := len(files.readPath)
	if _, _, err := active.ReadSourceMap("/work/app.js", "data:application/json,%7B%7D"); err == nil {
		t.Fatal("closed binding loaded an inline source map")
	}
	if len(files.readPath) != before {
		t.Fatalf("closed binding performed a filesystem read: %q", files.readPath)
	}
}

func TestReadSourceMapMapsStackFromSidecarBase(t *testing.T) {
	mapData := []byte(`{"version":3,"sources":["../src/original.ts"],"names":[],"mappings":"AAAA;AACA"}`)
	files := &sourceMapTestFS{files: map[string][]byte{"/work/maps/app.map": mapData}}
	rt := quickjs.New(quickjs.WithSourceMaps(func(script, reference string) ([]byte, string, error) {
		return readSourceMap(context.Background(), files, script, reference)
	}))
	defer rt.Close()
	source := "function fail(){ throw new Error('mapped') }\nfail()\n//# sourceMappingURL=../maps/app.map\n"
	_, evalErr := rt.EvalFile("/work/dist/app.js", source)
	var exception *quickjs.Error
	if !errors.As(evalErr, &exception) {
		t.Fatalf("evaluation error = %v, want a JavaScript exception", evalErr)
	}
	stack, err := exception.Value().Get("stack")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stack.String(), "/work/src/original.ts:") {
		t.Fatalf("stack was not mapped against the sidecar's directory: %s", stack.String())
	}
	if got := strings.Join(files.readPath, ","); got != "/work/maps/app.map" {
		t.Fatalf("sidecar reads = %q", got)
	}
}
