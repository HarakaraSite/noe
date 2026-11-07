package nativeio

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func testUDPConn(t *testing.T) *net.UDPConn {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func onlyPacketOperation(t *testing.T, h *Host) *packetOperation {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for resource := range h.resources {
		if operation, ok := resource.(*packetOperation); ok {
			return operation
		}
	}
	t.Fatal("native packet operation was not owned before publication")
	return nil
}

func packetPromise(t *testing.T, operation quickjs.Value) quickjs.Value {
	t.Helper()
	promise, err := operation.Get("promise")
	if err != nil {
		t.Fatal(err)
	}
	return promise
}

func TestUDPPendingBindCloseReclaimsResultWithoutOwnerDelivery(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	conn := testUDPConn(t)
	address := conn.LocalAddr().(*net.UDPAddr)
	_ = h.bindPacket(`{"network":"udp","host":"127.0.0.1","port":0}`, func(context.Context, string, string) (net.PacketConn, error) {
		return conn, nil
	})
	op := onlyPacketOperation(t, h)
	select {
	case <-op.done:
	case <-time.After(time.Second):
		t.Fatal("bind did not finish its native result")
	}
	// The owner has not driven the queued result. Closing native bindings
	// must reclaim it before returning, without closing the borrowed VM.
	h.Close()
	if err := conn.SetReadDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("undelivered UDP result remained open: %v", err)
	}
	replacement, err := net.ListenUDP("udp", address)
	if err != nil {
		t.Fatal(err)
	}
	_ = replacement.Close()
	if value := evalNet(t, rt, `21*2`); value.Int() != 42 || rt.Context().Err() != nil {
		t.Fatal("UDP bind cleanup ended the borrowed runtime")
	}
}

func TestUDPCanceledBindClosesLateNativeResult(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	conn := testUDPConn(t)
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	operation := h.bindPacket(`{"network":"udp","host":"127.0.0.1","port":0}`, func(context.Context, string, string) (net.PacketConn, error) {
		close(started)
		<-release // Simulate a bind completing after its cancellation.
		return conn, nil
	})
	op := onlyPacketOperation(t, h)
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("native bind did not start")
	}
	op.cancelPending()
	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err := packetPromise(t, operation).Await(ctx)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("late canceled bind did not reject promptly: %v", err)
	}
	if err := conn.SetReadDeadline(time.Now()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("canceled bind leaked its late socket: %v", err)
	}
	if rt.Busy() {
		t.Fatal("late bind result retained native work")
	}
}

type blockedUDPWriter struct {
	*net.UDPConn
	mu          sync.Mutex
	first       bool
	deadline    time.Time
	entered     chan struct{}
	interrupted chan struct{}
	once        sync.Once
}

func (c *blockedUDPWriter) WriteToUDP(data []byte, address *net.UDPAddr) (int, error) {
	c.mu.Lock()
	first := c.first
	c.first = false
	c.mu.Unlock()
	if first {
		close(c.entered)
		<-c.interrupted
		return 0, os.ErrDeadlineExceeded
	}
	return c.UDPConn.WriteToUDP(data, address)
}

func (c *blockedUDPWriter) SetWriteDeadline(deadline time.Time) error {
	c.mu.Lock()
	c.deadline = deadline
	c.mu.Unlock()
	if !deadline.IsZero() && !deadline.After(time.Now()) {
		c.once.Do(func() { close(c.interrupted) })
	}
	return c.UDPConn.SetWriteDeadline(deadline)
}

func TestUDPWriteCancellationClearsDeadlineBeforeNextSend(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	defer h.Close()
	receiver := testUDPConn(t)
	writer := &blockedUDPWriter{UDPConn: testUDPConn(t), first: true, entered: make(chan struct{}), interrupted: make(chan struct{})}
	socket, err := newPacketSocket(h, writer)
	if err != nil {
		t.Fatal(err)
	}
	target := receiver.LocalAddr().(*net.UDPAddr)
	payload := `{"network":"udp","host":"127.0.0.1","port":` + strconv.Itoa(target.Port) + `}`
	operation := socket.send(rt.NewBytes([]byte{1}), payload)
	op := onlyPacketOperation(t, h)
	select {
	case <-writer.entered:
	case <-time.After(time.Second):
		t.Fatal("native UDP write did not block")
	}
	op.cancelPending()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	_, err = packetPromise(t, operation).Await(ctx)
	if err == nil || errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "canceled") {
		t.Fatalf("blocked write cancellation did not reject promptly: %v", err)
	}
	writer.mu.Lock()
	cleared := writer.deadline.IsZero()
	writer.mu.Unlock()
	if !cleared {
		t.Fatal("UDP cancellation left its write deadline installed")
	}
	sent := awaitNet(t, packetPromise(t, socket.send(rt.NewBytes([]byte{2}), payload)))
	if sent.Int() != 1 {
		t.Fatalf("second send byte count=%d", sent.Int())
	}
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4)
	n, _, err := receiver.ReadFromUDP(buf)
	if err != nil || n != 1 || buf[0] != 2 {
		t.Fatalf("second send after cancellation: n=%d bytes=%v error=%v", n, buf[:n], err)
	}
	if err := socket.Close(); err != nil {
		t.Fatal(err)
	}
}

type fullUDPReader struct{ *net.UDPConn }

func (c *fullUDPReader) ReadFromUDP(buffer []byte) (int, *net.UDPAddr, error) {
	return len(buffer), &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}, nil
}

func TestUDPReceiveRejectsPossibleTruncationWithoutPublishingPrefix(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	socket, err := newPacketSocket(h, &fullUDPReader{testUDPConn(t)})
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	operation := socket.receive()
	promise := packetPromise(t, operation)
	if err := rt.Set("packetResult", promise); err != nil {
		t.Fatal(err)
	}
	result := awaitNet(t, evalNet(t, rt, `packetResult.then(()=>"partial payload exposed",error=>error.name+":"+error.code)`))
	if result.String() != "RangeError:ERR_UDP_MESSAGE_TOO_LARGE" {
		t.Fatalf("full packet buffer result: %s", result.String())
	}
}
