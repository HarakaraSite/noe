//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHTTPUpdateLifecycleThroughCLI(t *testing.T) {
	var mu sync.Mutex
	var value map[string]any
	var methods []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		methods = append(methods, r.Method)
		if r.Method == http.MethodDelete {
			value = nil
			w.WriteHeader(204)
			return
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("missing explicit JSON content type: %v", r.Header)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			http.Error(w, "bad JSON", 400)
			return
		}
		switch r.Method {
		case http.MethodPut:
			value = body
			w.WriteHeader(201)
		case http.MethodPatch:
			for key, item := range body {
				value[key] = item
			}
		default:
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
			return
		}
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	work := t.TempDir()
	script := filepath.Join(work, "lifecycle.js")
	writeFixture(t, script, `const url = noe.args[0];
const created = await fetch(url, {method:"put", headers:{"Content-Type":"application/json"}, body:JSON.stringify({name:"日本語",count:1})});
if (created.status !== 201 || !created.ok) throw new Error("PUT failed");
const updated = await fetch(url, {method:"PATCH", headers:{"Content-Type":"application/json"}, body:JSON.stringify({count:2})});
if (updated.status !== 200 || !updated.ok) throw new Error("PATCH failed");
const removed = await fetch(url, {method:"DELETE"});
console.log(JSON.stringify({created:await created.json(), updated:await updated.json(), removed:{status:removed.status,ok:removed.ok,text:await removed.text()}}));`)
	got := runCLI(t, work, nil, "run", script, server.URL)
	requireExit(t, got, 0)
	want := `{"created":{"count":1,"name":"日本語"},"updated":{"count":2,"name":"日本語"},"removed":{"status":204,"ok":true,"text":""}}` + "\n"
	if got.stdout != want || got.stderr != "" {
		t.Fatalf("HTTP lifecycle: %+v, want %q", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(methods, ",") != "PUT,PATCH,DELETE" || value != nil {
		t.Fatalf("server methods=%q remaining value=%v", methods, value)
	}
}

func TestHTTPMethodsPipedREPLAndUnawaitedCompletion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		_, _ = fmt.Fprintf(w, "%s:%s", r.Method, body)
	}))
	defer server.Close()
	url := strconv.Quote(server.URL)
	repl := runREPLInput(t, t.TempDir(), map[string]string{"NOE_REPL_HISTORY": ""}, strings.Join([]string{
		`const response = await fetch(` + url + `,{method:"patch",body:"雪"})`,
		`const text = await response.text()`,
		`console.log("REPL_PATCH", text)`,
		`await fetch(` + url + `,{method:"GET",body:""})`,
		`const removed = await fetch(` + url + `,{method:"DELETE"})`,
		`console.log("REPL_DELETE", removed.status)`,
		`console.log("REPL_CONTINUED")`,
	}, "\n")+"\n")
	requireExit(t, repl, 0)
	if !strings.Contains(repl.stdout, "REPL_PATCH PATCH:雪") || !strings.Contains(repl.stdout, "REPL_DELETE 204") || !strings.Contains(repl.stdout, "REPL_CONTINUED") || !strings.Contains(repl.stderr, "fetch body must be a string") {
		t.Fatalf("REPL HTTP contract/recovery: %+v", repl)
	}
	source := `fetch(` + url + `,{method:"PUT",body:"unawaited"}).then(r => r.text()).then(text => console.log(text)); console.log("returned");`
	got := runCLI(t, t.TempDir(), nil, "eval", source)
	requireExit(t, got, 0)
	if got.stdout != "returned\nPUT:unawaited\n" || got.stderr != "" {
		t.Fatalf("unawaited update did not finish: %+v", got)
	}
}

func TestHTTPPatchTimeoutCancelsRequest(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method=%s, want PATCH", r.Method)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || string(body) != "{}" {
			t.Errorf("PATCH body=%q err=%v", body, err)
		}
		close(started)
		select {
		case <-r.Context().Done():
			close(canceled)
		case <-release:
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	source := `console.log("STARTED"); await fetch(` + strconv.Quote(server.URL) + `,{method:"PATCH",body:"{}"}); console.log("AFTER");`
	got := runCLI(t, t.TempDir(), nil, "eval", "--timeout=200ms", source)
	requireExecutionTimeout(t, got)
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("PATCH request did not start")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout did not cancel PATCH")
	}
}

func TestHTTPJSONExampleSendsUpdatesAndHandlesResponses(t *testing.T) {
	type request struct {
		Method      string
		Body        string
		ContentType string
	}
	observed := make(chan request, 8)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		observed <- request{r.Method, string(body), r.Header.Get("Content-Type")}
		if r.URL.Path == "/conflict" {
			w.WriteHeader(409)
			_, _ = io.WriteString(w, "conflict")
			return
		}
		if r.Method == http.MethodDelete {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(201)
		_, _ = w.Write(body)
	}))
	defer server.Close()
	example := filepath.Join(repoRootPath, "examples", "http-json.js")
	for _, method := range []string{"PUT", "patch", "DELETE"} {
		t.Run(method, func(t *testing.T) {
			args := []string{"run", example, method, server.URL}
			wantBody, contentType := "", ""
			want := `{"status":204,"ok":true,"body":null}` + "\n"
			if method != "DELETE" {
				args = append(args, `{"message": "日本語"}`)
				wantBody, contentType = `{"message":"日本語"}`, "application/json"
				want = `{"status":201,"ok":true,"body":{"message":"日本語"}}` + "\n"
			}
			got := runCLI(t, t.TempDir(), nil, args...)
			requireExit(t, got, 0)
			if got.stdout != want || got.stderr != "" {
				t.Fatalf("JSON example result: %+v, want %q", got, want)
			}
			request := <-observed
			if request.Method != strings.ToUpper(method) || request.Body != wantBody || request.ContentType != contentType {
				t.Fatalf("JSON example request: %+v", request)
			}
		})
	}
	failed := runCLI(t, t.TempDir(), nil, "run", example, "PUT", server.URL+"/conflict", "{}")
	requireExit(t, failed, 1)
	if failed.stdout != "" || !strings.Contains(failed.stderr, "HTTP 409: conflict") {
		t.Fatalf("JSON example HTTP failure: %+v", failed)
	}
	<-observed
	invalid := runCLI(t, t.TempDir(), nil, "run", example, "PUT", server.URL, "{")
	requireExit(t, invalid, 1)
	if invalid.stdout != "" || invalid.stderr == "" {
		t.Fatalf("JSON example invalid input: %+v", invalid)
	}
	select {
	case request := <-observed:
		t.Fatalf("invalid JSON was sent: %+v", request)
	default:
	}
	usage := runCLI(t, t.TempDir(), nil, "run", example)
	requireExit(t, usage, 2)
	if usage.stdout != "" || !strings.Contains(usage.stderr, "Usage:") {
		t.Fatalf("JSON example usage: %+v", usage)
	}
}
