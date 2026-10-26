package stdlib_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// These expectations use only public APIs and net/url, not the implementation's
// metadata/path helpers.
func importMetaFileURL(name string) string {
	name = filepath.ToSlash(name)
	if len(name) > 1 && name[1] == ':' {
		name = "/" + name
	}
	return (&url.URL{Scheme: "file", Path: name}).String()
}

func importMetaWrite(t *testing.T, name, code string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
}

func importMetaExecute(t *testing.T, source stdlib.Source, profile stdlib.Profile, want any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, source, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	expected, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(result.Data, &gotValue); err != nil {
		t.Fatalf("data=%s: %v", result.Data, err)
	}
	if err := json.Unmarshal(expected, &wantValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("data=%s want %s", result.Data, expected)
	}
}

func TestImportMetaLocalModuleRelativeAssetsAndIdentity(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "working directory")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	special := "日本語 space #%"
	if runtime.GOOS != "windows" {
		special += "?"
	}
	dir := filepath.Join(base, special)
	entry := filepath.Join(dir, "entry.mjs")
	dep := filepath.Join(dir, "nested", "dep.mjs")
	leaf := filepath.Join(dir, "nested", "leaf.mjs")
	asset := filepath.Join(dir, "nested", "asset 日本語 #%.txt")
	importMetaWrite(t, asset, "module asset")
	importMetaWrite(t, filepath.Join(cwd, "asset 日本語 #%.txt"), "wrong cwd asset")
	importMetaWrite(t, leaf, "export const url=import.meta.url;")
	// node:url currently returns a slash-prefixed drive pathname on Windows;
	// adapt that existing API rather than testing a new fs URL-input contract.
	importMetaWrite(t, dep, `import fs from "node:fs";
import {fileURLToPath} from "node:url";
import {url as leaf} from "./leaf.mjs";
export const url=import.meta.url;
export const asset=new URL("./asset%20%E6%97%A5%E6%9C%AC%E8%AA%9E%20%23%25.txt",import.meta.url).href;
let path=fileURLToPath(asset);
if(/^\/[A-Za-z]:\//.test(path))path=path.slice(1);
export const text=fs.readFileSync(path,"utf8");
export {leaf};`)
	code := `import * as direct from "./nested/dep.mjs";
import * as alias from "dependency";
const dynamic=await import("dependency");
if(direct!==alias||direct!==dynamic)throw Error("local module identity");
if(import.meta!==import.meta)throw Error("metadata identity");
export default {entry:import.meta.url,dep:direct.url,leaf:direct.leaf,asset:direct.asset,text:direct.text,keys:Object.keys(import.meta)};`
	importMetaWrite(t, entry, code)
	importMetaExecute(t, stdlib.Source{Name: entry, Module: true, Code: code}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: cwd},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}},
		Loader:      stdlib.ModuleLoader(stdlib.LoaderOptions{Imports: map[string]string{"dependency": dep}}),
	}, map[string]any{
		"entry": importMetaFileURL(entry), "dep": importMetaFileURL(dep), "leaf": importMetaFileURL(leaf),
		"asset": importMetaFileURL(asset), "text": "module asset", "keys": []string{"url"},
	})
}

func TestImportMetaHTTPRedirectCanonicalURL(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(fmt.Sprintf("TLS=%t", tls), func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/javascript")
				switch r.URL.Path {
				case "/start":
					http.Redirect(w, r, "/pkg/main.mjs?q=a%20b&v=1#section", http.StatusFound)
				case "/pkg/main.mjs":
					fmt.Fprint(w, `import {url as dep} from "./dep.mjs?kind=one#leaf";
export const url=import.meta.url;
export const asset=new URL("./asset.txt",import.meta.url).href;
export {dep};`)
				case "/pkg/dep.mjs":
					fmt.Fprint(w, "export const url=import.meta.url;")
				case "/pkg/asset.txt":
					fmt.Fprint(w, "remote asset")
				default:
					http.NotFound(w, r)
				}
			})
			var server *httptest.Server
			if tls {
				server = httptest.NewTLSServer(handler)
			} else {
				server = httptest.NewServer(handler)
			}
			defer server.Close()
			canonical := server.URL + "/pkg/main.mjs?q=a%20b&v=1#section"
			code := fmt.Sprintf(`import * as alias from "remote";
import * as requested from %q;
import * as canonical from %q;
const dynamic=await import("remote");
const other=await import(%q);
if(alias!==requested||alias!==canonical||alias!==dynamic||alias===other)throw Error("remote identity");
export default {url:alias.url,dep:alias.dep,other:other.url,asset:alias.asset,text:await (await fetch(alias.asset)).text()};`,
				server.URL+"/start", canonical, server.URL+"/pkg/main.mjs?q=a%20b&v=1#other")
			importMetaExecute(t, stdlib.Source{Name: "[HTTP metadata]", Virtual: true, Module: true, Code: code}, stdlib.Profile{
				Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Client: server.Client()}},
				Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
					Imports: map[string]string{"remote": server.URL + "/start"},
					HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
				}),
			}, map[string]string{
				"url": canonical, "dep": server.URL + "/pkg/dep.mjs?kind=one#leaf",
				"other": server.URL + "/pkg/main.mjs?q=a%20b&v=1#other",
				"asset": server.URL + "/pkg/asset.txt", "text": "remote asset",
			})
		})
	}
}

func TestImportMetaEmbeddingVirtualAndRootedFilesystem(t *testing.T) {
	for _, rooted := range []bool{false, true} {
		for _, virtual := range []bool{false, true} {
			t.Run(fmt.Sprintf("rooted=%t/virtual=%t", rooted, virtual), func(t *testing.T) {
				root := t.TempDir()
				physicalCwd := filepath.Join(root, "app")
				importMetaWrite(t, filepath.Join(physicalCwd, "dep.mjs"), "export const url=import.meta.url;")
				importMetaWrite(t, filepath.Join(physicalCwd, "asset.txt"), "embedded asset")
				cwd := physicalCwd
				fs := &stdlib.FileSystemOptions{ReadOnly: true}
				if rooted {
					cwd, fs.Root = "/app", root
				}
				rt := quickjs.New()
				loop := stdlib.NewLoop(rt)
				rt.OnClose(loop.Close)
				defer rt.Close()
				view, err := stdlib.Attach(rt, loop, stdlib.Profile{
					Environment: stdlib.Environment{Cwd: cwd},
					Modules:     stdlib.Modules{FS: fs},
					NewLoader:   stdlib.FileLoader(nil),
				})
				if err != nil {
					t.Fatal(err)
				}
				defer view.Close()
				name := "entry.mjs"
				if virtual {
					name = "diagnostic:virtual entry"
				}
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				source, err := view.Prepare(stdlib.Source{Name: name, Virtual: virtual, Module: true, Code: `import {url as dep} from "./dep.mjs";
import fs from "node:fs";
import {fileURLToPath} from "node:url";
const asset=new URL("./asset.txt",import.meta.url);
let path=fileURLToPath(asset);
if(/^\/[A-Za-z]:\//.test(path))path=path.slice(1);
globalThis.metadata=JSON.stringify({url:import.meta.url,dep,asset:asset.href,text:fs.readFileSync(path,"utf8")});`})
				if err != nil {
					t.Fatal(err)
				}
				if virtual && source.Name != name {
					t.Fatalf("virtual diagnostic label=%q want %q", source.Name, name)
				}
				entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
				if err != nil {
					t.Fatal(err)
				}
				if err := loop.Run(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Await(ctx); err != nil {
					t.Fatal(err)
				}
				value, err := rt.Get("metadata")
				if err != nil {
					t.Fatal(err)
				}
				entryName := "entry.mjs"
				if virtual {
					entryName = "<virtual-entry>.mjs"
				}
				// Rooted filesystems expose slash-only virtual paths on all OSes.
				base := importMetaFileURL(cwd)
				if rooted {
					base = "file:///app"
				}
				entryURL := base + "/" + url.PathEscape(entryName)
				// net/url leaves some path-safe characters unescaped, notably
				// angle brackets must still be encoded in the virtual filename.
				want, _ := json.Marshal(map[string]string{
					"url": entryURL, "dep": base + "/dep.mjs",
					"asset": base + "/asset.txt", "text": "embedded asset",
				})
				var got, expected map[string]string
				if err := json.Unmarshal([]byte(value.String()), &got); err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(want, &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("metadata=%s want %s", value.String(), want)
				}
				if rooted && strings.Contains(value.String(), filepath.ToSlash(root)) {
					t.Fatalf("host root leaked in metadata: %s", value.String())
				}
			})
		}
	}
}

func TestImportMetaWorkersFileAndEval(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "nested", "child 日本語.mjs")
	dep := filepath.Join(dir, "dep.mjs")
	importMetaWrite(t, dep, "export const url=import.meta.url;")
	importMetaWrite(t, child, `import {parentPort} from "node:worker_threads";
import {url as dep} from "../dep.mjs";
parentPort.postMessage({url:import.meta.url,dep});`)
	eval := `import {parentPort} from "node:worker_threads";
import {url as dep} from "./dep.mjs";
parentPort.postMessage({url:import.meta.url,dep});`
	code := fmt.Sprintf(`import {Worker} from "node:worker_threads";
function message(worker){return new Promise((ok,fail)=>{worker.once("message",ok);worker.once("error",fail)})}
const file=new Worker("./nested/child 日本語.mjs",{type:"module"});
const fileResult=await message(file);
const evaluated=new Worker(%q,{eval:true,type:"module"});
export default {file:fileResult,eval:await message(evaluated)};`, eval)
	importMetaExecute(t, stdlib.Source{Name: "[worker parent]", Virtual: true, Module: true, Code: code}, stdlib.Profile{
		Environment: stdlib.Environment{Cwd: dir},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}, Workers: &stdlib.WorkerOptions{Max: -1}},
		NewLoader:   stdlib.FileLoader(nil),
	}, map[string]any{
		"file": map[string]string{"url": importMetaFileURL(child), "dep": importMetaFileURL(dep)},
		"eval": map[string]string{"url": importMetaFileURL(filepath.Join(dir, "<virtual-entry>.mjs")), "dep": importMetaFileURL(dep)},
	})
}

func TestImportMetaHostDefinedAndCustomLoaderIDs(t *testing.T) {
	file := filepath.Join(t.TempDir(), "host 日本語 #%.mjs")
	defined := []string{file, "https://example.invalid/defined.mjs?q=1#part", "owner module/日本語"}
	custom := []string{filepath.Join(filepath.Dir(file), "custom.mjs"), "urn:example:module?q=2#part", "custom opaque/日本語"}
	specifiers := append(append([]string{}, defined...), "custom-file", "custom-uri", "custom-opaque")
	input, _ := json.Marshal(specifiers)
	profile := stdlib.Profile{
		Initialize: func(_ context.Context, b *stdlib.Bindings) (func(), error) {
			for _, id := range defined {
				if err := b.DefineModule(id, "export default import.meta.url;"); err != nil {
					return nil, err
				}
			}
			return nil, nil
		},
		NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, referrer string) (string, string, error) {
				for i, name := range []string{"custom-file", "custom-uri", "custom-opaque"} {
					if specifier == name {
						return "export default import.meta.url;", custom[i], nil
					}
				}
				return "", "", fmt.Errorf("unexpected import %q from %q", specifier, referrer)
			}, nil
		},
	}
	importMetaExecute(t, stdlib.Source{Name: "[host modules]", Virtual: true, Module: true,
		Code: fmt.Sprintf("const urls=[];for(const id of %s)urls.push((await import(id)).default);export default urls;", input)}, profile,
		[]string{
			importMetaFileURL(defined[0]), defined[1], "noe:module/" + url.PathEscape(defined[2]),
			importMetaFileURL(custom[0]), custom[1], "noe:module/" + url.PathEscape(custom[2]),
		})
}

func TestImportMetaHostCanOverrideHook(t *testing.T) {
	install := func(rt *quickjs.Runtime) {
		rt.OnImportMeta(func(specifier string, meta quickjs.Value) {
			if err := meta.Set("url", "host:override"); err != nil {
				t.Error(err)
			}
			if err := meta.Set("owner", specifier); err != nil {
				t.Error(err)
			}
		})
	}
	t.Run("Initialize", func(t *testing.T) {
		importMetaExecute(t, stdlib.Source{Name: "[host override]", Virtual: true, Module: true,
			Code: "export default {url:import.meta.url,owner:import.meta.owner};"}, stdlib.Profile{
			Initialize: func(_ context.Context, b *stdlib.Bindings) (func(), error) {
				install(b.Runtime())
				return nil, nil
			},
		}, map[string]string{"url": "host:override", "owner": "[host override]"})
	})
	t.Run("AfterAttach", func(t *testing.T) {
		rt := quickjs.New()
		loop := stdlib.NewLoop(rt)
		rt.OnClose(loop.Close)
		defer rt.Close()
		view, err := stdlib.Attach(rt, loop, stdlib.Profile{})
		if err != nil {
			t.Fatal(err)
		}
		defer view.Close()
		install(rt)
		source, err := view.Prepare(stdlib.Source{Name: "[attached override]", Virtual: true, Module: true,
			Code: "globalThis.override=import.meta.url+'|'+import.meta.owner;"})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()
		entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
		if err != nil {
			t.Fatal(err)
		}
		if err := loop.Run(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Await(ctx); err != nil {
			t.Fatal(err)
		}
		value, err := rt.Get("override")
		if err != nil || value.String() != "host:override|[attached override]" {
			t.Fatalf("override=%q error=%v", value.String(), err)
		}
	})
}
