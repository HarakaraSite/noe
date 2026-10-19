//go:build e2e

package e2e_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

var e2eReceivePrefix = []byte{0, 255, 17}
var e2eReceiveRemainder = []byte{34, 128, 0, 255}

func TestNoeHTTPReceiveStreamExampleThroughCLI(t *testing.T) {
	got := runCLI(t, repoRootPath, nil, "run", filepath.Join(repoRootPath, "examples", "http", "receive-stream.js"))
	requireExit(t, got, 0)
	if got.stdout != "{\"bytes\":[0,255,17,34,128,0,255],\"byteCount\":7,\"sum\":689}\n" || got.stderr != "" {
		t.Fatalf("receive-stream example: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
}

type e2eHTTPReceiveFixture struct {
	server      *httptest.Server
	prefix      chan struct{}
	release     chan struct{}
	prefixOnce  sync.Once
	releaseOnce sync.Once
}

func newE2EHTTPReceiveFixture(t *testing.T) *e2eHTTPReceiveFixture {
	t.Helper()
	f := &e2eHTTPReceiveFixture{prefix: make(chan struct{}), release: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/prefix", func(w http.ResponseWriter, r *http.Request) {
		f.prefixOnce.Do(func() { close(f.prefix) })
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/wait", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-f.release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	f.server = httptest.NewServer(mux)
	t.Cleanup(func() {
		f.releaseWait()
		f.server.CloseClientConnections()
		f.server.Close()
	})
	return f
}

func (f *e2eHTTPReceiveFixture) releaseWait() {
	f.releaseOnce.Do(func() { close(f.release) })
}

func (f *e2eHTTPReceiveFixture) waitPrefix(t *testing.T) {
	t.Helper()
	select {
	case <-f.prefix:
	case <-time.After(5 * time.Second):
		t.Fatal("CLI handler did not read the request prefix before EOF")
	}
}

func beginE2EHTTPReceiveRequest(t *testing.T, address string, target string, length int, prefix []byte) net.Conn {
	t.Helper()
	u, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", u.Host, 3*time.Second)
	if err != nil {
		t.Fatalf("connect to CLI receive server %s: %v", address, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(8 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := fmt.Fprintf(conn, "POST %s HTTP/1.1\r\nHost: %s\r\nContent-Type: application/octet-stream\r\nContent-Length: %d\r\nConnection: close\r\n\r\n", target, u.Host, length); err != nil {
		t.Fatalf("write CLI request headers: %v", err)
	}
	if _, err := conn.Write(prefix); err != nil {
		t.Fatalf("write CLI request prefix: %v", err)
	}
	return conn
}

func finishE2EHTTPReceiveRequest(t *testing.T, conn net.Conn, remainder []byte) (int, []byte) {
	t.Helper()
	if _, err := conn.Write(remainder); err != nil {
		t.Fatalf("write CLI request remainder: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodPost})
	if err != nil {
		t.Fatalf("read CLI receive response: %v", err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read CLI receive response body: %v", err)
	}
	return response.StatusCode, body
}

func requireE2EHTTPReceiveClientStopped(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	_, err := io.Copy(io.Discard, conn)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("CLI shutdown did not stop its pending HTTP client: %v", err)
	}
}

func TestNoeHTTPReceivePrefixBeforeEOFThroughCLI(t *testing.T) {
	fixture := newE2EHTTPReceiveFixture(t)
	source := fmt.Sprintf(`import {fetch} from "noe:http";
const fixture=%q;let resolveDone;
const done=new Promise(resolve=>{resolveDone=resolve;});
const server=serve({port:0},async request=>{
 const reader=request.body.getReader();let prefix=[],byteCount=0,sum=0;
 for(;;){const {value,done}=await reader.read();if(done)break;for(const byte of value){byteCount++;sum+=byte;if(prefix.length<3)prefix.push(byte);}if(prefix.length===3){const ack=await fetch(fixture+"/prefix");if(ack.status!==204)throw Error("prefix acknowledgement failed");break;}}
 if(prefix.length===3)for(;;){const {value,done}=await reader.read();if(done)break;for(const byte of value){byteCount++;sum+=byte;}}
 const result={byteCount,sum};resolveDone(result);return Response.json(result);
});
console.log("SERVER_URL",server.url);
const result=await done;
await fetch(fixture+"/wait");
server.close();
console.log("RECEIVE_RESULT",JSON.stringify(result));`, fixture.server.URL)
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", source)
	address := waitServeURL(t, process)
	conn := beginE2EHTTPReceiveRequest(t, address, "/receive", len(e2eReceivePrefix)+len(e2eReceiveRemainder), e2eReceivePrefix)
	fixture.waitPrefix(t)
	status, body := finishE2EHTTPReceiveRequest(t, conn, e2eReceiveRemainder)
	var result struct {
		ByteCount int `json:"byteCount"`
		Sum       int `json:"sum"`
	}
	if err := json.Unmarshal(body, &result); err != nil || status != http.StatusOK || result.ByteCount != 7 || result.Sum != 689 {
		t.Fatalf("CLI receive response: status=%d body=%q decoded=%+v err=%v", status, body, result, err)
	}
	fixture.releaseWait()
	got := process.wait(t, 5*time.Second)
	requireExit(t, got, 0)
	if got.stderr != "" || !strings.Contains(got.stdout, "RECEIVE_RESULT {\"byteCount\":7,\"sum\":689}\n") {
		t.Fatalf("CLI receive completion: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
	requireServeRebind(t, address)
}

func pendingCLIReceiveSource() string {
	return `const server=serve({port:0},async request=>{
 const reader=request.body.getReader();
 const first=await reader.read();if(first.done)throw Error("missing initial body byte");
 const pending=reader.read();
 console.log("BODY_READ_PENDING");
 await pending;
 return new Response("unexpected");
});
console.log("SERVER_URL",server.url);
await new Promise(()=>{});`
}

func startCLIWithPendingReceive(t *testing.T, timeout string) (*openStdinCLI, string, net.Conn) {
	t.Helper()
	args := []string{"eval"}
	if timeout != "" {
		args = append(args, "--timeout", timeout)
	}
	args = append(args, pendingCLIReceiveSource())
	process := startCLIWithOpenStdin(t, t.TempDir(), args...)
	address := waitServeURL(t, process)
	conn := beginE2EHTTPReceiveRequest(t, address, "/pending", 2, []byte{42})
	if !process.stdout.waitFor("BODY_READ_PENDING\n", 3*time.Second) {
		t.Fatalf("CLI handler did not enter its pending body read: stdout=%q stderr=%q", process.stdout.String(), process.stderr.String())
	}
	return process, address, conn
}

func TestNoeHTTPReceivePendingReadStopsOnCLISIGINT(t *testing.T) {
	process, address, conn := startCLIWithPendingReceive(t, "5s")
	if err := process.cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	got := process.wait(t, 4*time.Second)
	requireExit(t, got, 1)
	if got.stderr == "" || strings.Contains(got.stderr, "execution timed out") {
		t.Fatalf("SIGINT result: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
	requireE2EHTTPReceiveClientStopped(t, conn)
	requireServeRebind(t, address)
}

func TestNoeHTTPReceivePendingReadStopsOnCLITimeout(t *testing.T) {
	process, address, conn := startCLIWithPendingReceive(t, "5s")
	got := process.wait(t, 7*time.Second)
	requireExit(t, got, 124)
	if !strings.Contains(got.stderr, "execution timed out after 5s") || !strings.Contains(got.stdout, "BODY_READ_PENDING\n") {
		t.Fatalf("pending-read timeout result: stdout=%q stderr=%q", got.stdout, got.stderr)
	}
	requireE2EHTTPReceiveClientStopped(t, conn)
	requireServeRebind(t, address)
}
