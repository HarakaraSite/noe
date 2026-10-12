//go:build e2e

package e2e_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func binaryPayload() []byte {
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	return data
}

func TestBinaryFileRoundtripViewsAndSnapshotsThroughCLI(t *testing.T) {
	work := t.TempDir()
	payload := binaryPayload()
	input := filepath.Join(work, "入力.bin")
	if err := os.WriteFile(input, payload, 0600); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, filepath.Join(work, "protected.bin"), "keep")
	script := filepath.Join(work, "binary.js")
	writeFixture(t, script, `const bytes = await noe.readFile(noe.args[0]);
if (!(bytes instanceof Uint8Array) || bytes.length !== 256) throw new Error("typed input");
const pending = noe.writeFile("部分.bin", bytes.subarray(1,255));
bytes.fill(9); bytes.buffer.transfer(); await pending;
const again = await noe.readFile("部分.bin");
if (again.length !== 254 || again[0] !== 1 || again[253] !== 254) throw new Error("view snapshot");
await noe.writeFile("empty.bin", new Uint8Array(0));
let rejected = false;
try { await noe.writeFile("protected.bin", [1,2]); } catch (error) { rejected = error instanceof TypeError; }
if (!rejected) throw new Error("invalid type accepted");
noe.writeFile("unawaited.bin", new Uint8Array([0,255,128]));
console.log("binary complete");`)
	got := runCLI(t, work, nil, "run", script, input)
	requireExit(t, got, 0)
	if got.stdout != "binary complete\n" || got.stderr != "" {
		t.Fatalf("binary CLI: %+v", got)
	}
	for name, want := range map[string][]byte{
		"部分.bin":        payload[1:255],
		"empty.bin":     {},
		"unawaited.bin": {0, 255, 128},
		"protected.bin": []byte("keep"),
	} {
		data, err := os.ReadFile(filepath.Join(work, name))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("%s data=%v want=%v err=%v", name, data, want, err)
		}
	}
}

func TestBinaryHTTPDownloadExampleAndPipedREPL(t *testing.T) {
	payload := binaryPayload()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/empty" {
			w.WriteHeader(204)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	defer server.Close()
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "download-file.js")
	for _, tc := range []struct {
		path, output string
		want         []byte
	}{
		{"", "download.bin", payload},
		{"/empty", "empty.bin", []byte{}},
	} {
		got := runCLI(t, work, nil, "run", example, server.URL+tc.path, tc.output)
		requireExit(t, got, 0)
		wantOutput := "Saved " + strconv.Itoa(len(tc.want)) + " bytes to " + tc.output + "\n"
		if got.stdout != wantOutput || got.stderr != "" {
			t.Fatalf("download example: %+v", got)
		}
		data, err := os.ReadFile(filepath.Join(work, tc.output))
		if err != nil || !bytes.Equal(data, tc.want) {
			t.Fatalf("download changed bytes: %v err=%v", data, err)
		}
	}
	url := strconv.Quote(server.URL)
	got := runCLI(t, work, nil, "eval", `const r = await fetch(`+url+`);
const changed = await r.bytes(); changed.fill(0); changed.buffer.transfer();
await noe.writeFile("cached.bin", await r.bytes()); console.log("cached bytes saved");`)
	requireExit(t, got, 0)
	if got.stdout != "cached bytes saved\n" || got.stderr != "" {
		t.Fatalf("response byte cache: %+v", got)
	}
	if data, err := os.ReadFile(filepath.Join(work, "cached.bin")); err != nil || !bytes.Equal(data, payload) {
		t.Fatalf("response cache modified: %v err=%v", data, err)
	}
	repl := runREPLInput(t, work, map[string]string{"NOE_REPL_HISTORY": ""}, strings.Join([]string{
		`const localBytes = await noe.readFile("download.bin")`,
		`console.log("REPL_BINARY", localBytes.length)`,
		`await noe.writeFile("repl.bin", localBytes.subarray(0,3))`,
		`await noe.writeFile("rejected.bin", "text")`,
		`const response = await fetch(` + url + `)`,
		`const remoteBytes = await response.bytes()`,
		`console.log("REPL_REMOTE", remoteBytes[255])`,
		`console.log("REPL_CONTINUED")`,
	}, "\n")+"\n")
	requireExit(t, repl, 0)
	if !strings.Contains(repl.stdout, "REPL_BINARY 256") || !strings.Contains(repl.stdout, "REPL_REMOTE 255") || !strings.Contains(repl.stdout, "REPL_CONTINUED") || !strings.Contains(repl.stderr, "Uint8Array") {
		t.Fatalf("binary REPL: %+v", repl)
	}
	if data, err := os.ReadFile(filepath.Join(work, "repl.bin")); err != nil || !bytes.Equal(data, []byte{0, 1, 2}) {
		t.Fatalf("REPL saved bytes: %v err=%v", data, err)
	}
	if _, err := os.Stat(filepath.Join(work, "rejected.bin")); !os.IsNotExist(err) {
		t.Fatalf("rejected write created file: %v", err)
	}
}

func TestBinaryDownloadFailuresAndTimeout(t *testing.T) {
	started, canceled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/missing":
			w.WriteHeader(404)
			return
		case "/slow":
			_, _ = w.Write([]byte{0, 255, 128})
			w.(http.Flusher).Flush()
			close(started)
			select {
			case <-r.Context().Done():
				close(canceled)
			case <-release:
			}
		default:
			_, _ = w.Write([]byte{0, 255})
		}
	}))
	t.Cleanup(func() { close(release); server.Close() })
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "download-file.js")
	usage := runCLI(t, work, nil, "run", example)
	requireExit(t, usage, 2)
	if usage.stdout != "" || !strings.Contains(usage.stderr, "Usage:") {
		t.Fatalf("download usage: %+v", usage)
	}
	for _, tc := range []struct{ url, output, diagnostic string }{
		{server.URL + "/missing", "missing.bin", "HTTP 404"},
		{server.URL, "missing-parent/output.bin", "missing-parent"},
	} {
		failed := runCLI(t, work, nil, "run", example, tc.url, tc.output)
		requireExit(t, failed, 1)
		if failed.stdout != "" || !strings.Contains(failed.stderr, tc.diagnostic) {
			t.Fatalf("download failure: %+v", failed)
		}
		if _, err := os.Stat(filepath.Join(work, tc.output)); !os.IsNotExist(err) {
			t.Fatalf("failed download created file: %v", err)
		}
	}
	timed := runCLI(t, work, nil, "run", "--timeout=200ms", example, server.URL+"/slow", "timed.bin")
	requireExit(t, timed, 124)
	if timed.stdout != "" || !strings.Contains(timed.stderr, "execution timed out after 200ms") {
		t.Fatalf("binary timeout: %+v", timed)
	}
	for name, signal := range map[string]<-chan struct{}{"request start": started, "request cancellation": canceled} {
		select {
		case <-signal:
		case <-time.After(2 * time.Second):
			t.Fatalf("binary timeout did not observe %s", name)
		}
	}
	if _, err := os.Stat(filepath.Join(work, "timed.bin")); !os.IsNotExist(err) {
		t.Fatalf("incomplete response saved: %v", err)
	}
}
