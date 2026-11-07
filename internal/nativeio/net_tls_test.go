package nativeio

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func nativeTLSIdentity(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	fixture := httptest.NewTLSServer(http.NotFoundHandler())
	pair := fixture.TLS.Certificates[0]
	fixture.Close()
	certificate, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return pair, roots
}

func nativeTLSListener(t *testing.T, h *Host, config *tls.Config) *netListener {
	t.Helper()
	transport, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := newBoundNetListener(transport, "tls", "")
	if err != nil {
		t.Fatal(err)
	}
	bound.tlsConfig = config
	listener, err := newNetListener(h, bound)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	return listener
}

func TestNetTLSListenerCloseReclaimsAuthenticatedResultBeforeOwnerDelivery(t *testing.T) {
	pair, roots := nativeTLSIdentity(t)
	rt, h := netRuntime(t, t.TempDir(), nil)
	listener := nativeTLSListener(t, h, &tls.Config{Certificates: []tls.Certificate{pair}})
	pending := listener.accept()
	client, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", listener.bound.Addr().String(), &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer client.NetConn().Close()
	// No owner evaluation runs between Accept and Close. The authenticated
	// result is queued, while ownership must remain with the listener.
	ready := make(chan struct{})
	go func() { listener.accepts.Wait(); close(ready) }()
	select {
	case <-ready:
	case <-time.After(time.Second):
		t.Fatal("authenticated Accept did not queue its result")
	}
	listener.mu.Lock()
	owned := len(listener.pending)
	listener.mu.Unlock()
	if owned != 1 {
		t.Fatalf("undelivered TLS ownership=%d, want 1", owned)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := client.Read(make([]byte, 1)); err == nil {
		t.Fatal("queued TLS connection remained open after listener close")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("queued TLS connection was not reclaimed")
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := pending.Value().Await(ctx); err == nil {
		t.Fatal("queued Accept resolved after listener close")
	}
	if value := evalNet(t, rt, `21*2`); value.Int() != 42 || rt.Context().Err() != nil {
		t.Fatal("listener close ended the borrowed runtime")
	}
}

func TestNetTLSListenerCloseCancelsCertificateCallbackContext(t *testing.T) {
	_, h := netRuntime(t, t.TempDir(), nil)
	entered := make(chan struct{})
	listener := nativeTLSListener(t, h, &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		close(entered)
		<-hello.Context().Done()
		return nil, hello.Context().Err()
	}})
	listener.accept()
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		conn, _ := tls.DialWithDialer(&net.Dialer{Timeout: 2 * time.Second}, "tcp", listener.bound.Addr().String(), &tls.Config{ServerName: "localhost"})
		if conn != nil {
			_ = conn.NetConn().Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("server did not reach its certificate callback")
	}
	closed := make(chan struct{})
	go func() { _ = listener.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("listener close waited for the handshake timeout in a context-aware callback")
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("TLS client survived listener close")
	}
}

type tlsWriteBarrier struct {
	net.Conn
	armed   atomic.Bool
	entered chan struct{}
	once    sync.Once
}

func (c *tlsWriteBarrier) Write(p []byte) (int, error) {
	if c.armed.Load() {
		c.once.Do(func() { close(c.entered) })
	}
	return c.Conn.Write(p)
}

func TestNetTLSHostCloseInterruptsBlockedWriteWithoutAlertWait(t *testing.T) {
	pair, roots := nativeTLSIdentity(t)
	rt, h := netRuntime(t, t.TempDir(), nil)
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	barrier := &tlsWriteBarrier{Conn: left, entered: make(chan struct{})}
	client := tls.Client(barrier, &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"})
	server := tls.Server(right, &tls.Config{Certificates: []tls.Certificate{pair}, SessionTicketsDisabled: true})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.HandshakeContext(ctx) }()
	if err := client.HandshakeContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	socket, err := newNetConnection(h, &tlsSocketConn{Conn: client})
	if err != nil {
		t.Fatal(err)
	}
	barrier.armed.Store(true)
	if err := socket.beginIO(false); err != nil {
		t.Fatal(err)
	}
	written := make(chan error, 1)
	go func() {
		defer socket.io.Done()
		defer socket.writeIO.Done()
		_, err := socket.conn.Write(make([]byte, 64<<10))
		written <- err
	}()
	select {
	case <-barrier.entered:
	case <-time.After(time.Second):
		t.Fatal("TLS application write did not reach the unread transport")
	}
	closed := make(chan struct{})
	go func() { h.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Host.Close waited for TLS close_notify behind a blocked write")
	}
	if err := <-written; err == nil {
		t.Fatal("blocked write succeeded on the unread pipe")
	}
	if _, err := right.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("TLS transport remained open: %v", err)
	}
	if value := evalNet(t, rt, `21*2`); value.Int() != 42 || rt.Context().Err() != nil {
		t.Fatal("host cleanup ended the borrowed runtime")
	}
}

func TestNetTLSReadCancellationDoesNotWaitBehindControlWrite(t *testing.T) {
	pair, roots := nativeTLSIdentity(t)
	_, h := netRuntime(t, t.TempDir(), nil)
	defer h.Close()
	left, right := net.Pipe()
	defer left.Close()
	defer right.Close()
	barrier := &tlsWriteBarrier{Conn: left, entered: make(chan struct{})}
	client := tls.Client(barrier, &tls.Config{RootCAs: roots, ServerName: "127.0.0.1"})
	server := tls.Server(right, &tls.Config{Certificates: []tls.Certificate{pair}, SessionTicketsDisabled: true})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.HandshakeContext(ctx) }()
	if err := client.HandshakeContext(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
	socket, err := newNetConnection(h, &tlsSocketConn{Conn: client})
	if err != nil {
		t.Fatal(err)
	}
	barrier.armed.Store(true)
	if err := socket.beginIO(false); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer socket.io.Done()
		defer socket.writeIO.Done()
		_, _ = socket.conn.Write([]byte("blocked application bytes"))
	}()
	select {
	case <-barrier.entered:
	case <-ctx.Done():
		t.Fatal("application writer did not reach the unread pipe")
	}
	_ = socket.read()
	// The invalid record version makes TLS Read send a protocol alert. That
	// requires the same output mutex held by the blocked application writer.
	_ = right.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := right.Write([]byte{23, 3, 1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	readCancel, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if _, err := socket.cancelRead().Value().Await(readCancel); err != nil {
		t.Fatalf("read cancellation waited for the unrelated TLS writer: %v", err)
	}
	// Full close must still join the native reader and writer retained by the
	// connection, even though local read cancellation has already completed.
	if err := socket.Close(); err != nil {
		t.Fatal(err)
	}
}
