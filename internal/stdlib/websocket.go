package stdlib

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib/internal/wsproto"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// WebSockets describes the sockets a runtime may open and accept.
//
// A socket is a connection that stays open, so it is network access of the
// longest-lived kind: the zero value opens nothing, and Allow decides for every
// address a script tries to reach.
type WebSockets struct {
	// Loop is where a message is delivered, and is required: a socket that
	// nothing is listening on is a socket that does nothing.
	Loop *Loop
	// Allow is asked before a socket is opened, with the URL it would open.
	// Nil refuses everything, which is what a zero value means.
	Allow func(target *url.URL) error
	// Serve is the server sockets are accepted on. Without it a script can
	// open a socket but not answer one, since there is nothing listening.
	Serve *Serve
	// MaxMessageBytes bounds one message. Zero means 16 MB.
	MaxMessageBytes int
	// HandshakeTimeout bounds the complete TCP/TLS/HTTP opening. Zero means 30s.
	HandshakeTimeout time.Duration
	// TLS is the configuration for a wss connection. Nil uses the defaults.
	TLS *tls.Config
}

// Sockets installs the standard WebSocket global and returns selected host
// helpers for publication through noe:http.
//
//	const socket = new WebSocket("wss://example.com/feed")
//	socket.addEventListener("message", (e) => console.log(e.data))
//	socket.addEventListener("open", () => socket.send("hello"))
//
// On the other side, a handler answers a request by taking it over:
//
//	import {serve, upgradeWebSocket} from "noe:http"
//
//	serve({port: 8080}, (request) => {
//	    const {socket, response} = upgradeWebSocket(request)
//	    socket.onmessage = (e) => socket.send("you said " + e.data)
//	    return response
//	})
//
// What arrives is a message, not a stream of bytes: the protocol's frames, its
// fragments and its pings are handled here, and what the script sees is what
// the other end sent.
func Sockets(rt *quickjs.Runtime, cfg *WebSockets) (map[string]quickjs.Value, error) {
	if cfg == nil {
		cfg = &WebSockets{}
	}
	if cfg.Loop == nil {
		return nil, errors.New("stdlib: sockets need an event loop to deliver on")
	}
	h := &wsHost{rt: rt, cfg: cfg, pending: map[string]*socket{}}

	host := rt.NewObject()
	if err := setAll(host, map[string]any{
		"connect": h.connect,
		"reserve": h.reserve,
		"serving": cfg.Serve != nil,
	}); err != nil {
		return nil, err
	}
	if cfg.Serve != nil {
		// The server hands a taken-over connection back here, which is the
		// only way a socket arrives from the outside.
		cfg.Serve.upgrade = h.attach
	}

	api, err := evalWithHost(rt, "<websocket>", webSocketJS, host)
	if err != nil {
		return nil, err
	}
	webSocket, err := api.Get("WebSocket")
	if err != nil {
		return nil, err
	}
	if err := rt.Set("WebSocket", webSocket); err != nil {
		return nil, err
	}
	helpers := make(map[string]quickjs.Value)
	for _, name := range []string{"createWebSocket", "upgradeWebSocket"} {
		if name == "upgradeWebSocket" && cfg.Serve == nil {
			continue
		}
		v, err := api.Get(name)
		if err != nil {
			return nil, err
		}
		helpers[name] = v
	}
	return helpers, nil
}

type wsHost struct {
	rt  *quickjs.Runtime
	cfg *WebSockets
	// pending holds the sockets a handler has asked for but the connection has
	// not reached yet, by the token the answer carries.
	pending map[string]*socket
	nextID  int64
}

// socket is one WebSocket as the host sees it.
//
// Everything that touches the runtime happens on the loop; the connection is
// read and written on goroutines of its own, and the two meet at Post.
type socket struct {
	rt   *quickjs.Runtime
	loop *Loop
	host *wsHost

	// opened settles when the handshake has finished, one way or the other.
	opened *quickjs.Promise
	// closing settles on the owner after transport cleanup. Forced teardown
	// leaves JS settlement to the execution that still owns the runtime.
	closing *quickjs.Promise
	conn    *wsproto.Conn
	// unwatch stops the connection being closed with the loop, and unhook
	// with the runtime, once it has been closed anyway.
	unwatch      func() bool
	unhook       func()
	scopeUnhook  func()
	openCancel   context.CancelFunc
	pendingToken string

	mu            sync.Mutex
	queue         []outgoing
	signal        chan struct{}
	closed        bool
	closeWriting  bool
	wireSent      bool
	wireReceived  bool
	wireComplete  bool
	wirePayload   []byte
	handshakeDone chan struct{}
	readMu        sync.Mutex

	// Published protocol state is owner-only; native wire progress uses mu.
	closeStarted bool
	closePayload []byte
	closeStop    chan struct{}
	closeWritten bool
	peerClosed   bool
	peerCode     int
	peerReason   string
	settled      bool
	failure      error

	// reading is true while a read is outstanding, since only one may be.
	reading  bool
	draining bool
	// ended stops delivery; finished also accounts for native hook cleanup.
	ended    bool
	finished bool
	// released says the hold this socket put on the loop has been given back.
	released bool
}

type outgoing struct {
	op   byte
	data []byte
}

const webSocketCloseTimeout = 5 * time.Second

func (h *wsHost) limit() int {
	if h.cfg.MaxMessageBytes > 0 {
		return h.cfg.MaxMessageBytes
	}
	return 16 << 20
}

// connect opens a socket to a URL and hands back the object the script drives
// it through. The dialling happens on another goroutine, so the call returns
// before the connection exists and the script waits on opened.
func (h *wsHost) connect(rawURL string, protocols, headerPairs quickjs.Value) (quickjs.Value, error) {
	target, err := url.Parse(rawURL)
	if err != nil {
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("SyntaxError",
			"that is not a URL: "+rawURL))
	}
	switch target.Scheme {
	case "ws", "wss":
	default:
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("SyntaxError",
			"a socket needs a ws: or wss: URL, not "+target.Scheme+":"))
	}
	headers, err := readWebSocketHeaders(headerPairs)
	if err != nil {
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("TypeError", err.Error()))
	}
	names := stringsOf(protocols)
	for _, name := range names {
		if !webSocketHeaderToken(name) {
			return quickjs.Value{}, h.rt.Throw(h.rt.NewError("SyntaxError", "invalid WebSocket protocol name"))
		}
	}
	if h.cfg.Allow == nil {
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("Error",
			"opening a socket is not allowed"))
	}
	if err := h.cfg.Allow(target); err != nil {
		return quickjs.Value{}, h.rt.Throw(h.rt.NewError("Error", err.Error()))
	}

	s, err := h.newSocket()
	if err != nil {
		return quickjs.Value{}, err
	}
	timeout := h.cfg.HandshakeTimeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	tlsCfg := h.cfg.TLS
	limit := h.limit()

	loop := h.cfg.Loop
	openingCtx, cancel := context.WithCancel(loopContext(loop))
	s.openCancel = cancel
	loop.Begin()
	if err := loop.Go(func(context.Context) {
		defer loop.Done()
		conn, protocol, err := dialWebSocket(openingCtx, target, names, headers, timeout, tlsCfg, limit)
		var removeConnCleanup func()
		var stopDelivery func() bool
		if err == nil && conn != nil {
			// Delivery can be discarded during shutdown, including on a legacy
			// loop without an execution scope. Keep the connection cancellable
			// until start installs its active-connection hooks on the owner.
			stopDelivery = context.AfterFunc(openingCtx, conn.Close)
			if scope := loop.executionScope(); scope != nil {
				removeConnCleanup, err = scope.AddCleanupHandle(func() { conn.Close() })
				if err != nil {
					conn.Close()
					return
				}
			}
		}
		loop.postExecution(func() {
			if removeConnCleanup != nil {
				defer removeConnCleanup()
			}
			if stopDelivery != nil {
				stopDelivery()
			}
			defer s.cancelOpening()
			if s.ended {
				if conn != nil {
					conn.Close()
				}
				return
			}
			if err == nil {
				err = openingCtx.Err()
			}
			if err != nil {
				if conn != nil {
					conn.Close()
				}
				s.opened.RejectError(err)
				s.finish()
				return
			}
			if err := s.start(conn); err != nil {
				s.opened.RejectError(err)
				return
			}
			o := s.rt.NewObject()
			o.Set("protocol", protocol)
			s.opened.Resolve(o)
		})
	}); err != nil {
		loop.Done()
		s.finish()
		s.opened.RejectError(err)
	}
	return s.value()
}

// reserve makes a socket that has no connection yet, for a handler that means
// to take one over. The token comes back with the answer, which is how the
// connection finds its way to this socket.
func (h *wsHost) reserve() (quickjs.Value, error) {
	h.nextID++
	token := "ws-" + strconv.FormatInt(h.nextID, 10)
	s, err := h.newSocket()
	if err != nil {
		return quickjs.Value{}, err
	}
	h.pending[token] = s
	s.pendingToken = token

	v, err := s.value()
	if err != nil {
		return quickjs.Value{}, err
	}
	out := h.rt.NewObject()
	if err := errors.Join(out.Set("token", token), out.Set("handle", v)); err != nil {
		return quickjs.Value{}, err
	}
	return out, nil
}

// attach is what the server calls once it has taken a connection over. It runs
// on the loop, like everything else that touches a socket's runtime side.
func (h *wsHost) attach(token string, conn net.Conn, br *bufio.Reader, protocol string) {
	s := h.pending[token]
	delete(h.pending, token)
	if s == nil || s.ended {
		conn.Close()
		return
	}
	s.pendingToken = ""
	if err := s.start(wsproto.NewConn(conn, br, false, h.limit())); err != nil {
		s.opened.RejectError(err)
		return
	}
	o := s.rt.NewObject()
	o.Set("protocol", protocol)
	s.opened.Resolve(o)
}

func (h *wsHost) newSocket() (*socket, error) {
	s := &socket{
		rt:            h.rt,
		loop:          h.cfg.Loop,
		host:          h,
		opened:        h.rt.NewPromise(),
		closing:       h.rt.NewPromise(),
		handshakeDone: make(chan struct{}),
		signal:        make(chan struct{}, 1),
	}
	if scope := h.cfg.Loop.executionScope(); scope != nil {
		var err error
		s.scopeUnhook, err = scope.AddCleanupHandle(s.finish)
		if err != nil {
			return nil, err
		}
	}
	return s, nil
}

// value is the object the script holds: the handshake, the next message, and
// the two ways to end it.
func (s *socket) value() (quickjs.Value, error) {
	o := s.rt.NewObject()
	if err := errors.Join(
		o.Set("opened", s.opened.Value()),
		o.Set("closed", s.closing.Value()),
		o.Set("receive", s.receive),
		o.Set("send", s.send),
		o.Set("close", s.closeWith),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

// start takes ownership of an open connection and begins writing whatever the
// script has queued.
func (s *socket) start(conn *wsproto.Conn) error {
	if s.ended {
		conn.Close()
		return errors.New("the socket was closed before the connection opened")
	}
	s.conn = conn
	// The loop is held open while the socket is: a program whose last act is
	// to open one is waiting for what comes back.
	s.loop.Begin()
	// And the connection is closed with the loop, and with the runtime,
	// before its Close returns.
	s.unwatch = context.AfterFunc(loopContext(s.loop), conn.Close)
	s.unhook = s.rt.OnClose(conn.Close)
	if err := s.loop.Go(func(ctx context.Context) { s.writing(ctx) }); err != nil {
		s.finish()
		return err
	}
	return nil
}

// writing is the one goroutine that writes to the connection, so that messages
// leave in the order the script sent them.
func (s *socket) writing(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		if len(s.queue) == 0 {
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			select {
			case <-s.signal:
			case <-ctx.Done():
				return
			}
			continue
		}
		m := s.queue[0]
		s.queue[0] = outgoing{}
		s.queue = s.queue[1:]
		if m.op == wsproto.OpClose {
			s.closeWriting = true
		}
		s.mu.Unlock()

		if err := s.conn.WriteMessage(m.op, m.data); err != nil {
			s.conn.Close()
			s.loop.postExecution(func() { s.failClose(err) })
			return
		}
		if m.op == wsproto.OpClose {
			s.recordCloseFrame(true, nil)
			s.loop.postExecution(func() {
				s.closeWritten = true
				s.maybeCompleteClose()
			})
			return
		}
	}
}

// Native completion stops the deadline even while JavaScript is busy. Both
// workers record successful frames before posting their owner-side results.
func (s *socket) recordCloseFrame(sent bool, payload []byte) {
	s.mu.Lock()
	if sent {
		s.wireSent = true
	} else {
		s.wireReceived = true
		s.wirePayload = append([]byte(nil), payload...)
	}
	complete := s.wireSent && s.wireReceived && !s.wireComplete
	if complete {
		s.wireComplete = true
		close(s.handshakeDone)
	}
	s.mu.Unlock()
	if complete {
		s.conn.Close()
	}
}

// Closing has a native reader even when the JS owner is busy. Serialize it
// with an ordinary read and retain the first validated peer close result.
func (s *socket) readMessage() (byte, []byte, error) {
	s.readMu.Lock()
	defer s.readMu.Unlock()
	s.mu.Lock()
	received, payload := s.wireReceived, s.wirePayload
	s.mu.Unlock()
	if received {
		return wsproto.OpClose, payload, nil
	}
	op, payload, err := s.conn.ReadMessage()
	if err == nil && op == wsproto.OpClose {
		if _, _, err = wsproto.ParseClose(payload); err == nil {
			s.recordCloseFrame(false, payload)
		}
	}
	if err != nil {
		s.conn.Close()
	}
	return op, payload, err
}

// send queues a message. The script's bytes are copied, since the writing
// happens elsewhere and what it holds is its own.
func (s *socket) send(data quickjs.Value) error {
	if s.conn == nil {
		return s.rt.Throw(s.rt.NewError("Error", "this socket is not open yet"))
	}
	op := byte(wsproto.OpText)
	var payload []byte
	if b, ok := data.Bytes(); ok {
		op = wsproto.OpBinary
		payload = append([]byte(nil), b...)
	} else {
		payload = []byte(data.String())
	}
	s.enqueue(outgoing{op: op, data: payload})
	return nil
}

func (s *socket) enqueue(m outgoing) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.queue = append(s.queue, m)
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
}

// receive answers with the next message or a transition to the closing result.
func (s *socket) receive() *quickjs.Promise {
	p := s.rt.NewPromise()
	if s.ended || s.closeStarted || s.conn == nil {
		s.deliverClosing(p)
		return p
	}
	if s.reading {
		p.RejectError(errors.New("this socket is already being read"))
		return p
	}
	s.reading = true

	loop := s.loop
	loop.Begin()
	if err := loop.Go(func(context.Context) {
		defer loop.Done()
		op, payload, err := s.readMessage()
		loop.postExecution(func() {
			s.reading = false
			if s.ended {
				s.deliverClosing(p)
				return
			}
			if err != nil {
				s.failClose(err)
				s.deliverClosing(p)
				return
			}
			if op == wsproto.OpClose {
				code, reason := wsproto.CloseInfo(payload)
				s.peerClosed, s.peerCode, s.peerReason = true, code, reason
				if s.failure != nil {
					s.completeClose(s.failure)
				} else {
					s.beginClosing(payload, true)
					s.maybeCompleteClose()
				}
				s.deliverClosing(p)
				return
			}
			if s.failure != nil {
				s.failClose(s.failure)
				s.deliverClosing(p)
				return
			}
			out := s.rt.NewObject()
			if op == wsproto.OpBinary {
				out.Set("type", "binary")
				out.Set("data", s.rt.NewBytes(payload))
			} else {
				out.Set("type", "text")
				out.Set("data", string(payload))
			}
			p.Resolve(out)
		})
	}); err != nil {
		loop.Done()
		s.reading = false
		s.completeClose(err)
		s.deliverClosing(p)
	}
	return p
}

func (s *socket) deliverClosing(p *quickjs.Promise) {
	out := s.rt.NewObject()
	out.Set("type", "closing")
	p.Resolve(out)
}

// closeWith starts the closing handshake after the previously accepted sends.
// Zero means no status code, so a no-argument close has an empty wire payload.
func (s *socket) closeWith(code quickjs.Value, reason quickjs.Value) {
	if s.ended || s.closeStarted {
		return
	}
	if s.conn == nil {
		s.opened.RejectError(errors.New("the socket was closed before the connection opened"))
		s.finish()
		return
	}
	n := code.Int()
	var payload []byte
	if n != 0 {
		payload = append(payload, byte(n>>8), byte(n))
		payload = append(payload, reason.String()...)
	}
	s.beginClosing(payload, false)
}

// beginClosing is owner-only. Peer close discards unsent data and prioritizes
// the close response after the one frame that may already be writing.
func (s *socket) beginClosing(payload []byte, peer bool) {
	if s.ended {
		return
	}
	first := !s.closeStarted
	if first {
		s.closeStarted = true
		s.closePayload = append([]byte(nil), payload...)
	}
	s.mu.Lock()
	if first {
		s.closed = true
		if peer {
			s.queue = nil
		}
		s.queue = append(s.queue, outgoing{op: wsproto.OpClose, data: s.closePayload})
	} else if peer && !s.closeWriting {
		s.queue = []outgoing{{op: wsproto.OpClose, data: s.closePayload}}
	}
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
	if !first {
		return
	}

	s.closeStop = make(chan struct{})
	loop, conn, stop := s.loop, s.conn, s.closeStop
	loop.Begin()
	if err := loop.Go(func(ctx context.Context) {
		defer loop.Done()
		timer := time.NewTimer(webSocketCloseTimeout)
		defer timer.Stop()
		select {
		case <-stop:
			return
		case <-s.handshakeDone:
			return
		case <-ctx.Done():
			conn.Close()
			return
		case <-timer.C:
			select {
			case <-s.handshakeDone:
				return
			default:
			}
			conn.Close()
			loop.postExecution(func() {
				s.failClose(errors.New("the WebSocket closing handshake timed out"))
			})
		}
	}); err != nil {
		loop.Done()
		s.completeClose(err)
	}
	if !peer && !s.ended {
		s.drainClosing()
	}
}

func (s *socket) drainClosing() {
	s.draining = true
	loop := s.loop
	loop.Begin()
	if err := loop.Go(func(context.Context) {
		defer loop.Done()
		for {
			op, payload, err := s.readMessage()
			if err == nil && op != wsproto.OpClose {
				continue
			}
			loop.postExecution(func() {
				s.draining = false
				if s.ended {
					return
				}
				if err != nil {
					if s.failure != nil {
						err = s.failure
					}
					s.completeClose(err)
					return
				}
				s.peerClosed = true
				s.peerCode, s.peerReason = wsproto.CloseInfo(payload)
				if s.failure != nil {
					s.completeClose(s.failure)
				} else {
					s.beginClosing(payload, true)
					s.maybeCompleteClose()
				}
			})
			return
		}
	}); err != nil {
		loop.Done()
		s.draining = false
		s.failClose(err)
	}
}

// An outstanding read may already contain the peer's first Close frame.
// Let that result reach the owner before fixing the terminal code/reason.
func (s *socket) failClose(err error) {
	if s.ended {
		return
	}
	if s.failure == nil {
		s.failure = err
	}
	s.conn.Close()
	if !s.reading && !s.draining {
		s.completeClose(s.failure)
	}
}

func (s *socket) maybeCompleteClose() {
	if s.closeWritten && s.peerClosed {
		s.completeClose(s.failure)
	}
}

// completeClose settles protocol results only on the owner. Native force
// cleanup continues to use finish directly and never touches JS promises.
func (s *socket) completeClose(err error) {
	if s.settled || s.finished {
		return
	}
	s.settled = true
	clean := err == nil && s.closeWritten && s.peerClosed
	code, reason := 1006, ""
	if s.peerClosed {
		code, reason = s.peerCode, s.peerReason
	} else {
		s.mu.Lock()
		if s.wireReceived {
			code, reason = wsproto.CloseInfo(s.wirePayload)
		}
		s.mu.Unlock()
	}
	s.finish()
	out := s.rt.NewObject()
	out.Set("code", code)
	out.Set("reason", reason)
	out.Set("clean", clean)
	if err != nil {
		out.Set("error", s.rt.NewError("Error", err.Error()))
	}
	s.closing.Resolve(out)
}

// finish marks the socket over and lets the loop go.
func (s *socket) finish() {
	if s.finished {
		return
	}
	s.finished = true
	s.ended = true
	s.cancelOpening()
	if s.pendingToken != "" {
		delete(s.host.pending, s.pendingToken)
		s.pendingToken = ""
	}
	if s.closeStop != nil {
		close(s.closeStop)
		s.closeStop = nil
	}
	s.mu.Lock()
	s.closed = true
	s.queue = nil
	s.mu.Unlock()
	select {
	case s.signal <- struct{}{}:
	default:
	}
	if s.conn != nil {
		s.conn.Close()
	}
	if s.unwatch != nil {
		s.unwatch()
	}
	if s.unhook != nil {
		s.unhook()
	}
	if s.scopeUnhook != nil {
		s.scopeUnhook()
	}
	s.releaseLoop()
}

func (s *socket) cancelOpening() {
	if s.openCancel != nil {
		s.openCancel()
		s.openCancel = nil
	}
}

// releaseLoop undoes the Begin that start made, once.
func (s *socket) releaseLoop() {
	if s.conn == nil || s.released {
		return
	}
	s.released = true
	s.loop.Done()
}

// dialWebSocket opens the connection and does the handshake. It touches no
// JavaScript value, which is what lets it run on another goroutine.
func dialWebSocket(ctx context.Context, target *url.URL, protocols []string, headers http.Header, timeout time.Duration, tlsCfg *tls.Config, limit int) (*wsproto.Conn, string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	host := target.Host
	if target.Port() == "" {
		if target.Scheme == "wss" {
			host = net.JoinHostPort(host, "443")
		} else {
			host = net.JoinHostPort(host, "80")
		}
	}

	dialer := &net.Dialer{}
	var conn net.Conn
	var err error
	if target.Scheme == "wss" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", host)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", host)
	}
	if err != nil {
		return nil, "", err
	}
	// DialContext stops governing plain TCP after it connects. Cancellation
	// must also interrupt the HTTP handshake's blocked reads and writes.
	closed := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		conn.Close()
		close(closed)
	})
	var stopOnce sync.Once
	stopHandshake := func() {
		stopOnce.Do(func() {
			if !stop() {
				<-closed
			}
		})
	}
	defer stopHandshake()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)

	key, err := wsproto.Key()
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	path := target.RequestURI()
	if path == "" {
		path = "/"
	}
	var req strings.Builder
	fmt.Fprintf(&req, "GET %s HTTP/1.1\r\n", path)
	fmt.Fprintf(&req, "Host: %s\r\n", target.Host)
	req.WriteString("Upgrade: websocket\r\nConnection: Upgrade\r\n")
	fmt.Fprintf(&req, "Sec-WebSocket-Key: %s\r\n", key)
	req.WriteString("Sec-WebSocket-Version: 13\r\n")
	if len(protocols) > 0 {
		fmt.Fprintf(&req, "Sec-WebSocket-Protocol: %s\r\n", strings.Join(protocols, ", "))
	}
	if err := headers.Write(&req); err != nil {
		conn.Close()
		return nil, "", err
	}
	req.WriteString("\r\n")
	if _, err := conn.Write([]byte(req.String())); err != nil {
		conn.Close()
		return nil, "", err
	}

	br := bufio.NewReader(conn)
	res, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, "", err
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		return nil, "", fmt.Errorf("the server answered %s rather than opening a socket", res.Status)
	}
	if !strings.EqualFold(res.Header.Get("Upgrade"), "websocket") ||
		res.Header.Get("Sec-WebSocket-Accept") != wsproto.Accept(key) {
		conn.Close()
		return nil, "", errors.New("the server's answer is not a websocket handshake")
	}
	// Join a racing cancellation before handing the connection to its next
	// owner, so this handshake watcher cannot close a healthy active socket.
	stopHandshake()
	if err := ctx.Err(); err != nil {
		conn.Close()
		return nil, "", err
	}
	// The deadline was for the handshake; what follows has no time limit of
	// its own, since a socket is meant to stay open.
	conn.SetDeadline(time.Time{})
	return wsproto.NewConn(conn, br, true, limit), res.Header.Get("Sec-WebSocket-Protocol"), nil
}

// webSocketJS is the interface a program written for a browser expects.
const webSocketJS = `(function (host) {
  "use strict";

  const CONNECTING = 0, OPEN = 1, CLOSING = 2, CLOSED = 3;
  const acceptedSocket = Symbol("accepted socket");
  const customHandshake = Symbol("custom handshake");

  class WebSocket extends EventTarget {
    constructor(url, protocols = []) {
      super();
      const accepted = url === acceptedSocket;
      this.url = accepted ? "" : String(url);
      this.readyState = CONNECTING;
      this.protocol = "";
      this.extensions = "";
      this.bufferedAmount = 0;
      this.binaryType = "blob";
      this.onopen = null;
      this.onmessage = null;
      this.onerror = null;
      this.onclose = null;
      if (!accepted) {
        this._drive(host.connect(this.url, protocols === undefined ? []
          : (Array.isArray(protocols) ? protocols.map(value => String(value)) : [String(protocols)]),
          arguments[2] === customHandshake ? requestHeaders(arguments[3]) : []));
      }
    }

    // _drive is the whole life of the socket: the handshake, then every
    // message until it ends, then the one close event.
    async _drive(handle) {
      this._handle = handle;
      let ending = null;
      try {
        const opened = await handle.opened;
        if (this.readyState !== CONNECTING) {
          throw new Error("the socket was closed before the connection opened");
        }
        this.protocol = (opened && opened.protocol) || "";
        this.readyState = OPEN;
        this._fire("open", {});
        for (;;) {
          const message = await handle.receive();
          if (message.type === "closing") {
            this.readyState = CLOSING;
            const outcome = await handle.closed;
            ending = {code: outcome.code, reason: outcome.reason, clean: outcome.clean};
            if (outcome.error) {
              this.readyState = CLOSED;
              this._fire("error", {error: outcome.error, message: String(outcome.error.message)});
            }
            break;
          }
          if (this.readyState !== OPEN) continue;
          this._fire("message", {
            data: message.type === "text" ? message.data : this._binary(message.data),
            origin: this.url,
          });
        }
      } catch (e) {
        this.readyState = CLOSED;
        this._fire("error", {error: e, message: String((e && e.message) || e)});
        ending = {code: 1006, reason: "", clean: false};
      }
      this.readyState = CLOSED;
      const end = ending || {code: 1006, reason: "", clean: false};
      this._fire("close", {code: end.code, reason: end.reason, wasClean: end.clean});
    }

    _binary(bytes) {
      if (this.binaryType === "blob" && typeof Blob !== "undefined") {
        return new Blob([bytes]);
      }
      return bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength);
    }

    // dispatchEvent calls the onwhatever property itself, so this does not.
    _fire(type, props) {
      const event = new Event(type);
      Object.assign(event, props);
      this.dispatchEvent(event);
    }

    send(data) {
      if (this.readyState === CONNECTING) {
        throw new DOMException_("the socket is not open yet");
      }
      if (this.readyState !== OPEN) return;
      // NOE's Blob stores its immutable bytes synchronously. The native queue
      // snapshots them, preserving message order alongside text/buffer sends.
      this._handle.send(typeof Blob !== "undefined" && data instanceof Blob ? data._bytes : data);
    }

    close(code, reason) {
      // WebIDL clamps an unsigned short, rounding ties to the even integer.
      let number = 1000;
      if (code !== undefined) {
        const value = +code;
        const bounded = Number.isNaN(value) ? 0 : Math.min(65535, Math.max(0, value));
        const lower = Math.floor(bounded);
        number = bounded - lower === 0.5
          ? lower + (lower % 2) : Math.round(bounded);
        if (number !== 1000 && (number < 3000 || number > 4999)) {
          throw new DOMException_("invalid WebSocket close code", "InvalidAccessError");
        }
      }
      if (typeof reason === "symbol") throw new TypeError("a close reason must be a string");
      const text = reason === undefined ? "" : String(reason).toWellFormed();
      if (new TextEncoder().encode(text).byteLength > 123) {
        throw new DOMException_("WebSocket close reason exceeds 123 UTF-8 bytes", "SyntaxError");
      }
      if (this.readyState === CLOSED || this.readyState === CLOSING) return;
      this.readyState = CLOSING;
      this._handle.close(code === undefined && reason === undefined ? 0 : number, text);
    }
  }

  // The host factory keeps handshake extensions out of the standard global
  // constructor while returning the same WebSocket type and lifecycle.
  function createWebSocket(url, options = {}) {
    if (options == null) options = {};
    if (typeof options !== "object" && typeof options !== "function") {
      throw new TypeError("WebSocket options must be an object");
    }
    return new WebSocket(url, options.protocols, customHandshake, options);
  }

  // Snapshot headers before connecting; only Go strings leave this runtime.
  function requestHeaders(options) {
    if (options == null) return [];
    if (typeof options !== "object" && typeof options !== "function") {
      throw new TypeError("WebSocket options must be an object");
    }
    const headers = options.headers;
    if (headers == null) return [];
    if (typeof headers !== "object" && typeof headers !== "function") {
      throw new TypeError("WebSocket headers must be a record or iterable of pairs");
    }
    const iterator = headers[Symbol.iterator];
    if (iterator != null) {
      if (typeof iterator !== "function") throw new TypeError("WebSocket headers must be iterable");
      return Array.from({[Symbol.iterator]() { return Reflect.apply(iterator, headers, []); }}, pair => {
        if (pair === null || (typeof pair !== "object" && typeof pair !== "function")) {
          throw new TypeError("WebSocket header entries must be pairs");
        }
        const values = Array.from(pair);
        if (values.length !== 2) throw new TypeError("WebSocket header entries must be pairs");
        return [String(values[0]), String(values[1])];
      });
    }
    return Object.entries(headers).map(([name, value]) => [name, String(value)]);
  }

  // Use the same named error where an embedder supplies only EventTarget.
  function DOMException_(message, name = "InvalidStateError") {
    if (typeof DOMException === "function") return new DOMException(message, name);
    const e = new Error(message);
    e.name = name;
    return e;
  }

  WebSocket.CONNECTING = CONNECTING;
  WebSocket.OPEN = OPEN;
  WebSocket.CLOSING = CLOSING;
  WebSocket.CLOSED = CLOSED;
  for (const [name, value] of Object.entries(
      {CONNECTING, OPEN, CLOSING, CLOSED})) {
    Object.defineProperty(WebSocket.prototype, name, {value, writable: false});
  }

  // upgradeWebSocket takes a request over: the answer tells the server to stop
  // treating the connection as a request and hand it to the socket.
  function upgradeWebSocket(request, options = {}) {
    if (!host.serving) {
      throw new TypeError("this runtime is not serving, so there is nothing to upgrade");
    }
    const headers = request && request.headers;
    const wanted = headers && headers.get("upgrade");
    if (!wanted || String(wanted).toLowerCase() !== "websocket") {
      throw new TypeError("this request did not ask for a socket");
    }
    const reserved = host.reserve();
    const socket = new WebSocket(acceptedSocket);
    socket._drive(reserved.handle);

    const response = new Response(null, {status: 101});
    // The token is what the connection is handed over with; it is on the
    // response because the response is what the handler gives back.
    Object.defineProperty(response, "upgrade", {value: reserved.token});
    if (options.protocol) {
      Object.defineProperty(response, "protocol", {value: String(options.protocol)});
    }
    return {socket, response};
  }

  return {WebSocket, createWebSocket, upgradeWebSocket};
})`
