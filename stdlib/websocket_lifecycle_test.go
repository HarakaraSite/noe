package stdlib_test

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// heldWebSocketHandshake accepts an opening request but does not send its
// response until released. Its control endpoints let a script wait until the
// request has reached the server and, after close events, let the test release
// the response and wait until the peer has gone away.
type heldWebSocketHandshake struct {
	server *httptest.Server

	started         chan struct{}
	release         chan struct{}
	responseWritten chan struct{}
	peerClosed      chan struct{}

	startOnce    sync.Once
	releaseOnce  sync.Once
	responseOnce sync.Once
	peerOnce     sync.Once
	connMu       sync.Mutex
	conn         net.Conn
}

func newHeldWebSocketHandshake(t *testing.T) *heldWebSocketHandshake {
	t.Helper()
	f := &heldWebSocketHandshake{
		started:         make(chan struct{}),
		release:         make(chan struct{}),
		responseWritten: make(chan struct{}),
		peerClosed:      make(chan struct{}),
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ready":
			if !waitWebSocketBarrier(f.started, 3*time.Second) {
				http.Error(w, "websocket request did not arrive", http.StatusGatewayTimeout)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case "/release":
			f.releaseHandshake()
			if !waitWebSocketBarrier(f.responseWritten, 3*time.Second) {
				http.Error(w, "websocket response was not written", http.StatusGatewayTimeout)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		case "/peer-closed":
			if !waitWebSocketBarrier(f.peerClosed, 3*time.Second) {
				http.Error(w, "websocket peer stayed open", http.StatusGatewayTimeout)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack WebSocket handshake: %v", err)
			return
		}
		f.connMu.Lock()
		f.conn = conn
		f.connMu.Unlock()
		f.startOnce.Do(func() { close(f.started) })
		defer conn.Close()
		<-f.release

		accept := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		_, writeErr := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:]))
		if writeErr == nil {
			// If a late handshake is incorrectly attached, deliver data as well
			// so the test can detect a message after close() while CONNECTING.
			_, writeErr = rw.Write([]byte{0x81, 2, 'o', 'k', 0x88, 2, 3, 232})
		}
		if writeErr == nil {
			writeErr = rw.Flush()
		}
		// A client that closed while CONNECTING may already have closed the
		// peer, so a failed response write is an expected fixture outcome.
		f.responseOnce.Do(func() { close(f.responseWritten) })

		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var buf [256]byte
		for {
			if _, err := conn.Read(buf[:]); err != nil {
				if err == io.EOF {
					f.peerOnce.Do(func() { close(f.peerClosed) })
					return
				}
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					return
				}
				// A reset also means the peer has closed its side.
				f.peerOnce.Do(func() { close(f.peerClosed) })
				break
			}
		}
	}))
	t.Cleanup(func() {
		f.releaseHandshake()
		f.closePeer()
		f.server.Close()
	})
	return f
}

func waitWebSocketBarrier(ch <-chan struct{}, timeout time.Duration) bool {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-ch:
		return true
	case <-timer.C:
		return false
	}
}

func (f *heldWebSocketHandshake) releaseHandshake() {
	f.releaseOnce.Do(func() { close(f.release) })
}

func (f *heldWebSocketHandshake) closePeer() {
	f.connMu.Lock()
	conn := f.conn
	f.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (f *heldWebSocketHandshake) webSocketURL() string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + "/socket"
}

func TestWebSocketCloseWhileConnectingCancelsOpeningAndSuppressesLateEvents(t *testing.T) {
	fixture := newHeldWebSocketHandshake(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
const events = [];
let closeFailure = "";
const closed = new Promise(resolve => {
  socket.addEventListener("open", () => { events.push("open"); socket.close(); });
  socket.addEventListener("message", () => events.push("message"));
  socket.addEventListener("error", () => {
    events.push("error");
    if (socket.readyState !== WebSocket.CLOSED) closeFailure = "error saw non-CLOSED state";
  });
  socket.addEventListener("close", event => {
    events.push("close");
    if (socket.readyState !== WebSocket.CLOSED) closeFailure = "close saw non-CLOSED state";
    if (event.code !== 1006 || event.wasClean) closeFailure = "CONNECTING close was not an unclean 1006";
    resolve();
  });
});
const ready = await fetch(%q + "/ready");
if (!ready.ok) throw Error("server did not observe the opening request");
socket.close();
await closed;
const released = await fetch(%q + "/release");
if (!released.ok) throw Error("server could not release the held response");
const peer = await fetch(%q + "/peer-closed");
if (!peer.ok) throw Error("WebSocket peer did not close");
if (events.join(",") !== "error,close") throw Error("unexpected events: " + events.join(","));
if (closeFailure) throw Error(closeFailure);
if (socket.readyState !== WebSocket.CLOSED) throw Error("socket did not stay CLOSED");
`, fixture.webSocketURL(), fixture.server.URL, fixture.server.URL, fixture.server.URL)
	profile := stdlib.Profile{Modules: stdlib.Modules{
		Fetch:      &stdlib.FetchOptions{Client: fixture.server.Client()},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }, HandshakeTimeout: 4 * time.Second},
	}}
	done := make(chan error, 1)
	go func() {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket close while connecting]", Virtual: true, Module: true, Code: code}, profile)
		done <- err
	}()
	completed := false
	defer func() {
		cancel()
		fixture.releaseHandshake()
		fixture.closePeer()
		if !completed {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("Execute remained blocked after the held peer was forcibly closed")
			}
		}
	}()

	if !waitWebSocketBarrier(fixture.started, time.Second) {
		t.Fatal("WebSocket opening request did not reach the held-response fixture")
	}
	select {
	case err := <-done:
		completed = true
		if err != nil {
			t.Fatal(stdlib.Diagnostic(err))
		}
	case <-time.After(time.Second):
		// Cleanup releases and closes the peer before waiting for Execute, so a
		// regression cannot leave this test blocked through HandshakeTimeout.
		t.Fatal("close() while CONNECTING did not finish before the held handshake was released")
	}
}

func TestWebSocketExecutionCancellationInterruptsHeldHandshake(t *testing.T) {
	fixture := newHeldWebSocketHandshake(t)
	ctx, cancel := context.WithCancel(context.Background())
	code := fmt.Sprintf(`new WebSocket(%q);`, fixture.webSocketURL())
	profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{
		Allow: func(*url.URL) error { return nil }, HandshakeTimeout: 4 * time.Second,
	}}}
	done := make(chan error, 1)
	go func() {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket execution cancellation]", Virtual: true, Module: true, Code: code}, profile)
		done <- err
	}()
	completed := false
	defer func() {
		cancel()
		fixture.releaseHandshake()
		fixture.closePeer()
		if !completed {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("Execute remained blocked after the held peer was forcibly closed")
			}
		}
	}()

	if !waitWebSocketBarrier(fixture.started, time.Second) {
		t.Fatal("WebSocket opening request did not reach the held-response fixture")
	}
	cancel()
	select {
	case <-done:
		completed = true
	case <-time.After(time.Second):
		t.Fatal("execution cancellation did not interrupt the held WebSocket handshake promptly")
	}
}

func TestWebSocketWorkerTerminationInterruptsHeldHandshake(t *testing.T) {
	fixture := newHeldWebSocketHandshake(t)
	worker := fmt.Sprintf(`
import {parentPort} from "node:worker_threads";
new WebSocket(%q);
parentPort.postMessage("started");
await new Promise(() => {});
`, fixture.webSocketURL())
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker = new Worker(%q, {eval: true, type: "module"});
const started = await new Promise((resolve, reject) => {
  worker.once("message", resolve);
  worker.once("error", reject);
});
if (started !== "started") throw Error("Worker did not start its socket");
const ready = await fetch(%q + "/ready");
if (!ready.ok) throw Error("server did not observe the Worker handshake");
const terminated = await Promise.race([
  worker.terminate().then(() => true),
  new Promise(resolve => setTimeout(() => resolve(false), 1000)),
]);
if (!terminated) throw Error("Worker.terminate() waited for the held handshake timeout");
const released = await fetch(%q + "/release");
if (!released.ok) throw Error("server could not release the Worker handshake");
const peer = await fetch(%q + "/peer-closed");
if (!peer.ok) throw Error("terminated Worker left the WebSocket peer open");
`, worker, fixture.server.URL, fixture.server.URL, fixture.server.URL)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	profile := stdlib.Profile{Modules: stdlib.Modules{
		Fetch:      &stdlib.FetchOptions{Client: fixture.server.Client()},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }, HandshakeTimeout: 4 * time.Second},
		Workers:    &stdlib.WorkerOptions{Max: 1},
	}}
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket Worker termination]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}

func TestWebSocketCloseValidatesCodeAndUTF8ReasonBeforeChangingState(t *testing.T) {
	requests := make(chan *http.Request, 1)
	server := websocketHeaderFixture(t, false, requests, false)
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http")
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
const closed = new Promise((resolve, reject) => {
  socket.addEventListener("open", () => {
    const invalid = [
      [2000, "", "InvalidAccessError"],
      [1000, "é".repeat(62), "SyntaxError"],
    ];
    for (const [code, reason, name] of invalid) {
      let error;
      try { socket.close(code, reason); } catch (caught) { error = caught; }
      if (!error || error.name !== name) reject(Error("close validation expected " + name));
      if (socket.readyState !== WebSocket.OPEN) reject(Error("invalid close changed OPEN state"));
    }
    socket.close();
  });
  socket.addEventListener("error", event => reject(event.error || Error(event.message)));
  socket.addEventListener("close", event => {
    if (socket.readyState !== WebSocket.CLOSED) reject(Error("close event saw non-CLOSED state"));
    if (!event.wasClean || event.code !== 1000) reject(Error("valid default close changed"));
    for (const [code, reason, name] of [[2000, "", "InvalidAccessError"], [1000, "é".repeat(62), "SyntaxError"]]) {
      let error;
      try { socket.close(code, reason); } catch (caught) { error = caught; }
      if (!error || error.name !== name) reject(Error("CLOSED close validation expected " + name));
    }
    resolve();
  });
});
await closed;
`, endpoint)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket close validation]", Virtual: true, Module: true, Code: code}, stdlib.Profile{
		Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }}},
	})
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	select {
	case <-requests:
	default:
		t.Fatal("WebSocket handshake did not reach the fixture")
	}
}

func TestWebSocketUpgradeClosedBeforeAttachDoesNotOpen(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	code := `
import {serve, upgradeWebSocket} from "noe:http";
const serverEvents = [];
const server = serve({port: 0}, async request => {
  const {socket, response} = upgradeWebSocket(request);
  const closed = new Promise(resolve => {
    socket.addEventListener("open", () => serverEvents.push("open"));
    socket.addEventListener("message", () => serverEvents.push("message"));
    socket.addEventListener("error", () => serverEvents.push("error"));
    socket.addEventListener("close", event => {
      serverEvents.push("close");
      if (socket.readyState !== WebSocket.CLOSED || event.code !== 1006 || event.wasClean)
        serverEvents.push("bad-close");
      resolve();
    });
  });
  socket.close();
  await closed;
  return response;
});
const client = new WebSocket(server.url.replace("http", "ws"));
const clientClosed = await new Promise((resolve, reject) => {
  const timer = setTimeout(() => reject(Error("client did not close after reserved socket cleanup")), 2000);
  client.addEventListener("error", () => {});
  client.addEventListener("close", event => { clearTimeout(timer); resolve(event); }, {once: true});
});
await server.close();
if (serverEvents.join(",") !== "error,close") throw Error("reserved server socket events: " + serverEvents.join(","));
if (client.readyState !== WebSocket.CLOSED || !clientClosed) throw Error("client lifecycle did not finish");
`
	profile := stdlib.Profile{Modules: stdlib.Modules{
		Serve:      &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }, HandshakeTimeout: 2 * time.Second},
	}}
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket upgrade closed before attach]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}
