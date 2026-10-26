package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestURLImportCLIUsesDefaultProviderAndMap(t *testing.T) {
	var entries, deps atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/entry":
			entries.Add(1)
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			fmt.Fprint(w, `export {answer} from "./dep.data";`)
		case "/dep.data":
			deps.Add(1)
			w.Header().Set("Content-Type", "application/javascript")
			fmt.Fprint(w, `export const answer=42;`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir := t.TempDir()
	mapping, err := json.Marshal(map[string]any{"imports": map[string]string{
		"remote": server.URL + "/entry", "unused": server.URL + "/must-not-fetch",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "map.json"), mapping, 0600); err != nil {
		t.Fatal(err)
	}
	source := fmt.Sprintf(`import * as one from "remote"; export {answer} from "remote";
const two=await import(%q); console.log(one.answer,one===two);`, server.URL+"/entry")
	if err := os.WriteFile(filepath.Join(dir, "main.mjs"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for _, args := range [][]string{
		{"run", "--import-map", "map.json", "main.mjs"},
		{"eval", "--import-map", "map.json", source},
		{"eval", fmt.Sprintf(`const lib=await import(%q);console.log(lib.answer,true);`, server.URL+"/entry")},
	} {
		var out, errOut bytes.Buffer
		code := Run(ctx, args, strings.NewReader(""), &out, &errOut)
		if code != 0 || out.String() != "42 true\n" || errOut.Len() != 0 {
			t.Fatalf("args=%v code=%d stdout=%q stderr=%q", args, code, &out, &errOut)
		}
	}
	if entries.Load() != 3 || deps.Load() != 3 {
		t.Fatalf("requests entry=%d dependency=%d; want fresh source per runtime", entries.Load(), deps.Load())
	}
	var out, errOut bytes.Buffer
	if code := Run(ctx, []string{"run", server.URL + "/entry"}, strings.NewReader(""), &out, &errOut); code != 1 {
		t.Fatalf("URL entry unexpectedly accepted: code=%d stdout=%q stderr=%q", code, &out, &errOut)
	}
	if entries.Load() != 3 {
		t.Fatal("CLI URL entry made an HTTP request")
	}
}
