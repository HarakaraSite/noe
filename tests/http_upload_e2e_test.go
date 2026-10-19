//go:build e2e

package e2e_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNoeHTTPUploadStreamExampleThroughCLI(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		received <- body
		_, _ = io.WriteString(w, "upload acknowledged")
	}))
	defer server.Close()

	work := t.TempDir()
	output := filepath.Join(work, "upload-response.bin")
	example := filepath.Join(repoRootPath, "examples", "http", "upload-stream.js")
	got := runCLI(t, work, nil, "run", example, server.URL, output)
	requireExit(t, got, 0)
	if got.stderr != "" {
		t.Fatalf("upload example stderr: %s", got.stderr)
	}
	select {
	case body := <-received:
		if want := []byte{11, 12, 0, 255, 42}; !bytes.Equal(body, want) {
			t.Fatalf("upload body=%v, want %v", body, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("CLI example did not deliver its upload")
	}
	data, err := os.ReadFile(output)
	if err != nil || string(data) != "upload acknowledged" {
		t.Fatalf("saved response=%q err=%v", data, err)
	}
}

type uploadStopE2EFixture struct {
	server       *httptest.Server
	started      chan struct{}
	canceled     chan struct{}
	prefixOnce   sync.Once
	canceledOnce sync.Once
}

func newUploadStopE2EFixture(t *testing.T) *uploadStopE2EFixture {
	t.Helper()
	fixture := &uploadStopE2EFixture{started: make(chan struct{}), canceled: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		prefix := make([]byte, 3)
		if _, err := io.ReadFull(r.Body, prefix); err != nil {
			return
		}
		fixture.prefixOnce.Do(func() { close(fixture.started) })
		_, _ = io.Copy(io.Discard, r.Body)
		fixture.canceledOnce.Do(func() { close(fixture.canceled) })
	})
	mux.HandleFunc("/ack", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-fixture.started:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		fixture.server.CloseClientConnections()
		fixture.server.Close()
	})
	return fixture
}

func (fixture *uploadStopE2EFixture) source(startupMessage, acknowledgedMessage string) string {
	source := `const base=` + strconv.Quote(fixture.server.URL) + `;
let pull=0;
const body=new ReadableStream({async pull(controller){
 if(pull++===0){controller.enqueue(new Uint8Array([0,255,17]));return;}
 const ack=await fetch(base+"/ack");if(ack.status!==204)throw Error("prefix acknowledgement failed");
 console.log(` + strconv.Quote(acknowledgedMessage) + `);
 return new Promise(()=>{});
}},{highWaterMark:0});
const pending=fetch(base+"/upload",{method:"POST",body});pending.catch(()=>{});`
	if startupMessage != "" {
		source += `console.log(` + strconv.Quote(startupMessage) + `);`
	}
	return source + `await new Promise(()=>{});`
}

func (fixture *uploadStopE2EFixture) waitForStop(t *testing.T) {
	t.Helper()
	select {
	case <-fixture.canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("CLI stop did not release the started upload")
	}
}

func TestNoeHTTPStreamingUploadSIGINTReleasesStartedRequest(t *testing.T) {
	fixture := newUploadStopE2EFixture(t)
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout", "5s", fixture.source("upload source ready", "upload prefix acknowledged"))
	if !process.stdout.waitFor("upload source ready\n", 3*time.Second) {
		t.Fatalf("CLI did not start: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	select {
	case <-fixture.started:
	case <-time.After(3 * time.Second):
		t.Fatalf("upload prefix was not received: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	if !process.stdout.waitFor("upload prefix acknowledged\n", 3*time.Second) {
		t.Fatalf("producer did not receive the prefix acknowledgement: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	got := process.wait(t, 3*time.Second)
	requireExit(t, got, 1)
	if got.stdout != "upload source ready\nupload prefix acknowledged\n" || got.stderr == "" || strings.Contains(got.stderr, "execution timed out") {
		t.Fatalf("SIGINT result: %+v", got)
	}
	fixture.waitForStop(t)
}

func TestNoeHTTPStreamingUploadTimeoutReleasesStartedRequest(t *testing.T) {
	fixture := newUploadStopE2EFixture(t)
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--timeout", "200ms", fixture.source("", "STARTED"))
	select {
	case <-fixture.started:
	case <-time.After(3 * time.Second):
		t.Fatalf("upload prefix was not received: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	if !process.stdout.waitFor("STARTED\n", 3*time.Second) {
		t.Fatalf("producer did not receive the prefix acknowledgement: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	got := process.wait(t, 3*time.Second)
	requireExecutionTimeout(t, got)
	fixture.waitForStop(t)
}
