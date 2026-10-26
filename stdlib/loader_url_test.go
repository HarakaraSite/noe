package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func executeURLTest(t *testing.T, profile stdlib.Profile, code string) stdlib.Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[URL fixture]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return result
}

func TestModuleLoaderURLGraphRedirectIdentityAndCycle(t *testing.T) {
	var mainReads atomic.Int32
	var mu sync.Mutex
	seen := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.RequestURI]++
		mu.Unlock()
		w.Header().Set("Content-Type", "text/javascript")
		switch r.URL.Path {
		case "/redirect", "/other":
			http.Redirect(w, r, "/pkg/main?variant=1", http.StatusFound)
		case "/cycle":
			http.Redirect(w, r, "/pkg/left", http.StatusFound)
		case "/pkg/main":
			if mainReads.Add(1) > 1 {
				// A previously unknown alias must not replace retained source.
				fmt.Fprint(w, "throw Error('replacement source evaluated');")
				return
			}
			fmt.Fprint(w, `import {value} from "./dep.ts?b=2&a=1";import {extra} from "/origin";export {combined} from "./left";globalThis.mainRuns=(globalThis.mainRuns||0)+1;export const answer=value+extra;export const marker={};export const runs=mainRuns;`)
		case "/pkg/dep.ts":
			fmt.Fprint(w, `export {value} from "./leaf";`)
		case "/pkg/leaf":
			w.Header().Set("Content-Type", "Text/Plain; charset=utf-8")
			fmt.Fprint(w, "\xef\xbb\xbfexport const value=40;")
		case "/origin":
			fmt.Fprint(w, `export const extra=2;`)
		case "/pkg/left":
			fmt.Fprint(w, `import {right} from "./right";export function left(){return 1}export function combined(){return left()+right()}`)
		case "/pkg/right":
			fmt.Fprint(w, `import {left} from "/cycle";export function right(){return left()+1}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	profile := stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
		Imports: map[string]string{"library": server.URL + "/redirect", "unknown-alias": server.URL + "/other"},
		HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
	})}
	code := fmt.Sprintf(`import * as alias from "library";import * as requested from %q;import * as final from %q;
const dynamic=await import("unknown-alias");
if(alias!==requested||alias!==final||alias!==dynamic)throw Error("namespace identity");
if(alias.answer!==42||alias.combined()!==3||alias.runs!==1)throw Error("graph or evaluation");
export default {answer:alias.answer,cycle:alias.combined(),runs:alias.runs};`, server.URL+"/redirect", server.URL+"/pkg/main?variant=1")
	result := executeURLTest(t, profile, code)
	if string(result.Data) != `{"answer":42,"cycle":3,"runs":1}` {
		t.Fatalf("data=%s", result.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	for target, count := range map[string]int{
		"/redirect": 1, "/other": 1, "/pkg/main?variant=1": 2,
		"/pkg/dep.ts?b=2&a=1": 1, "/pkg/leaf": 1, "/origin": 1,
		"/pkg/left": 2, "/pkg/right": 1, "/cycle": 1,
	} {
		if seen[target] != count {
			t.Errorf("requests[%q]=%d want %d; all=%v", target, seen[target], count, seen)
		}
	}
}

func TestModuleLoaderFragmentsAndQueriesAreModuleIDs(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if strings.Contains(r.RequestURI, "#") {
			t.Error("fragment sent in HTTP request target")
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprint(w, `globalThis.evaluations=(globalThis.evaluations||0)+1;export const value=evaluations;`)
	}))
	defer server.Close()
	code := fmt.Sprintf(`import * as a from %q;import * as b from %q;import * as c from %q;import * as empty from %q;import * as plain from %q;
if(a===b||b===c||empty!==plain)throw Error("module IDs");export default [a.value,b.value,c.value,empty.value,plain.value];`,
		server.URL+"/m?q=1#one", server.URL+"/m?q=1#two", server.URL+"/m?q=2#one", server.URL+"/m#", server.URL+"/m")
	result := executeURLTest(t, stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{}})}, code)
	if string(result.Data) != "[1,2,3,4,4]" || requests.Load() != 4 {
		t.Fatalf("data=%s requests=%d", result.Data, requests.Load())
	}
}

func TestModuleLoaderOptionsSnapshotAndFreshRuntimeSource(t *testing.T) {
	var value, requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "export const value=%d;", value.Load())
	}))
	defer server.Close()
	imports := map[string]string{"library": server.URL + "/module", "unused": server.URL + "/never"}
	options := &stdlib.ModuleHTTPOptions{Client: server.Client(), MaxBodyBytes: -1}
	provider := stdlib.ModuleLoader(stdlib.LoaderOptions{Imports: imports, HTTP: options})
	imports["library"] = "http:invalid"
	options.Client = &http.Client{}
	options.MaxBodyBytes = 1
	options.Allow = func(*http.Request) error { return errors.New("mutated option") }
	profile := stdlib.Profile{Loader: provider}
	for _, want := range []int32{1, 2} {
		value.Store(want)
		result := executeURLTest(t, profile, `import * as a from "library";const b=await import("library");if(a!==b)throw Error("namespace");export default a.value;`)
		if string(result.Data) != fmt.Sprint(want) {
			t.Fatalf("new runtime data=%s want %d", result.Data, want)
		}
	}
	if requests.Load() != 2 {
		t.Fatalf("requests=%d; unused maps must not connect, runtimes must refetch", requests.Load())
	}
}

func TestModuleLoaderFSAndFetchStayIndependent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, `export const value=42;`)
	}))
	defer server.Close()
	profile := stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{}})}
	executeURLTest(t, profile, fmt.Sprintf(`import {value} from %q;if(value!==42||typeof fetch!=="undefined")throw Error("HTTP/Fetch separation");`, server.URL))
	_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: `import "./missing.mjs";`}, profile)
	if err == nil || !strings.Contains(err.Error(), "filesystem is unavailable") {
		t.Fatalf("HTTP provider granted FS: %v", err)
	}
	for _, legacy := range []struct {
		factory stdlib.LoaderFactory
		code    string
	}{
		{stdlib.FileLoader(nil), fmt.Sprintf("import %q;", server.URL)},
		{stdlib.FileLoader(map[string]string{"unused": server.URL}), "export {};"},
	} {
		_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: legacy.code}, stdlib.Profile{
			Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}, NewLoader: legacy.factory,
		})
		if err == nil || !strings.Contains(err.Error(), "HTTP/HTTPS") {
			t.Fatalf("Fetch silently enabled FileLoader: %v", err)
		}
	}
	disabled := stdlib.ModuleLoader(stdlib.LoaderOptions{})
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: fmt.Sprintf("import %q;", server.URL)}, stdlib.Profile{Loader: disabled})
	if err == nil || !strings.Contains(err.Error(), "HTTP/HTTPS") {
		t.Fatalf("HTTP nil enabled URL import: %v", err)
	}

	// Selected local FS remains available to remote modules, without origin
	// restrictions or authority being granted by the HTTP provider itself.
	dir := t.TempDir()
	local := filepath.Join(dir, "local.mjs")
	if err := os.WriteFile(local, []byte("export const value=7;"), 0600); err != nil {
		t.Fatal(err)
	}
	filePath := filepath.ToSlash(local)
	if len(filePath) >= 2 && filePath[1] == ':' {
		filePath = "/" + filePath
	}
	fileID := (&url.URL{Scheme: "file", Path: filePath}).String()
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		fmt.Fprintf(w, "export {value} from %q;", fileID)
	}))
	defer remote.Close()
	withFS := stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}}, Loader: profile.Loader}
	result := executeURLTest(t, withFS, fmt.Sprintf("import {value} from %q;export default value;", remote.URL))
	if string(result.Data) != "7" {
		t.Fatalf("remote to selected FS data=%s", result.Data)
	}
}

func TestModuleLoaderRemoteSourceDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, code, want string }{
		{"syntax", "export const = ;", "SyntaxError"},
		{"evaluation", "throw new Error('remote evaluation sentinel');", "remote evaluation sentinel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/start" {
					w.Header().Set("Location", "/source")
					w.WriteHeader(302)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				fmt.Fprint(w, tc.code)
			}))
			defer server.Close()
			_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: fmt.Sprintf("import %q;", server.URL+"/start")}, stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{}})})
			diagnostic := stdlib.Diagnostic(err)
			if err == nil || !strings.Contains(diagnostic, tc.want) || !strings.Contains(diagnostic, server.URL+"/source") {
				t.Fatalf("diagnostic=%s error=%v", diagnostic, err)
			}
		})
	}
}
