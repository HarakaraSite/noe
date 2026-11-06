package stdlib_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

const publicMappedBundle = "\"use strict\";\nfunction compiled(){\n throw new Error(\"public map marker\");\n}\ncompiled();\n//# sourceMappingURL=maps/bundle.map"
const publicSourceMap = `{"version":3,"sources":["../../src/original.ts"],"names":["original"],"mappings":";;AAKAA"}`

func TestPublicAttachSourceMapsPreservesCallerVMOptions(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "caller-disabled"
		if enabled {
			name = "caller-enabled"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.MkdirAll(filepath.Join(root, "dist", "maps"), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "dist", "bundle.js"), []byte(publicMappedBundle), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "dist", "maps", "bundle.map"), []byte(publicSourceMap), 0o600); err != nil {
				t.Fatal(err)
			}
			var bindings *stdlib.Bindings
			var options []quickjs.Option
			if enabled {
				options = append(options, quickjs.WithSourceMaps(func(script, reference string) ([]byte, string, error) {
					if bindings == nil {
						return nil, "", errors.New("bindings are not attached")
					}
					return bindings.ReadSourceMap(script, reference)
				}))
			}
			rt := quickjs.New(options...)
			loop := stdlib.NewLoop(rt)
			rt.OnClose(loop.Close)
			defer rt.Close()
			var err error
			bindings, err = stdlib.Attach(rt, loop, stdlib.Profile{
				Environment: stdlib.Environment{Cwd: "/dist"},
				Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}},
				NewLoader:   stdlib.FileLoader(nil),
			})
			if err != nil {
				t.Fatal(err)
			}
			defer bindings.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			source, err := bindings.Prepare(stdlib.Source{Name: "/main.mjs", Module: true, Code: `
try{await import("/dist/bundle.js")}catch(error){globalThis.capturedMapStack=error.stack}
`})
			if err != nil {
				t.Fatal(err)
			}
			entry, err := rt.EvalModuleContext(ctx, source.Name, source.Code)
			if err == nil {
				err = loop.Run(ctx)
			}
			if err == nil {
				_, err = entry.Await(ctx)
			}
			if err != nil {
				t.Fatal(err)
			}
			stack, err := rt.Get("capturedMapStack")
			if err != nil {
				t.Fatal(err)
			}
			want := "/dist/bundle.js:3:"
			if enabled {
				want = "/src/original.ts:6:1"
			}
			if !strings.Contains(stack.String(), want) || strings.Contains(stack.String(), root) {
				t.Fatalf("caller source-map option lost: %s", stack.String())
			}
			if err := bindings.Close(); err != nil {
				t.Fatal(err)
			}
			if _, _, err := bindings.ReadSourceMap("/dist/bundle.js", "maps/bundle.map"); err == nil {
				t.Fatal("closed binding read a sidecar")
			}
			value, err := rt.Eval(`40+2`)
			if err != nil || value.String() != "42" || loop.Context().Err() != nil {
				t.Fatalf("mapping cleanup closed borrowed VM/loop: %v %v", value, err)
			}
		})
	}
}

func TestPublicRemoteSourceMapsUseInlineDataWithoutExtraRequests(t *testing.T) {
	for _, inline := range []bool{false, true} {
		name := "remote-sidecar-fallback"
		if inline {
			name = "remote-inline"
		}
		t.Run(name, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/javascript")
				code := publicMappedBundle
				if inline {
					code = strings.Replace(code, "maps/bundle.map", "data:application/json;base64,"+base64.StdEncoding.EncodeToString([]byte(`{"version":3,"sources":["original.ts"],"mappings":";;AAKAA"}`)), 1)
				}
				_, _ = io.WriteString(w, code)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[remote source maps]", Virtual: true, Module: true, Code: `await import(` + strconv.Quote(server.URL+"/dist/bundle.js") + `);`}, stdlib.Profile{
				Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{Client: server.Client()}}),
			})
			want := server.URL + "/dist/bundle.js:3:"
			if inline {
				want = server.URL + "/dist/original.ts:6:1"
			}
			if result.Code != 1 || err == nil || !strings.Contains(stdlib.Diagnostic(err), want) || requests.Load() != 1 {
				t.Fatalf("remote map requests=%d result=%+v error=%s", requests.Load(), result, stdlib.Diagnostic(err))
			}
		})
	}
}
