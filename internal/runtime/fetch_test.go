package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get":
			if r.Method != "GET" || r.Header.Get("X-Test") != "value" {
				t.Errorf("request=%s headers=%v", r.Method, r.Header)
			}
			io.WriteString(w, `{"value":"日本語"}`)
		case "/post":
			if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
				t.Errorf("request=%s headers=%v", r.Method, r.Header)
			}
			data, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			w.WriteHeader(201)
			w.Write(data)
		case "/status":
			w.WriteHeader(404)
			io.WriteString(w, "missing")
		case "/bad-json":
			io.WriteString(w, "invalid JSON")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	for _, tc := range []struct{ name, source, out, errorText string }{
		{"get", `const r = await fetch(base + "/get", {headers:{"X-Test":"value"}}); if (!r.ok || r.status !== 200) throw new Error("status"); console.log((await r.json()).value); console.log(await r.text()); console.log((await r.json()).value);`, "日本語\n{\"value\":\"日本語\"}\n日本語\n", ""},
		{"post", `const r = await fetch(base + "/post", {method:"POST",headers:{"Content-Type":"application/json"},body:'{"ok":true}'}); console.log(r.status, (await r.json()).ok);`, "201 true\n", ""},
		{"http error", `const r = await fetch(base + "/status"); console.log(r.status, r.ok, await r.text());`, "404 false missing\n", ""},
		{"remaining request", `fetch(base + "/status").then(async r => console.log(await r.text()));`, "missing\n", ""},
		{"bad json", `await (await fetch(base + "/bad-json")).json();`, "", "JSON"},
		{"unsupported option", `await fetch(base, {cache:"no-cache"});`, "", "unsupported fetch option"},
		{"unsupported method", `await fetch(base, {method:"HEAD"});`, "", "GET, POST, PUT, PATCH and DELETE"},
		{"bad headers", `await fetch(base, {headers:{"X-Test":42}});`, "", "header values"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stdout bytes.Buffer
			h, err := newHost(Options{Stdout: &stdout})
			if err != nil {
				t.Fatal(err)
			}
			defer h.rt.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "fetch.js"), "const base = " + string(url) + ";" + tc.source})
			if stdout.String() != tc.out {
				t.Fatalf("out=%q want=%q err=%v", &stdout, tc.out, err)
			}
			if tc.errorText == "" {
				if err != nil || code != 0 {
					t.Fatalf("code=%d err=%v", code, err)
				}
			} else if code != 1 || err == nil || !strings.Contains(err.Error(), tc.errorText) {
				t.Fatalf("code=%d err=%v", code, err)
			}
		})
	}
}

func TestFetchCancellation(t *testing.T) {
	started := make(chan struct{})
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	h, err := newHost(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := h.rt.Set("waitForRequest", func() error {
		select {
		case <-started:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}); err != nil {
		t.Fatal(err)
	}
	code, err := h.run(ctx, Source{filepath.Join(t.TempDir(), "cancel.js"), `fetch(` + string(url) + `); waitForRequest(); noe.exit(0);`})
	if err != nil || code != 0 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("HTTP request was not cancelled on exit")
	}
}
