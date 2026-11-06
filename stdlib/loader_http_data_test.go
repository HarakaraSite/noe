package stdlib_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestHTTPDataImportsShareRawSourceAcrossTypesAndRedirects(t *testing.T) {
	var mu sync.Mutex
	reads := map[string]int{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reads[r.URL.Path]++
		count := reads[r.URL.Path]
		mu.Unlock()
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/settings.json", http.StatusFound)
		case "/settings.json":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			if count > 1 {
				fmt.Fprint(w, `{"value":99}`)
				return
			}
			fmt.Fprint(w, `{"value":0,"value":42,"label":"日本語"}`)
		case "/payload":
			w.Header().Set("Content-Type", "image/png")
			w.Write([]byte{0xef, 0xbb, 0xbf, 0, 0xff, 0x80, 10})
		case "/template":
			w.Header()["Content-Type"] = nil
			fmt.Fprint(w, "\ufeffhello 日本語")
		case "/script":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, "\ufeffexport const marker=42;")
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	profile := stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
		Imports: map[string]string{"settings": server.URL + "/redirect", "remote/": server.URL + "/"},
		HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
	})}
	result := executeURLTest(t, profile, `
import settings from "settings" with {type:"json"};
import * as alias from "remote/settings.json" with {type:"json"};
import text from "remote/settings.json" with {type:"text"};
import jsonBytes from "remote/settings.json" with {type:"bytes"};
import payload from "remote/payload" with {type:"bytes"};
import template from "remote/template" with {type:"text"};
import {marker} from "remote/script";
const again=await import("settings",{with:{type:"json"}});
if(again!==alias||alias.default!==settings||settings.value!==42||settings.label!=="日本語")throw Error("JSON namespace or parsing");
if(text!=='{"value":0,"value":42,"label":"日本語"}'||new TextDecoder().decode(jsonBytes)!==text||!jsonBytes.buffer.immutable)throw Error("shared JSON/text/bytes source");
if(!payload.buffer.immutable||Array.from(payload).join()!=="239,187,191,0,255,128,10")throw Error("HTTP payload was modified");
if(template!=="\ufeffhello 日本語"||marker!==42||typeof fetch!=="undefined")throw Error("BOM, MIME, or capability handling");
export default settings.value;`)
	if string(result.Data) != "42" {
		t.Fatalf("HTTP data result=%s", result.Data)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/redirect", "/settings.json", "/payload", "/template", "/script"} {
		if reads[path] != 1 {
			t.Errorf("requests[%q]=%d; each resource should be loaded once", path, reads[path])
		}
	}
}

func TestHTTPDataImportsUseEngineParsingAndPreserveBOMAfterFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, "\ufeff{\"value\":42}")
	}))
	defer server.Close()
	profile := stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
		Imports: map[string]string{"data": server.URL},
		HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
	})}
	executeURLTest(t, profile, `
let error;try{await import("data",{with:{type:"json"}})}catch(value){error=value}
if(!(error instanceof SyntaxError))throw Error("BOM-prefixed JSON must use engine parsing");
const text=await import("data",{with:{type:"text"}});
const bytes=await import("data",{with:{type:"bytes"}});
if(text.default!=='\ufeff{"value":42}'||Array.from(bytes.default).slice(0,3).join()!=="239,187,191")throw Error("failed JSON import changed retained source");`)
}

func TestHTTPDataMIMEDoesNotSelectAnImportType(t *testing.T) {
	for _, body := range []string{`{"value":42}`, `{"value":`} {
		t.Run(body, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			provider := stdlib.ModuleLoader(stdlib.LoaderOptions{HTTP: &stdlib.ModuleHTTPOptions{Client: server.Client()}})
			code := fmt.Sprintf(`import data from %q;`, server.URL)
			if strings.HasSuffix(body, ":") {
				code = fmt.Sprintf(`import data from %q with {type:"json"};`, server.URL)
			}
			_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Code: code, Module: true}, stdlib.Profile{Loader: provider})
			if err == nil || !strings.Contains(stdlib.Diagnostic(err), "SyntaxError") || strings.Contains(err.Error(), "Content-Type") {
				t.Fatalf("engine parse error=%s", stdlib.Diagnostic(err))
			}
		})
	}
}
