package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestFetchBytesPreserveAllByteValuesAndIndependentCopies(t *testing.T) {
	payload := make([]byte, 256)
	for i := range payload {
		payload[i] = byte(i)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	source := `const response = await fetch(` + string(url) + `);
const first = await response.bytes();
const second = await response.bytes();
if (!(first instanceof Uint8Array) || first === second || first.buffer === second.buffer) throw new Error("independent typed bytes");
for (let i = 0; i < 256; i++) if (first[i] !== i) throw new Error("byte corruption");
first.fill(77); first.buffer.transfer();
if (second[0] !== 0 || second[255] !== 255) throw new Error("shared returned buffers");
console.log(JSON.stringify(Array.from(await response.bytes())));`
	var stdout bytes.Buffer
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "bytes.js"), source}, Options{Stdout: &stdout})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v output=%q", code, err, &stdout)
	}
	var values []int
	if err := json.Unmarshal(stdout.Bytes(), &values); err != nil || len(values) != len(payload) {
		t.Fatalf("output=%q decode=%v", &stdout, err)
	}
	for i, value := range values {
		if value != int(payload[i]) {
			t.Fatalf("byte %d=%d want=%d", i, value, payload[i])
		}
	}
}

func TestFetchBytesPreserveTextJSONEmptyAndErrorResponses(t *testing.T) {
	payload := []byte(`{"message":"日本語","count":2}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			w.WriteHeader(204)
			return
		}
		if r.URL.Path == "/error" {
			w.WriteHeader(503)
		}
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	url, _ := json.Marshal(server.URL)
	source := `const base = ` + string(url) + `;
const response = await fetch(base);
const before = await response.text();
const changed = await response.bytes(); changed.fill(0);
if (await response.text() !== before || (await response.json()).message !== "日本語") throw new Error("text/json changed");
const fresh = await response.bytes();
if (fresh[0] !== 123 || fresh.length === 0) throw new Error("cached bytes changed");
const empty = await fetch(base + "/empty", {method:"DELETE"});
const a = await empty.bytes(), b = await empty.bytes();
if (!empty.ok || empty.status !== 204 || !(a instanceof Uint8Array) || a.length !== 0 || a === b || a.buffer === b.buffer || await empty.text() !== "") throw new Error("empty bytes");
let rejected = false; try { await empty.json(); } catch (error) { rejected = true; }
if (!rejected) throw new Error("empty JSON parsed");
const failed = await fetch(base + "/error");
if (failed.ok || failed.status !== 503 || (await failed.bytes()).length !== fresh.length) throw new Error("error response bytes");
console.log("responses preserved");`
	var stdout bytes.Buffer
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "responses.js"), source}, Options{Stdout: &stdout})
	if code != 0 || err != nil || stdout.String() != "responses preserved\n" {
		t.Fatalf("code=%d err=%v output=%q", code, err, &stdout)
	}
}
