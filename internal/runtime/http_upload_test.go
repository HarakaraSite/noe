package runtime

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

var (
	uploadTestPrefix    = []byte{0, 255, 17}
	uploadTestRemainder = []byte{34, 128, 0, 255}
)

type uploadTestObservation struct {
	label string
	body  []byte
	err   error
}

type uploadTestBarrier struct {
	prefixRead chan struct{}
	once       sync.Once
}

type uploadTestFixture struct {
	server   *httptest.Server
	barriers map[string]*uploadTestBarrier
	observed chan uploadTestObservation
}

// newUploadTestFixture exposes an ack endpoint that opens only after the
// receiver has read the request prefix. A stream producer can wait for that
// acknowledgement before it emits the remainder and EOF.
func newUploadTestFixture(t *testing.T, labels ...string) *uploadTestFixture {
	t.Helper()
	f := &uploadTestFixture{
		barriers: make(map[string]*uploadTestBarrier, len(labels)),
		observed: make(chan uploadTestObservation, len(labels)),
	}
	for _, label := range labels {
		f.barriers[label] = &uploadTestBarrier{prefixRead: make(chan struct{})}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		label := r.URL.Query().Get("label")
		barrier := f.barriers[label]
		if barrier == nil {
			http.Error(w, "unknown upload label", http.StatusBadRequest)
			return
		}
		prefix := make([]byte, len(uploadTestPrefix))
		_, err := io.ReadFull(r.Body, prefix)
		barrier.once.Do(func() { close(barrier.prefixRead) })
		if err == nil && !bytes.Equal(prefix, uploadTestPrefix) {
			err = fmt.Errorf("prefix=%v, want %v", prefix, uploadTestPrefix)
		}
		remainder, readErr := io.ReadAll(r.Body)
		if err == nil {
			err = readErr
		}
		body := append(prefix, remainder...)
		f.observed <- uploadTestObservation{label: label, body: body, err: err}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, "received")
	})
	mux.HandleFunc("/ack", func(w http.ResponseWriter, r *http.Request) {
		barrier := f.barriers[r.URL.Query().Get("label")]
		if barrier == nil {
			http.Error(w, "unknown upload label", http.StatusBadRequest)
			return
		}
		select {
		case <-barrier.prefixRead:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *uploadTestFixture) requireBodies(t *testing.T, labels ...string) {
	t.Helper()
	wantBody := append(append([]byte(nil), uploadTestPrefix...), uploadTestRemainder...)
	seen := make(map[string]bool, len(labels))
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for len(seen) < len(labels) {
		select {
		case result := <-f.observed:
			if result.err != nil {
				t.Fatalf("upload %q: %v", result.label, result.err)
			}
			if !bytes.Equal(result.body, wantBody) {
				t.Fatalf("upload %q body=%v, want %v", result.label, result.body, wantBody)
			}
			if seen[result.label] {
				t.Fatalf("upload %q was received more than once", result.label)
			}
			seen[result.label] = true
		case <-timer.C:
			t.Fatalf("received %d of %d upload bodies; missing labels from %v", len(seen), len(labels), labels)
		}
	}
}

func (f *uploadTestFixture) baseURL() string { return f.server.URL }

const uploadExerciseModule = `import * as http from "noe:http";
const base = %q;
export async function upload(label) {
 if (http.fetch !== globalThis.fetch) throw Error("fetch identity changed");
 const storage = new Uint8Array([99,0,255,17,88]);
 const prefix = storage.subarray(1,4);
 const remainder = new Uint8Array([34,128,0,255]);
 let pull = 0;
 const body = new ReadableStream({
  async pull(controller) {
   if (pull++ === 0) { controller.enqueue(prefix); return; }
   if (pull === 2) { controller.enqueue(new Uint8Array(0)); return; }
   const ack = await fetch(base + "/ack?label=" + label);
   if (ack.status !== 204) throw Error("prefix acknowledgement failed");
   controller.enqueue(remainder);
   controller.close();
  }
 }, {highWaterMark:0});
 const response = await http.fetch(base + "/upload?label=" + label, {method:"POST",body});
 const text = await response.text();
 if (response.status !== 200 || text !== "received") throw Error("upload response: " + response.status + " " + text);
 return text;
}`

func TestNoeHTTPStreamingUploadReachesMainAndWorkers(t *testing.T) {
	fixture := newUploadTestFixture(t, "main", "node", "grandchild", "web")
	dir := t.TempDir()
	module := fmt.Sprintf(uploadExerciseModule, fixture.baseURL())
	if err := os.WriteFile(filepath.Join(dir, "upload.mjs"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"node-worker.mjs": `import {parentPort,workerData,Worker} from "node:worker_threads";
import {upload} from "./upload.mjs";
const own = await upload(workerData.label);
let child = null;
if (workerData.nested) {
 const nested = new Worker("./node-worker.mjs", {workerData:{label:"grandchild",nested:false}});
 try { child = await new Promise((resolve,reject)=>{nested.once("message",resolve);nested.once("error",reject);}); }
 finally { await nested.terminate(); }
}
parentPort.postMessage({own,child});`,
		"web-worker.mjs": `import {upload} from "./upload.mjs";
globalThis.onmessage = async event => {
 try { postMessage(await upload(event.data)); }
 catch (error) { postMessage({error:String(error)}); }
};`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := fmt.Sprintf(`import {upload} from %q;
import {Worker} from "node:worker_threads";
if (await upload("main") !== "received") throw Error("main upload failed");
const node = new Worker("./node-worker.mjs", {workerData:{label:"node",nested:true}});
let nodeResult;
try { nodeResult = await new Promise((resolve,reject)=>{node.once("message",resolve);node.once("error",reject);}); }
finally { await node.terminate(); }
if (nodeResult.own !== "received" || nodeResult.child?.own !== "received" || nodeResult.child.child !== null) throw Error("Node Worker uploads: "+JSON.stringify(nodeResult));
const web = new globalThis.Worker("./web-worker.mjs", {type:"module"});
let webResult;
try {
 const arrived = new Promise((resolve,reject)=>{web.onmessage=event=>resolve(event.data);web.onerror=reject;});
 web.postMessage("web");
 webResult = await arrived;
} finally { await web.terminate(); }
if (webResult !== "received") throw Error("Web Worker upload: "+JSON.stringify(webResult));
console.log("upload reached main and Workers");`, filepath.Join(dir, "upload.mjs"))
	if out := runStdlib(t, main, Options{Cwd: dir}); out != "upload reached main and Workers\n" {
		t.Fatal(out)
	}
	fixture.requireBodies(t, "main", "node", "grandchild", "web")
}

type uploadCancellationFixture struct {
	server   *httptest.Server
	started  chan struct{}
	canceled chan struct{}
}

func newUploadCancellationFixture(t *testing.T) *uploadCancellationFixture {
	t.Helper()
	f := &uploadCancellationFixture{started: make(chan struct{}), canceled: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		prefix := make([]byte, 1)
		_, err := io.ReadFull(r.Body, prefix)
		if err != nil {
			return
		}
		close(f.started)
		_, _ = io.Copy(io.Discard, r.Body)
		close(f.canceled)
	})
	mux.HandleFunc("/started", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-f.started:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *uploadCancellationFixture) waitCanceled(t *testing.T) {
	t.Helper()
	select {
	case <-f.canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("receiver did not observe request cancellation")
	}
}

func TestNoeHTTPStreamingUploadStopsOnRootErrorAndWorkerTerminate(t *testing.T) {
	t.Run("root error", func(t *testing.T) {
		fixture := newUploadCancellationFixture(t)
		base := strconv.Quote(fixture.server.URL)
		source := fmt.Sprintf(`let sent=false;
const body=new ReadableStream({pull(c){if(!sent){sent=true;c.enqueue(new Uint8Array([42]));return;}return new Promise(()=>{});}}, {highWaterMark:0});
const pending=fetch(%s+"/upload",{method:"POST",body});pending.catch(()=>{});
await fetch(%s+"/started");
throw Error("root upload failure");`, base, base)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.mjs"), source}, Options{})
		if err == nil || !bytes.Contains([]byte(err.Error()), []byte("root upload failure")) {
			t.Fatalf("root execution error=%v", err)
		}
		fixture.waitCanceled(t)
	})

	t.Run("Worker terminate", func(t *testing.T) {
		fixture := newUploadCancellationFixture(t)
		dir := t.TempDir()
		worker := fmt.Sprintf(`import {parentPort} from "node:worker_threads";
const base=%q;
let sent=false;
const body=new ReadableStream({pull(c){if(!sent){sent=true;c.enqueue(new Uint8Array([42]));return;}return new Promise(()=>{});}}, {highWaterMark:0});
const pending=fetch(base+"/upload",{method:"POST",body});pending.catch(()=>{});
await fetch(base+"/started");parentPort.postMessage("upload started");await new Promise(()=>{});`, fixture.server.URL)
		if err := os.WriteFile(filepath.Join(dir, "upload-worker.mjs"), []byte(worker), 0600); err != nil {
			t.Fatal(err)
		}
		main := `import {Worker} from "node:worker_threads";
const worker=new Worker("./upload-worker.mjs");
await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});
await worker.terminate();
console.log("upload Worker terminated");`
		if out := runStdlib(t, main, Options{Cwd: dir}); out != "upload Worker terminated\n" {
			t.Fatal(out)
		}
		fixture.waitCanceled(t)
	})
}
