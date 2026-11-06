package stdlib_test

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
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

type websocketWireFrame struct {
	opcode  byte
	payload []byte
}

// websocketClosePeer is a local raw WebSocket endpoint. Its handler owns the
// connection until the test callback returns; cleanup always closes it first
// so a failed assertion cannot leave a protocol read blocked.
type websocketClosePeer struct {
	server       *httptest.Server
	started      chan struct{}
	frameStarted chan struct{}
	stop         chan struct{}
	stopOnce     sync.Once
	handlerDone  chan struct{}
	doneOnce     sync.Once

	connMu sync.Mutex
	conn   net.Conn
}

func newWebSocketClosePeer(t *testing.T, smallReceiveBuffer bool, serve func(net.Conn, *bufio.ReadWriter, <-chan struct{}, chan struct{})) *websocketClosePeer {
	t.Helper()
	f := &websocketClosePeer{
		started:      make(chan struct{}),
		frameStarted: make(chan struct{}),
		stop:         make(chan struct{}),
		handlerDone:  make(chan struct{}),
	}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/write-started" {
			select {
			case <-f.frameStarted:
				w.WriteHeader(http.StatusNoContent)
			case <-time.After(3 * time.Second):
				http.Error(w, "WebSocket writer did not start", http.StatusGatewayTimeout)
			}
			return
		}
		defer f.doneOnce.Do(func() { close(f.handlerDone) })
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack WebSocket connection: %v", err)
			return
		}
		f.connMu.Lock()
		f.conn = conn
		f.connMu.Unlock()
		defer conn.Close()
		if tcp, ok := conn.(*net.TCPConn); ok && smallReceiveBuffer {
			if err := tcp.SetReadBuffer(4096); err != nil {
				t.Errorf("set small local peer receive buffer: %v", err)
			}
		}

		accept := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		if _, err := fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:])); err != nil {
			t.Errorf("write WebSocket handshake: %v", err)
			return
		}
		if err := rw.Flush(); err != nil {
			t.Errorf("flush WebSocket handshake: %v", err)
			return
		}
		close(f.started)
		serve(conn, rw, f.stop, f.frameStarted)
	}))
	t.Cleanup(func() {
		f.stopOnce.Do(func() { close(f.stop) })
		f.forceClose()
		select {
		case <-f.handlerDone:
		case <-time.After(time.Second):
			t.Error("local WebSocket fixture did not stop during cleanup")
		}
		f.server.Close()
	})
	return f
}

func (f *websocketClosePeer) url() string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + "/socket"
}

func (f *websocketClosePeer) forceClose() {
	f.connMu.Lock()
	conn := f.conn
	f.connMu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (f *websocketClosePeer) stopPeer() {
	f.stopOnce.Do(func() { close(f.stop) })
}

func readWebSocketWireFrame(r *bufio.Reader) (websocketWireFrame, error) {
	var header [2]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return websocketWireFrame{}, err
	}
	length := uint64(header[1] & 0x7f)
	switch length {
	case 126:
		var extended [2]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return websocketWireFrame{}, err
		}
		length = uint64(binary.BigEndian.Uint16(extended[:]))
	case 127:
		var extended [8]byte
		if _, err := io.ReadFull(r, extended[:]); err != nil {
			return websocketWireFrame{}, err
		}
		length = binary.BigEndian.Uint64(extended[:])
	}
	if length > 16<<20 {
		return websocketWireFrame{}, fmt.Errorf("unexpectedly large WebSocket frame: %d bytes", length)
	}
	masked := header[1]&0x80 != 0
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(r, mask[:]); err != nil {
			return websocketWireFrame{}, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(r, payload); err != nil {
		return websocketWireFrame{}, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i&3]
		}
	}
	return websocketWireFrame{opcode: header[0] & 0x0f, payload: payload}, nil
}

func writeWebSocketWireFrame(rw *bufio.ReadWriter, opcode byte, payload []byte) error {
	if len(payload) > 125 {
		return fmt.Errorf("test control frame payload exceeds 125 bytes: %d", len(payload))
	}
	if err := rw.WriteByte(0x80 | opcode); err != nil {
		return err
	}
	if err := rw.WriteByte(byte(len(payload))); err != nil {
		return err
	}
	if _, err := rw.Write(payload); err != nil {
		return err
	}
	return rw.Flush()
}

func closePayload(code uint16, reason string) []byte {
	payload := make([]byte, 2+len(reason))
	binary.BigEndian.PutUint16(payload, code)
	copy(payload[2:], reason)
	return payload
}

func runWebSocketClosingScript(t *testing.T, ctx context.Context, code string) error {
	t.Helper()
	profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{
		Allow: func(*url.URL) error { return nil },
	}}}
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket closing regression]", Virtual: true, Module: true, Code: code}, profile)
	return err
}

func TestWebSocketCloseWaitsForDelayedPeerAndPreservesQueuedFrames(t *testing.T) {
	type observed struct {
		messages []string
		close    websocketWireFrame
		err      error
	}
	observedFrame := make(chan observed, 1)
	peer := newWebSocketClosePeer(t, false, func(conn net.Conn, rw *bufio.ReadWriter, stop <-chan struct{}, _ chan struct{}) {
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		var result observed
		for {
			frame, err := readWebSocketWireFrame(rw.Reader)
			if err != nil {
				result.err = err
				observedFrame <- result
				return
			}
			switch frame.opcode {
			case 1:
				result.messages = append(result.messages, string(frame.payload))
			case 8:
				result.close = frame
				observedFrame <- result
				// This delay is longer than the old local 50 ms drop. The script
				// records whether its timer ran while close() was still pending.
				timer := time.NewTimer(250 * time.Millisecond)
				select {
				case <-stop:
					timer.Stop()
					return
				case <-timer.C:
				}
				if err := writeWebSocketWireFrame(rw, 8, frame.payload); err != nil {
					select {
					case <-stop:
						return
					default:
						t.Errorf("write delayed close response: %v", err)
					}
				}
				return
			default:
				result.err = fmt.Errorf("unexpected client frame opcode %d", frame.opcode)
				observedFrame <- result
				return
			}
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
let closeSeen = false;
let timerProgress = false;
let ended;
let finishTimer;
const timer = new Promise(resolve => { finishTimer = resolve; });
const closed = new Promise((resolve, reject) => {
  socket.onopen = () => {
    socket.send("first");
    socket.send("second");
    socket.close(4001, "local reason");
    socket.close(4002, "ignored repeat");
    if (socket.readyState !== WebSocket.CLOSING) reject(Error("close() was not synchronous"));
    setTimeout(() => {
      timerProgress = true;
      finishTimer(!closeSeen && socket.readyState === WebSocket.CLOSING);
    }, 100);
  };
  socket.onerror = event => reject(event.error || Error(event.message));
  socket.onclose = event => {
    closeSeen = true;
    ended = {code: event.code, reason: event.reason, clean: event.wasClean, state: socket.readyState};
    resolve();
  };
});
await closed;
if (!await timer || !timerProgress) throw Error("close did not wait while timers progressed");
if (!ended.clean || ended.code !== 4001 || ended.reason !== "local reason" || ended.state !== WebSocket.CLOSED)
  throw Error("delayed close response was not reported cleanly: " + JSON.stringify(ended));
`, peer.url())
	done := make(chan error, 1)
	go func() { done <- runWebSocketClosingScript(t, ctx, code) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(stdlib.Diagnostic(err))
		}
	case <-ctx.Done():
		peer.forceClose()
		t.Fatal("WebSocket did not finish after the delayed close response")
	}
	select {
	case got := <-observedFrame:
		if got.err != nil {
			t.Fatal(got.err)
		}
		if strings.Join(got.messages, ",") != "first,second" {
			t.Fatalf("queued messages were sent out of order: %q", got.messages)
		}
		if got.close.opcode != 8 || string(got.close.payload) != string(closePayload(4001, "local reason")) {
			t.Fatalf("close frame payload=%v; want %v", got.close.payload, closePayload(4001, "local reason"))
		}
	case <-time.After(time.Second):
		t.Fatal("peer did not observe the client's close frame")
	}
}

func TestWebSocketNoArgumentCloseUsesEmptyPayloadAndReports1005OnEcho(t *testing.T) {
	frameSeen := make(chan websocketWireFrame, 1)
	peer := newWebSocketClosePeer(t, false, func(_ net.Conn, rw *bufio.ReadWriter, _ <-chan struct{}, _ chan struct{}) {
		frame, err := readWebSocketWireFrame(rw.Reader)
		if err != nil {
			t.Errorf("read client close frame: %v", err)
			return
		}
		frameSeen <- frame
		if frame.opcode != 8 {
			t.Errorf("got frame opcode %d, want close", frame.opcode)
			return
		}
		if err := writeWebSocketWireFrame(rw, 8, nil); err != nil {
			t.Errorf("write empty close echo: %v", err)
		}
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
await new Promise((resolve, reject) => {
  socket.onopen = () => socket.close();
  socket.onerror = event => reject(event.error || Error(event.message));
  socket.onclose = event => {
    if (socket.readyState !== WebSocket.CLOSED || event.code !== 1005 || event.reason !== "" || !event.wasClean)
      reject(Error("empty close echo details or state changed"));
    else resolve();
  };
});
`, peer.url())
	if err := runWebSocketClosingScript(t, ctx, code); err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	select {
	case frame := <-frameSeen:
		if frame.opcode != 8 || len(frame.payload) != 0 {
			t.Fatalf("no-argument close payload=%v; want empty close frame", frame.payload)
		}
	case <-time.After(time.Second):
		t.Fatal("peer did not observe no-argument close")
	}
}

func TestWebSocketPeerInitiatedCloseIsEchoedBeforeCleanTerminalEvent(t *testing.T) {
	peerClose := closePayload(4103, "peer initiated")
	ack := make(chan websocketWireFrame, 1)
	peer := newWebSocketClosePeer(t, false, func(conn net.Conn, rw *bufio.ReadWriter, _ <-chan struct{}, _ chan struct{}) {
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		if err := writeWebSocketWireFrame(rw, 8, peerClose); err != nil {
			t.Errorf("send peer close: %v", err)
			return
		}
		frame, err := readWebSocketWireFrame(rw.Reader)
		if err != nil {
			t.Errorf("read close response: %v", err)
			return
		}
		ack <- frame
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
await new Promise((resolve, reject) => {
  socket.onerror = event => reject(event.error || Error(event.message));
  socket.onclose = event => {
    if (socket.readyState !== WebSocket.CLOSED || event.code !== 4103 || event.reason !== "peer initiated" || !event.wasClean)
      reject(Error("peer close did not finish cleanly: " + JSON.stringify({code:event.code, reason:event.reason, wasClean:event.wasClean})));
    else resolve();
  };
});
`, peer.url())
	if err := runWebSocketClosingScript(t, ctx, code); err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	select {
	case frame := <-ack:
		if frame.opcode != 8 || string(frame.payload) != string(peerClose) {
			t.Fatalf("peer close response=%v; want echoed payload %v", frame.payload, peerClose)
		}
	case <-time.After(time.Second):
		t.Fatal("terminal close event completed without a peer-visible close response")
	}
}

func TestWebSocketAbruptEOFEmitsErrorThenUncleanCloseWithEmptyReason(t *testing.T) {
	peer := newWebSocketClosePeer(t, false, func(net.Conn, *bufio.ReadWriter, <-chan struct{}, chan struct{}) {})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
const events = [];
await new Promise((resolve, reject) => {
  socket.onerror = () => {
    events.push("error");
    if (socket.readyState !== WebSocket.CLOSED) reject(Error("error handler saw a non-CLOSED socket"));
  };
  socket.onclose = event => {
    events.push("close");
    if (socket.readyState !== WebSocket.CLOSED || event.code !== 1006 || event.reason !== "" || event.wasClean)
      reject(Error("abrupt EOF close details or state changed"));
    else resolve();
  };
});
if (events.join(",") !== "error,close") throw Error("unexpected event order: " + events.join(","));
`, peer.url())
	if err := runWebSocketClosingScript(t, ctx, code); err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}

func TestWebSocketLocalCloseWithoutPeerAckTimesOutUncleanly(t *testing.T) {
	closeFrameSeen := make(chan struct{})
	peer := newWebSocketClosePeer(t, false, func(conn net.Conn, rw *bufio.ReadWriter, _ <-chan struct{}, _ chan struct{}) {
		conn.SetReadDeadline(time.Now().Add(7 * time.Second))
		frame, err := readWebSocketWireFrame(rw.Reader)
		if err != nil {
			t.Errorf("read client close frame: %v", err)
			return
		}
		if frame.opcode != 8 {
			t.Errorf("got frame opcode %d, want close", frame.opcode)
			return
		}
		close(closeFrameSeen)
		var buf [256]byte
		_, _ = rw.Read(buf[:])
	})
	ctx, cancel := context.WithTimeout(t.Context(), 7*time.Second)
	defer cancel()
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
let closeCalledAt;
await new Promise((resolve, reject) => {
  socket.onopen = () => {
    closeCalledAt = Date.now();
    socket.close(4004, "no peer ack");
    if (socket.readyState !== WebSocket.CLOSING) reject(Error("close() did not synchronously enter CLOSING"));
  };
  socket.onclose = event => {
    const elapsed = Date.now() - closeCalledAt;
    if (socket.readyState !== WebSocket.CLOSED || event.code !== 1006 || event.reason !== "" || event.wasClean)
      reject(Error("missing peer ACK was not reported uncleanly"));
    else if (elapsed < 4300 || elapsed > 6500)
      reject(Error("missing peer ACK timeout was not bounded near five seconds: " + elapsed));
    else resolve();
  };
});
`, peer.url())
	if err := runWebSocketClosingScript(t, ctx, code); err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	select {
	case <-closeFrameSeen:
	case <-time.After(time.Second):
		t.Fatal("peer did not observe local close before the timeout")
	}
}

func TestWebSocketSimultaneousCloseReportsFirstPeerCodeAndReasonOnBothSockets(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	code := `
import {serve, upgradeWebSocket} from "noe:http";
let finishServerClose;
const serverClosed = new Promise(resolve => { finishServerClose = resolve; });
const server = serve({port: 0}, request => {
  const {socket, response} = upgradeWebSocket(request);
  socket.addEventListener("message", event => {
    if (event.data === "start-close") {
      socket.send("server-closing");
      socket.close(4202, "server reason");
    }
  });
  socket.addEventListener("close", event => finishServerClose({
    code: event.code, reason: event.reason, clean: event.wasClean, state: socket.readyState,
  }));
  return response;
});
const client = new WebSocket(server.url.replace("http", "ws"));
let finishClientClose;
const clientClosed = new Promise(resolve => { finishClientClose = resolve; });
client.addEventListener("open", () => client.send("start-close"));
client.addEventListener("message", event => {
  if (event.data === "server-closing") client.close(4201, "client reason");
});
client.addEventListener("close", event => finishClientClose({
  code: event.code, reason: event.reason, clean: event.wasClean, state: client.readyState,
}));
let clientEnd, serverEnd;
try {
  [clientEnd, serverEnd] = await Promise.all([clientClosed, serverClosed]);
} finally {
  client.close();
  await server.close();
}
for (const [name, end, code, reason] of [
  ["client", clientEnd, 4202, "server reason"],
  ["server", serverEnd, 4201, "client reason"],
]) {
  if (!end || end.state !== WebSocket.CLOSED || end.code !== code || end.reason !== reason || !end.clean)
    throw Error(name + " did not report its first peer close: " + JSON.stringify(end));
}
`
	profile := stdlib.Profile{Modules: stdlib.Modules{
		Serve:      &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }},
	}}
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket simultaneous close]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}

func TestWebSocketExecutionCancellationInterruptsBlockedSend(t *testing.T) {
	writeStarted := make(chan struct{})
	peer := newWebSocketClosePeer(t, true, func(conn net.Conn, rw *bufio.ReadWriter, stop <-chan struct{}, _ chan struct{}) {
		conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		var header [2]byte
		if _, err := io.ReadFull(rw.Reader, header[:]); err != nil {
			t.Errorf("wait for first outbound WebSocket frame: %v", err)
			return
		}
		if header[0]&0x0f != 1 {
			t.Errorf("first outbound frame opcode=%d; want text", header[0]&0x0f)
			return
		}
		close(writeStarted)
		// Leave the payload unread. A multi-megabyte send with the small TCP
		// receive buffer fills the local window and blocks the native writer.
		<-stop
	})
	ctx, cancel := context.WithCancel(t.Context())
	code := fmt.Sprintf(`
const socket = new WebSocket(%q);
await new Promise((resolve, reject) => {
  socket.onopen = () => {
    socket.send("x".repeat(4 * 1024 * 1024));
    resolve();
  };
  socket.onerror = event => reject(event.error || Error(event.message));
});
await new Promise(() => {});
`, peer.url())
	profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{
		Allow: func(*url.URL) error { return nil },
	}}}
	done := make(chan error, 1)
	go func() {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket blocked send cancellation]", Virtual: true, Module: true, Code: code}, profile)
		done <- err
	}()
	completed := false
	defer func() {
		cancel()
		peer.forceClose()
		if !completed {
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Error("Execute remained blocked after the WebSocket peer was forcibly closed")
			}
		}
	}()
	select {
	case <-peer.started:
	case <-time.After(time.Second):
		t.Fatal("WebSocket handshake did not reach the local peer")
	}
	select {
	case <-writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native writer did not start the queued large frame")
	}
	// Let the limited receive window fill before cancellation interrupts the
	// blocked WriteMessage call.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-done:
		completed = true
	case <-time.After(time.Second):
		t.Fatal("execution cancellation did not interrupt the blocked WebSocket send")
	}
	peer.stopPeer()
}

func TestWebSocketWorkerTerminationInterruptsBlockedSend(t *testing.T) {
	peer := newWebSocketClosePeer(t, true, func(conn net.Conn, rw *bufio.ReadWriter, stop <-chan struct{}, frameStarted chan struct{}) {
		conn.SetReadDeadline(time.Now().Add(4 * time.Second))
		var header [2]byte
		if _, err := io.ReadFull(rw.Reader, header[:]); err != nil {
			t.Errorf("wait for Worker WebSocket frame header: %v", err)
			return
		}
		if header[0]&0x0f != 1 {
			t.Errorf("Worker's first outbound frame opcode=%d; want text", header[0]&0x0f)
			return
		}
		close(frameStarted)
		// Keep the peer connected without consuming the multi-megabyte payload.
		<-stop
	})
	ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
	defer cancel()
	workerSource := fmt.Sprintf(`
import {parentPort} from "node:worker_threads";
const socket = new WebSocket(%q);
socket.addEventListener("open", () => {
  socket.send("x".repeat(4 * 1024 * 1024));
  parentPort.postMessage("queued");
});
await new Promise(() => {});
`, peer.url())
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker = new Worker(%q, {eval: true, type: "module"});
try {
  const started = await new Promise((resolve, reject) => {
    worker.once("message", resolve);
    worker.once("error", reject);
  });
  if (started !== "queued") throw Error("Worker did not queue its large send");
  const writing = await fetch(%q + "/write-started");
  if (!writing.ok) throw Error("Worker's native WebSocket writer did not start");
  await new Promise(resolve => setTimeout(resolve, 100));
  const termination = worker.terminate();
  const terminated = await Promise.race([
    termination.then(() => true),
    new Promise(resolve => setTimeout(() => resolve(false), 1000)),
  ]);
  if (!terminated) throw Error("Worker.terminate() waited for the blocked WebSocket send");
  let timerProgress = false;
  await new Promise(resolve => setTimeout(() => { timerProgress = true; resolve(); }, 10));
  if (!timerProgress || 1 + 1 !== 2) throw Error("parent runtime did not continue after Worker termination");
} finally {
  await Promise.race([
    worker.terminate(),
    new Promise(resolve => setTimeout(resolve, 1000)),
  ]);
}
`, workerSource, peer.server.URL)
	profile := stdlib.Profile{Modules: stdlib.Modules{
		Fetch: &stdlib.FetchOptions{Client: peer.server.Client()},
		WebSockets: &stdlib.WebSocketsOptions{
			Allow: func(*url.URL) error { return nil },
		},
		Workers: &stdlib.WorkerOptions{Max: 1},
	}}
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket Worker blocked send]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}
