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
	"sync/atomic"
	"testing"
)

func TestFetchUpdateMethodsSendBodyAndHeaders(t *testing.T) {
	type request struct {
		Method      string `json:"method"`
		Body        string `json:"body"`
		ContentType string `json:"contentType"`
		Custom      string `json:"custom"`
	}
	observed := make(chan request, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "read body", 500)
			return
		}
		value := request{r.Method, string(body), r.Header.Get("Content-Type"), r.Header.Get("X-Test")}
		observed <- value
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	for _, tc := range []struct{ name, method, bodyExpression, body, contentType string }{
		{"PUT JSON", "PUT", `JSON.stringify({message:"日本語🍵"})`, `{"message":"日本語🍵"}`, "application/json"},
		{"lowercase PATCH", "patch", `"雪\nplain text"`, "雪\nplain text", "text/plain"},
		{"mixed case DELETE body", "DeLeTe", `"reason=finished"`, "reason=finished", "text/plain"},
		{"PUT empty", "PUT", `""`, "", ""},
		{"PATCH omitted", "PATCH", "", "", ""},
		{"DELETE undefined", "DELETE", "undefined", "", ""},
		{"DELETE omitted", "DELETE", "", "", ""},
		{"GET undefined", "GET", "undefined", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			method, _ := json.Marshal(tc.method)
			contentType, _ := json.Marshal(tc.contentType)
			headers := `{"X-Test":"custom"}`
			if tc.contentType != "" {
				headers = `{"X-Test":"custom","Content-Type":` + string(contentType) + `}`
			}
			options := `method:` + string(method) + `,headers:` + headers
			if tc.bodyExpression != "" {
				options += `,body:` + tc.bodyExpression
			}
			source := `const r = await fetch(` + string(url) + `,{` + options + `});
if (r.status !== 200 || !r.ok) throw new Error("response status");
const text = await r.text();
if (text !== await r.text()) throw new Error("response text changed");
if (JSON.stringify(await r.json()) !== JSON.stringify(await r.json())) throw new Error("response JSON changed");
console.log(text);`
			var stdout bytes.Buffer
			code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "methods.js"), source}, Options{Stdout: &stdout})
			if code != 0 || err != nil {
				t.Fatalf("code=%d err=%v output=%q", code, err, &stdout)
			}
			want := request{strings.ToUpper(tc.method), tc.body, tc.contentType, "custom"}
			select {
			case value := <-observed:
				if value != want {
					t.Fatalf("request=%+v want=%+v", value, want)
				}
			default:
				t.Fatal("no request reached server")
			}
			var response request
			if err := json.Unmarshal(stdout.Bytes(), &response); err != nil || response != want {
				t.Fatalf("response=%+v want=%+v decode=%v output=%q", response, want, err, &stdout)
			}
		})
	}
}

func TestFetchRejectsUnsupportedInputsBeforeSending(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(204)
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	for _, options := range []string{
		`{method:"HEAD"}`, `{method:"OPTIONS"}`, `{method:42}`,
		`{body:""}`, `{method:"get",body:"data"}`,
		`{method:"PUT",body:{value:1}}`, `{method:"PATCH",body:null}`, `{method:"DELETE",body:42}`,
		`{method:"PUT",headers:{"X-Test":42}}`, `{method:"PATCH",cache:"no-cache"}`,
	} {
		t.Run(options, func(t *testing.T) {
			source := `await fetch(` + string(url) + `,` + options + `);`
			code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "invalid.js"), source}, Options{})
			if code != 1 || err == nil || !strings.Contains(Diagnostic(err), "TypeError") {
				t.Fatalf("code=%d err=%v", code, err)
			}
		})
	}
	if count := requests.Load(); count != 0 {
		t.Fatalf("invalid inputs sent %d requests", count)
	}
}

func TestFetchUpdateMethodsEmptyAndErrorResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(409)
		_, _ = io.WriteString(w, `{"error":"conflict"}`)
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	source := `const base = ` + string(url) + `;
const empty = await fetch(base + "/empty", {method:"DELETE"});
if (empty.status !== 204 || !empty.ok || await empty.text() !== "") throw new Error("empty response");
let rejected = false;
try { await empty.json(); } catch (error) { rejected = true; }
if (!rejected) throw new Error("empty body parsed as JSON");
const conflict = await fetch(base + "/conflict", {method:"PUT",body:"{}"});
if (conflict.status !== 409 || conflict.ok || (await conflict.json()).error !== "conflict") throw new Error("HTTP error response");
console.log("responses verified");`
	var stdout bytes.Buffer
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "responses.js"), source}, Options{Stdout: &stdout})
	if code != 0 || err != nil || stdout.String() != "responses verified\n" {
		t.Fatalf("code=%d err=%v output=%q", code, err, &stdout)
	}
}
