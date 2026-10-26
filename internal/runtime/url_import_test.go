package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

func TestURLImportWorkerEntryStaysLocalWhileDependenciesUseHTTP(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		if r.URL.Path == "/library" {
			fmt.Fprint(w, `export {value} from "./dependency";`)
		} else {
			fmt.Fprint(w, `export const value=42;`)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	worker := `import {value} from "remote/lib";postMessage(value);close();`
	if err := os.WriteFile(filepath.Join(dir, "worker.mjs"), []byte(worker), 0600); err != nil {
		t.Fatal(err)
	}
	mapping, err := json.Marshal(map[string]any{"imports": map[string]string{
		"local-entry": "./worker.mjs", "remote/lib": server.URL + "/library", "remote-entry": server.URL + "/entry",
	}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := modulemap.Parse(mapping, dir)
	if err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`
const value=await new Promise((resolve,reject)=>{
  const worker=new Worker("local-entry",{type:"module"});
  worker.onmessage=event=>resolve(event.data);
  worker.onerror=event=>{event.preventDefault();reject(Error(event.message))};
});
if(value!==42)throw Error("URL dependency failed");
for(const entry of [%q,"remote-entry"]){
  const message=await new Promise((resolve,reject)=>{
    const worker=new Worker(entry,{type:"module"});
    worker.onmessage=()=>reject(Error("remote Worker entry executed"));
    worker.onerror=event=>{event.preventDefault();resolve(event.message)};
  });
  if(!String(message).includes("Worker entry URLs are unsupported"))throw Error(String(message));
}
console.log("local entry and URL dependency");
`, server.URL+"/entry")
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := Execute(ctx, Source{filepath.Join(dir, "main.mjs"), source}, Options{Cwd: dir, Stdout: &out, ImportMap: snapshot})
	if code != 0 || err != nil || out.String() != "local entry and URL dependency\n" {
		t.Fatalf("code=%d err=%v stdout=%q", code, err, &out)
	}
	if requests.Load() != 2 {
		t.Fatalf("HTTP requests=%d; URL Worker entries must not connect", requests.Load())
	}
}
