package stdlib_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestDataImportsUseSelectedFilesystemScopesAndWorker(t *testing.T) {
	root := t.TempDir()
	workspace := filepath.Join(root, "workspace")
	if err := os.MkdirAll(filepath.Join(workspace, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"assets/settings.json": []byte(`{"value":0,"value":41}`),
		"assets/worker.json":   []byte(`{"value":42}`),
		"assets/template":      []byte("\ufeffhello 日本語\n"),
		"assets/payload.bin":   {0xef, 0xbb, 0xbf, 0, 0xff, 0x80, 10},
		"worker.mjs": []byte(`import {parentPort} from "node:worker_threads";
import data from "settings" with {type:"json"};
import bytes from "assets/payload.bin" with {type:"bytes"};
if(!bytes.buffer.immutable||bytes[4]!==255)throw Error("worker bytes");
parentPort.postMessage(data.value);parentPort.close();`),
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(workspace, filepath.FromSlash(name)), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	imports := map[string]string{"settings": "./assets/settings.json", "assets/": "./assets/"}
	scopes := map[string]map[string]string{"./worker.mjs": {"settings": "./assets/worker.json"}}
	provider := stdlib.ModuleLoader(stdlib.LoaderOptions{Imports: imports, Scopes: scopes})
	imports["assets/"] = "./missing/"
	scopes["./worker.mjs"]["settings"] = "./missing.json"
	scopes["./"] = map[string]string{"settings": "./missing.json"}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: "/workspace"},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}, Workers: &stdlib.WorkerOptions{Max: 1}},
		Loader:      provider,
	}
	code := `import data from "settings" with {type:"json"};
import * as alias from "./assets/settings.json" with {type:"json"};
import text from "assets/template" with {type:"text"};
import bytes from "assets/payload.bin" with {type:"bytes"};
import {Worker} from "node:worker_threads";
const dynamic=await import("assets/settings.json",{with:{type:"json"}});
const jsonText=await import("settings",{with:{type:"text"}});
const jsonBytes=await import("settings",{with:{type:"bytes"}});
if(alias!==dynamic||alias.default!==data||Object.keys(alias).join()!=="default")throw Error("JSON identity or exports");
if(data.value!==41||JSON.parse(jsonText.default).value!==41||jsonBytes.default[0]!==123)throw Error("independent import types");
if(text!=="\ufeffhello 日本語\n")throw Error("text source changed");
if(!(bytes instanceof Uint8Array)||!bytes.buffer.immutable||Array.from(bytes).join()!=="239,187,191,0,255,128,10")throw Error("raw byte source changed");
data.shared=7;if(dynamic.default.shared!==7)throw Error("JSON object identity");
const worker=new Worker("./worker.mjs",{type:"module"});
const answer=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();if(answer!==42)throw Error("worker scope or typed import");
export default [data.value,answer];`
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "/workspace/main.mjs", Code: code, Module: true}, profile)
	if err != nil || string(result.Data) != "[41,42]" {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestFileLoaderUsesImportTypeInsteadOfExtension(t *testing.T) {
	root := t.TempDir()
	for name, code := range map[string]string{
		"data": `{"answer":42}`, "invalid.json": `{"answer":`,
		"module.txt": `export const answer=42;`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(code), 0600); err != nil {
			t.Fatal(err)
		}
	}
	profile := stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}}, NewLoader: stdlib.FileLoader(map[string]string{"data": "./data", "modules/": "./"})}
	for _, test := range []struct{ name, code, want string }{
		{"extensionless JSON", `import data from "data" with {type:"json"};export default data.answer;`, "42"},
		{"JavaScript text suffix", `import {answer} from "modules/module.txt";export default answer;`, "42"},
		{"invalid JSON", `import data from "./invalid.json" with {type:"json"};`, "SyntaxError"},
		{"missing import type", `import data from "data";`, "SyntaxError"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "[data imports]", Virtual: true, Module: true, Code: test.code}, profile)
			if test.want == "42" {
				if err != nil || string(result.Data) != test.want {
					t.Fatalf("data=%s error=%s", result.Data, stdlib.Diagnostic(err))
				}
			} else if err == nil || !strings.Contains(stdlib.Diagnostic(err), test.want) {
				t.Fatalf("error=%s, want %s", stdlib.Diagnostic(err), test.want)
			}
		})
	}
	_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "entry.mjs", Module: true, Code: `import data from "./data" with {type:"bytes"};`}, stdlib.Profile{NewLoader: stdlib.FileLoader(nil)})
	if err == nil || !strings.Contains(err.Error(), "filesystem is unavailable") {
		t.Fatalf("typed import granted filesystem: %v", err)
	}
}

func TestHTTPImportsUseFinalReferrerScope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/pkg/main", http.StatusFound)
		case "/pkg/main":
			fmt.Fprint(w, `import {answer} from "library/value";export {answer};`)
		case "/v2/value":
			fmt.Fprint(w, `export const answer=42;`)
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	provider := stdlib.ModuleLoader(stdlib.LoaderOptions{
		Imports: map[string]string{"library/": server.URL + "/v1/"},
		Scopes:  map[string]map[string]string{server.URL + "/pkg/": {"library/": server.URL + "/v2/"}},
		HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
	})
	result := executeURLTest(t, stdlib.Profile{Loader: provider}, fmt.Sprintf(`import {answer} from %q;export default answer;`, server.URL+"/redirect"))
	if string(result.Data) != "42" {
		t.Fatalf("scoped remote data=%s", result.Data)
	}
	local := stdlib.ModuleLoader(stdlib.LoaderOptions{Scopes: map[string]map[string]string{"./": {"unused": server.URL}}})
	_, err := local.NewLoader(t.Context(), stdlib.Origin{Cwd: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "unsupported by the local module loader") {
		t.Fatalf("scoped HTTP target silently accepted without HTTP: %v", err)
	}
}
