package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var httpReceivePrefix = []byte{0, 255, 17}
var httpReceiveRemainder = []byte{34, 128, 0, 255}

type httpReceiveFixture struct {
	server      *httptest.Server
	ready       chan map[string]string
	prefixes    map[string]chan struct{}
	prefixOnce  map[string]*sync.Once
	releaseWait chan struct{}
	releaseOnce sync.Once
}

func newHTTPReceiveFixture(t *testing.T, modes ...string) *httpReceiveFixture {
	t.Helper()
	f := &httpReceiveFixture{
		ready:       make(chan map[string]string, 1),
		prefixes:    make(map[string]chan struct{}, len(modes)),
		prefixOnce:  make(map[string]*sync.Once, len(modes)),
		releaseWait: make(chan struct{}),
	}
	for _, mode := range modes {
		f.prefixes[mode] = make(chan struct{})
		f.prefixOnce[mode] = &sync.Once{}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		var urls map[string]string
		if err := json.NewDecoder(r.Body).Decode(&urls); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		select {
		case f.ready <- urls:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/prefix", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		barrier, ok := f.prefixes[mode]
		if !ok {
			http.Error(w, "unknown receive mode", http.StatusBadRequest)
			return
		}
		f.prefixOnce[mode].Do(func() { close(barrier) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/wait", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-f.releaseWait:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.release()
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *httpReceiveFixture) release() {
	f.releaseOnce.Do(func() { close(f.releaseWait) })
}

func (f *httpReceiveFixture) waitReady(t *testing.T) map[string]string {
	t.Helper()
	select {
	case urls := <-f.ready:
		return urls
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not publish its HTTP listener URLs")
		return nil
	}
}

func (f *httpReceiveFixture) waitPrefix(t *testing.T, mode string) {
	t.Helper()
	barrier, ok := f.prefixes[mode]
	if !ok {
		t.Fatalf("no prefix barrier for mode %q", mode)
	}
	select {
	case <-barrier:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s handler did not observe the request prefix before EOF", mode)
	}
}

// beginHTTPReceiveRequest uses a raw HTTP/1 client so the test can leave a
// declared request body open after its first bytes have reached the server.
func beginHTTPReceiveRequest(t *testing.T, address, target string, contentLength int64, prefix []byte) net.Conn {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if err != nil {
		t.Fatalf("connect to receive server %s: %v", address, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn,
		"POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\nConnection: close\r\n\r\n",
		target, u.Host, contentLength); err != nil {
		t.Fatalf("write request headers: %v", err)
	}
	if _, err := conn.Write(prefix); err != nil {
		t.Fatalf("write request prefix: %v", err)
	}
	return conn
}

func finishHTTPReceiveRequest(t *testing.T, conn net.Conn, remainder []byte) (int, []byte) {
	t.Helper()
	if _, err := conn.Write(remainder); err != nil {
		t.Fatalf("write request remainder: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("read receive response: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read receive response body: %v", err)
	}
	return resp.StatusCode, body
}

func requireHTTPReceiveConnectionStopped(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	// net/http may have buffered response bytes before termination closes
	// the connection. Drain them, then require EOF or a transport close;
	// receiving a byte alone does not establish that the socket stayed open.
	_, err := io.Copy(io.Discard, conn)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("pending HTTP client did not stop after runtime shutdown: %v", err)
	}
}

func requireHTTPReceivePortReleased(t *testing.T, address string) {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", u.Host)
	if err != nil {
		t.Fatalf("receive listener %s was not released: %v", u.Host, err)
	}
	_ = listener.Close()
}

const httpReceiveModule = `import {fetch} from "noe:http";
const fixture=%q;

export function launch(mode) {
 let resolveDone;
 const done=new Promise(resolve=>{resolveDone=resolve;});
 const server=serve({port:0},async request=>{
  const reader=request.body.getReader();
  const prefix=[];let byteCount=0,sum=0;
  for(;;){
   const {value,done}=await reader.read();
   if(done)break;
   for(const byte of value){byteCount++;sum+=byte;if(prefix.length<3)prefix.push(byte);}
   if(prefix.length===3){
    const ack=await fetch(fixture+"/prefix?mode="+mode);
    if(ack.status!==204)throw Error("prefix barrier failed for "+mode);
    break;
   }
  }
  if(prefix.length===3){
   for(;;){const {value,done}=await reader.read();if(done)break;for(const byte of value){byteCount++;sum+=byte;}}
  }
  const result={mode,byteCount,sum};
  resolveDone(result);
  return Response.json(result);
 });
 return {server,done};
}`

func TestNoeHTTPReceiveStreamReachesMainAndWorkersBeforeEOF(t *testing.T) {
	fixture := newHTTPReceiveFixture(t, "main", "node", "nested", "web")
	dir := t.TempDir()
	module := fmt.Sprintf(httpReceiveModule, fixture.server.URL)
	if err := os.WriteFile(filepath.Join(dir, "receive.mjs"), []byte(module), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"node-worker.mjs": `import {parentPort,workerData,Worker} from "node:worker_threads";
import {fetch} from "noe:http";
import {launch} from "./receive.mjs";
const own=launch(workerData.mode);
let child=null;
let childReady=null;
if(workerData.nested){
 child=new Worker("./node-worker.mjs",{workerData:{mode:"nested",nested:false}});
 childReady=await new Promise((resolve,reject)=>{child.once("message",resolve);child.once("error",reject);});
}
parentPort.postMessage({mode:workerData.mode,url:own.server.url,childUrl:childReady?.url||""});
parentPort.on("message",async message=>{
 if(message!=="stop")return;
 own.server.close();
 if(child){child.postMessage("stop");await new Promise((resolve,reject)=>{child.once("message",value=>value?.done?resolve():null);child.once("error",reject);});await child.terminate();}
 parentPort.postMessage({done:true});
});`,
		"web-worker.mjs": `import {launch} from "./receive.mjs";
let own;
globalThis.onmessage=event=>{
 if(event.data==="start"){
  own=launch("web");postMessage({mode:"web",url:own.server.url});return;
 }
 if(event.data==="stop"&&own){own.server.close();postMessage({done:true});}
};`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := fmt.Sprintf(`import {fetch} from "noe:http";
import {Worker} from "node:worker_threads";
import {launch} from %q;
const fixture=%q;
const own=launch("main");
const node=new Worker("./node-worker.mjs",{workerData:{mode:"node",nested:true}});
let nodeReadyResolve,nodeDoneResolve;
const nodeReady=new Promise(resolve=>{nodeReadyResolve=resolve;});
const nodeDone=new Promise(resolve=>{nodeDoneResolve=resolve;});
node.on("message",message=>{if(message?.done)nodeDoneResolve();else nodeReadyResolve(message);});
const web=new globalThis.Worker("./web-worker.mjs",{type:"module"});
let webReadyResolve,webDoneResolve;
const webReady=new Promise(resolve=>{webReadyResolve=resolve;});
const webDone=new Promise(resolve=>{webDoneResolve=resolve;});
web.onmessage=event=>{if(event.data?.done)webDoneResolve();else webReadyResolve(event.data);};
web.postMessage("start");
const [nodeInfo,webInfo]=await Promise.all([nodeReady,webReady]);
const childReady={mode:"nested",url:nodeInfo.childUrl};
const urls={main:own.server.url,node:nodeInfo.url,nested:childReady.url,web:webInfo.url};
const published=await fetch(fixture+"/ready",{method:"POST",body:JSON.stringify(urls)});
if(published.status!==204)throw Error("failed to publish receive URLs");
await fetch(fixture+"/wait");
own.server.close();
node.postMessage("stop");web.postMessage("stop");
await Promise.all([nodeDone,webDone]);
await node.terminate();await web.terminate();
console.log("HTTP receive reached main and Workers");`, filepath.Join(dir, "receive.mjs"), fixture.server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	type execution struct {
		code   int
		err    error
		stdout string
		stderr string
	}
	done := make(chan execution, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		code, err := Execute(ctx, Source{Name: filepath.Join(dir, "main.mjs"), Code: main}, Options{Cwd: dir, Stdout: &stdout, Stderr: &stderr})
		done <- execution{code, err, stdout.String(), stderr.String()}
	}()
	defer fixture.release()

	urls := fixture.waitReady(t)
	for _, mode := range []string{"main", "node", "nested", "web"} {
		t.Run(mode, func(t *testing.T) {
			target := "/receive"
			if mode != "main" {
				target += "?mode=" + mode
			}
			conn := beginHTTPReceiveRequest(t, urls[mode], target, int64(len(httpReceivePrefix)+len(httpReceiveRemainder)), httpReceivePrefix)
			fixture.waitPrefix(t, mode)
			status, body := finishHTTPReceiveRequest(t, conn, httpReceiveRemainder)
			var got struct {
				Mode      string `json:"mode"`
				ByteCount int    `json:"byteCount"`
				Sum       int    `json:"sum"`
			}
			if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK || got.Mode != mode || got.ByteCount != 7 || got.Sum != 689 {
				t.Fatalf("status=%d body=%q decoded=%+v err=%v", status, body, got, err)
			}
		})
	}
	fixture.release()
	select {
	case result := <-done:
		if result.code != 0 || result.err != nil || result.stdout != "HTTP receive reached main and Workers\n" || result.stderr != "" {
			t.Fatalf("runtime result: code=%d err=%v stdout=%q stderr=%q", result.code, result.err, result.stdout, result.stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("runtime did not close its receive servers after the fixture released it")
	}
	for _, mode := range []string{"main", "node", "nested", "web"} {
		requireHTTPReceivePortReleased(t, urls[mode])
	}
}

type httpReceiveLifecycleFixture struct {
	server      *httptest.Server
	ready       chan string
	pending     chan struct{}
	done        chan struct{}
	release     chan struct{}
	releaseOnce sync.Once
	readyOnce   sync.Once
	pendingOnce sync.Once
	doneOnce    sync.Once
}

func newHTTPReceiveLifecycleFixture(t *testing.T) *httpReceiveLifecycleFixture {
	t.Helper()
	f := &httpReceiveLifecycleFixture{
		ready:   make(chan string, 1),
		pending: make(chan struct{}),
		done:    make(chan struct{}),
		release: make(chan struct{}),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		f.readyOnce.Do(func() { f.ready <- r.URL.Query().Get("url") })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/pending", func(w http.ResponseWriter, r *http.Request) {
		f.pendingOnce.Do(func() { close(f.pending) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/continue", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-f.release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/done", func(w http.ResponseWriter, r *http.Request) {
		f.doneOnce.Do(func() { close(f.done) })
		w.WriteHeader(http.StatusNoContent)
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.releaseOnce.Do(func() { close(f.release) })
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *httpReceiveLifecycleFixture) waitReady(t *testing.T) string {
	t.Helper()
	select {
	case address := <-f.ready:
		return address
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle fixture did not receive the server URL")
		return ""
	}
}

func (f *httpReceiveLifecycleFixture) waitPending(t *testing.T) {
	t.Helper()
	select {
	case <-f.pending:
	case <-time.After(5 * time.Second):
		t.Fatal("request body did not reach its pending read")
	}
}

func TestNoeHTTPReceivePendingReadStopsOnRootFailure(t *testing.T) {
	fixture := newHTTPReceiveLifecycleFixture(t)
	base := strconv.Quote(fixture.server.URL)
	source := `import {fetch} from "noe:http";
const fixture=` + base + `;
const server=serve({port:0},async request=>{
 const reader=request.body.getReader();
 const first=await reader.read();if(first.done)throw Error("missing request prefix");
 const pending=reader.read();
 const ack=await fetch(fixture+"/pending");if(ack.status!==204)throw Error("pending barrier failed");
 await pending;
 return new Response("unexpected");
});
await fetch(fixture+"/ready?url="+encodeURIComponent(server.url));
await fetch(fixture+"/continue");
throw new Error("HTTP_ROOT_FAILURE");`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type execution struct {
		code int
		err  error
	}
	done := make(chan execution, 1)
	go func() {
		code, err := Execute(ctx, Source{Name: filepath.Join(t.TempDir(), "main.mjs"), Code: source}, Options{})
		done <- execution{code, err}
	}()
	address := fixture.waitReady(t)
	conn := beginHTTPReceiveRequest(t, address, "/pending", 2, []byte{42})
	fixture.waitPending(t)
	fixture.releaseOnce.Do(func() { close(fixture.release) })
	select {
	case result := <-done:
		if result.err == nil || !strings.Contains(result.err.Error(), "HTTP_ROOT_FAILURE") {
			t.Fatalf("root failure result: code=%d err=%v", result.code, result.err)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("root failure did not release the pending receive")
	}
	requireHTTPReceiveConnectionStopped(t, conn)
	requireHTTPReceivePortReleased(t, address)
}

func TestNoeHTTPReceivePendingReadStopsOnWorkerTermination(t *testing.T) {
	fixture := newHTTPReceiveLifecycleFixture(t)
	base := strconv.Quote(fixture.server.URL)
	source := `import {fetch} from "noe:http";
import {Worker} from "node:worker_threads";
const fixture=` + base + `;
const worker=new Worker(` + strconv.Quote(`import {parentPort} from "node:worker_threads";
import {fetch} from "noe:http";
const server=serve({port:0},async request=>{
 const reader=request.body.getReader();
 const first=await reader.read();if(first.done)throw Error("missing request prefix");
 const pending=reader.read().catch(()=>null);
 const ack=await fetch(`+base+`+"/pending");if(ack.status!==204)throw Error("pending barrier failed");
 await pending;
 return new Response("unexpected");
});
parentPort.postMessage(server.url);`) + `,{eval:true});
const address=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});
await fetch(fixture+"/ready?url="+encodeURIComponent(address));
await fetch(fixture+"/continue");
await worker.terminate();
await fetch(fixture+"/done");`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	type execution struct {
		code   int
		err    error
		stdout string
		stderr string
	}
	done := make(chan execution, 1)
	go func() {
		var stdout, stderr bytes.Buffer
		code, err := Execute(ctx, Source{Name: filepath.Join(t.TempDir(), "main.mjs"), Code: source}, Options{Stdout: &stdout, Stderr: &stderr})
		done <- execution{code, err, stdout.String(), stderr.String()}
	}()
	var address string
	select {
	case address = <-fixture.ready:
	case result := <-done:
		t.Fatalf("runtime exited before worker listener became ready: code=%d err=%v stdout=%q stderr=%q", result.code, result.err, result.stdout, result.stderr)
	case <-time.After(5 * time.Second):
		t.Fatal("lifecycle fixture did not receive the server URL")
	}
	conn := beginHTTPReceiveRequest(t, address, "/pending", 2, []byte{42})
	fixture.waitPending(t)
	fixture.releaseOnce.Do(func() { close(fixture.release) })
	select {
	case <-fixture.done:
	case <-time.After(7 * time.Second):
		t.Fatal("Worker termination did not complete")
	}
	select {
	case result := <-done:
		if result.code != 0 || result.err != nil || result.stderr != "" {
			t.Fatalf("Worker termination result: code=%d err=%v stdout=%q stderr=%q", result.code, result.err, result.stdout, result.stderr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runtime did not return after Worker termination")
	}
	requireHTTPReceiveConnectionStopped(t, conn)
	requireHTTPReceivePortReleased(t, address)
}
