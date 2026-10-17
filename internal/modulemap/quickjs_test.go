package modulemap

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

func TestQuickJSUnifiesAliasesAndRetainsLoadedNamespace(t *testing.T) {
	dir := t.TempDir()
	modulePath := filepath.Join(dir, "lib.js")
	if err := os.WriteFile(modulePath, []byte(`globalThis.moduleEvaluations = (globalThis.moduleEvaluations || 0) + 1; export const marker = {};`), 0o600); err != nil {
		t.Fatal(err)
	}
	snapshot, err := Parse([]byte(`{"imports":{"alias":"./lib.js","alias-two":"./nested/../lib.js"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	defer rt.Close()
	loader := NewLoader(snapshot, "", "")
	rt.SetModuleLoader(loader.Load)

	fileURL := testFileURL(t, modulePath)
	entryName := filepath.Join(dir, "main.js")
	entry := fmt.Sprintf(`import * as viaAlias from "alias";
import * as viaRelative from "./lib.js";
import * as viaAbsolute from %s;
import * as viaURL from %s;
globalThis.firstNamespace = viaAlias;
globalThis.sameNamespace = viaAlias === viaRelative && viaRelative === viaAbsolute && viaAbsolute === viaURL;
`, quoteJS(modulePath), quoteJS(fileURL))
	if _, err := rt.EvalModule(entryName, entry); err != nil {
		t.Fatal(err)
	}
	assertGlobalBool(t, rt, "sameNamespace", true)
	assertGlobalNumber(t, rt, "moduleEvaluations", 1)

	if err := os.WriteFile(modulePath, []byte(`throw new Error("disk source was re-read");`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(modulePath); err != nil {
		t.Fatal(err)
	}
	secondName := filepath.Join(dir, "other", "second.mjs")
	if _, err := rt.EvalModule(secondName, `import * as afterDelete from "alias-two"; globalThis.sameAfterDelete = globalThis.firstNamespace === afterDelete;`); err != nil {
		t.Fatalf("import through another alias after deleting source: %v", err)
	}
	assertGlobalBool(t, rt, "sameAfterDelete", true)
	assertGlobalNumber(t, rt, "moduleEvaluations", 1)
}

func TestQuickJSCyclicModules(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"left.js":  `import {right} from "./right.js"; export const left = "left"; export const combined = left + right;`,
		"right.js": `import {left} from "./left.js"; export const right = "right"; export function fromLeft(){ return left; }`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	rt := quickjs.New()
	defer rt.Close()
	rt.SetModuleLoader(NewLoader(nil, "", "").Load)
	entry := `import {combined} from "./left.js"; import {fromLeft} from "./right.js"; globalThis.cycleResult = combined + ":" + fromLeft();`
	if _, err := rt.EvalModule(filepath.Join(dir, "main.js"), entry); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Get("cycleResult")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := value.String(), "leftright:left"; got != want {
		t.Fatalf("cycle result = %q, want %q", got, want)
	}
}

func TestQuickJSInMemoryEntryAliasUsesSameModuleID(t *testing.T) {
	dir := t.TempDir()
	entryPath := filepath.Join(dir, "entry.js")
	snapshot, err := Parse([]byte(`{"imports":{"entry":"./entry.js"}}`), dir)
	if err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	defer rt.Close()
	loader := NewLoader(snapshot, entryPath, `globalThis.entryEvaluations = (globalThis.entryEvaluations || 0) + 1; export const value = 42;`)
	rt.SetModuleLoader(loader.Load)
	main := `import * as viaAlias from "entry"; import * as viaPath from "./entry.js"; globalThis.entrySame = viaAlias === viaPath; globalThis.entryValue = viaPath.value;`
	if _, err := rt.EvalModule(filepath.Join(dir, "main.js"), main); err != nil {
		t.Fatal(err)
	}
	assertGlobalBool(t, rt, "entrySame", true)
	assertGlobalNumber(t, rt, "entryEvaluations", 1)
	assertGlobalNumber(t, rt, "entryValue", 42)
}

func TestPinnedStdlibModuleReservationListMatchesRegisteredNames(t *testing.T) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	defer func() {
		loop.Close()
		rt.Close()
	}()
	config := stdlib.Config{
		Loop:    loop,
		FS:      &stdlib.FS{},
		Process: &stdlib.Process{},
		OS:      &stdlib.OSInfo{},
		Fetch:   &stdlib.Fetch{},
		DNS:     &stdlib.DNS{},
		Run:     &stdlib.Run{Allow: func(string, []string) error { return nil }},
		Serve:   &stdlib.Serve{Allow: func(string) error { return nil }},
		Sockets: &stdlib.WebSockets{Allow: func(*url.URL) error { return nil }},
		Workers: &stdlib.Workers{
			New:  func() (*quickjs.Runtime, error) { return quickjs.New(), nil },
			Load: func(string) (string, string, bool, error) { return "", "", false, fmt.Errorf("unexpected worker load") },
		},
	}
	if err := stdlib.Install(rt, config); err != nil {
		t.Fatal(err)
	}

	names := make([]string, 0, len(standardLibraryBareModules))
	for name := range standardLibraryBareModules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, specifier := range []string{name, "node:" + name} {
			if _, err := rt.RequireModule(specifier, ""); err != nil {
				t.Errorf("pinned stdlib did not register %q: %v", specifier, err)
			}
		}
	}

	// Registered native modules are looked up before the custom loader. This
	// verifies the priority behavior on the exact go-quickjs version we pin.
	var loaderRequests []string
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		loaderRequests = append(loaderRequests, specifier)
		return NewLoader(nil, "", "").Load(specifier, referrer)
	})
	if _, err := rt.EvalModule(filepath.Join(t.TempDir(), "main.js"), `import path from "node:path"; globalThis.nativeBasename = path.basename("/a/b.js");`); err != nil {
		t.Fatalf("native module import: %v", err)
	}
	for _, request := range loaderRequests {
		if request == "node:path" || request == "path" {
			t.Fatalf("loader intercepted native module %q", request)
		}
	}
	basename, err := rt.Get("nativeBasename")
	if err != nil || basename.String() != "b.js" {
		t.Fatalf("native basename = %v, %v", basename, err)
	}
}

func TestBuiltinCollisionRejectedBeforeModuleLoading(t *testing.T) {
	names := make([]string, 0, len(standardLibraryBareModules))
	for name := range standardLibraryBareModules {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{"imports": map[string]string{name: "./collision.js"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = Parse(body, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), "standard-library module name is reserved") {
				t.Fatalf("Parse collision error = %v", err)
			}
		})
	}
	for _, name := range []string{"node:unknown-native", "noe:agent/internal", "noe:unknown"} {
		body, _ := json.Marshal(map[string]any{"imports": map[string]string{name: "./collision.js"}})
		if _, err := Parse(body, t.TempDir()); err == nil {
			t.Errorf("Parse(%q) unexpectedly succeeded", name)
		}
	}
}

func assertGlobalBool(t *testing.T, rt *quickjs.Runtime, name string, want bool) {
	t.Helper()
	value, err := rt.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Bool(); got != want {
		t.Fatalf("global %s = %v, want %v", name, got, want)
	}
}

func assertGlobalNumber(t *testing.T, rt *quickjs.Runtime, name string, want int) {
	t.Helper()
	value, err := rt.Get(name)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.Int(); got != want {
		t.Fatalf("global %s = %d, want %d", name, got, want)
	}
}

func testFileURL(t *testing.T, path string) string {
	t.Helper()
	path = filepath.ToSlash(path)
	if len(path) >= 2 && path[1] == ':' {
		path = "/" + path
	}
	return (&url.URL{Scheme: "file", Path: path}).String()
}

func quoteJS(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
