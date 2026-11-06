package wsproto

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// pair returns two ends of a connection speaking the protocol at each other,
// the first masking as a client does.
func pair(t *testing.T) (*Conn, *Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return NewConn(a, nil, true, 1<<20), NewConn(b, nil, false, 1<<20)
}

func TestWSMessages(t *testing.T) {
	client, server := pair(t)

	for _, tc := range []struct {
		name string
		op   byte
		data []byte
	}{
		{"text", OpText, []byte("hello")},
		{"empty", OpText, nil},
		// The three lengths a frame header has: one byte, two, and eight.
		{"125 bytes", OpBinary, bytes.Repeat([]byte("x"), 125)},
		{"126 bytes", OpBinary, bytes.Repeat([]byte("y"), 126)},
		{"70k", OpBinary, bytes.Repeat([]byte("z"), 70000)},
		{"not ascii", OpText, []byte("héllo — 世界")},
	} {
		go func() {
			if err := client.WriteMessage(tc.op, tc.data); err != nil {
				t.Errorf("%s: write: %v", tc.name, err)
			}
		}()
		op, got, err := server.ReadMessage()
		if err != nil {
			t.Fatalf("%s: read: %v", tc.name, err)
		}
		if op != tc.op || !bytes.Equal(got, tc.data) {
			t.Errorf("%s: got op %d, %d bytes; want op %d, %d bytes",
				tc.name, op, len(got), tc.op, len(tc.data))
		}
	}
}

// What a client sends is masked and what a server sends is not, which is what
// the standard asks of each end.
func TestWSMasking(t *testing.T) {
	client, server := pair(t)

	go client.WriteMessage(OpText, []byte("from the client"))
	fin, op, data, err := server.readFrame()
	if err != nil || !fin || op != OpText || string(data) != "from the client" {
		t.Fatalf("frame = %v %d %q, %v", fin, op, data, err)
	}

	// Reading the raw bytes the other way shows the difference: a header with
	// the mask bit clear, and the payload in the clear behind it.
	const said = "from the server"
	var seen []byte
	done := make(chan struct{})
	go func() {
		defer close(done)
		head := make([]byte, 2)
		if _, err := io.ReadFull(client.conn, head); err != nil {
			return
		}
		body := make([]byte, len(said))
		if _, err := io.ReadFull(client.conn, body); err != nil {
			return
		}
		seen = append(head, body...)
	}()
	server.WriteMessage(OpText, []byte(said))
	<-done
	if len(seen) < 2 {
		t.Fatal("nothing arrived")
	}
	if seen[1]&0x80 != 0 {
		t.Error("the server masked a frame, which a server may not do")
	}
	if !strings.Contains(string(seen), said) {
		t.Errorf("the payload is not there in the clear: %q", seen)
	}
}

// A message that arrives in pieces is one message, and a ping in the middle of
// it is answered without disturbing it.
func TestWSFragmentsAndPings(t *testing.T) {
	client, server := pair(t)

	go func() {
		client.writeFrameFor(t, false, OpText, []byte("one "))
		client.writeFrameFor(t, true, OpPing, []byte("are you there"))
		client.writeFrameFor(t, false, OpContinuation, []byte("two "))
		client.writeFrameFor(t, true, OpContinuation, []byte("three"))
	}()

	// The server answers the ping while it is reading, so the pong is read
	// here before the message can be returned.
	go func() {
		for {
			fin, op, data, err := client.readFrame()
			if err != nil {
				return
			}
			if fin && op == OpPong && string(data) == "are you there" {
				return
			}
		}
	}()

	op, got, err := server.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if op != OpText || string(got) != "one two three" {
		t.Errorf("message = %d %q", op, got)
	}
}

// A close frame carries a code and a reason, which is how a finished
// conversation is told from a dropped connection.
func TestWSClose(t *testing.T) {
	client, server := pair(t)
	go client.SendClose(4000, "that is all")
	op, payload, err := server.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if op != OpClose {
		t.Fatalf("op = %d", op)
	}
	if code, reason := CloseInfo(payload); code != 4000 || reason != "that is all" {
		t.Errorf("close = %d %q", code, reason)
	}
	// A close frame with nothing in it means nobody said why.
	if code, reason := CloseInfo(nil); code != 1005 || reason != "" {
		t.Errorf("empty close = %d %q", code, reason)
	}
}

func TestWSCloseInterruptsBlockedWrite(t *testing.T) {
	raw, peer := net.Pipe()
	wire := newCloseWriteBarrierConn(raw, false)
	c := NewConn(wire, nil, true, 1<<20)
	t.Cleanup(func() {
		wire.releaseClose()
		_ = peer.Close()
		c.Close()
	})

	writeDone := make(chan error, 1)
	go func() {
		writeDone <- c.WriteMessage(OpBinary, bytes.Repeat([]byte("x"), 1024))
	}()
	select {
	case <-wire.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("the test write did not reach the blocked transport")
	}

	closeDone := make(chan struct{})
	go func() {
		c.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
	case <-time.After(time.Second):
		// The old implementation waits for the writer's mutex. Closing the
		// peer releases that writer so the test can fail without leaking it.
		_ = peer.Close()
		select {
		case <-writeDone:
		case <-time.After(time.Second):
			t.Fatal("the blocked writer did not stop after peer cleanup")
		}
		select {
		case <-closeDone:
		case <-time.After(time.Second):
			t.Fatal("Close did not finish after peer cleanup")
		}
		t.Fatal("Close waited for the write mutex instead of interrupting the blocked write")
	}

	select {
	case err := <-writeDone:
		if err == nil {
			t.Fatal("the interrupted write unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("Close returned without interrupting the blocked write")
	}
	if err := c.WriteMessage(OpText, []byte("after close")); err == nil {
		t.Fatal("WriteMessage succeeded after Close")
	}
}

func TestWSCloseConcurrentCallsWaitForOneNativeClose(t *testing.T) {
	raw, peer := net.Pipe()
	wire := newCloseWriteBarrierConn(raw, true)
	c := NewConn(wire, nil, true, 1<<20)
	t.Cleanup(func() {
		wire.releaseClose()
		_ = peer.Close()
		c.Close()
	})

	const callers = 16
	start := make(chan struct{})
	results := make(chan struct{}, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			c.Close()
			results <- struct{}{}
		}()
	}
	ready.Wait()
	close(start)
	select {
	case <-wire.closeStarted:
	case <-time.After(time.Second):
		t.Fatal("no Close call reached the underlying connection")
	}

	select {
	case <-results:
		t.Fatal("Close returned before the underlying Close completed")
	case <-time.After(25 * time.Millisecond):
	}

	wire.releaseClose()
	for range callers {
		select {
		case <-results:
		case <-time.After(time.Second):
			t.Fatal("a concurrent Close call did not complete")
		}
	}
	if got := wire.closeCalls.Load(); got != 1 {
		t.Errorf("underlying Close calls = %d, want 1", got)
	}
}

func TestWSParseClose(t *testing.T) {
	closePayload := func(code uint16, reason []byte) []byte {
		payload := make([]byte, 2, 2+len(reason))
		binary.BigEndian.PutUint16(payload, code)
		return append(payload, reason...)
	}
	tests := []struct {
		name       string
		payload    []byte
		wantCode   int
		wantReason string
		wantErr    bool
	}{
		{name: "empty", payload: nil, wantCode: 1005},
		{name: "one byte", payload: []byte{0x03}, wantErr: true},
		{name: "over control limit", payload: bytes.Repeat([]byte{0}, 126), wantErr: true},
		{name: "invalid utf8 reason", payload: closePayload(1000, []byte{0xff}), wantErr: true},
		{name: "code below wire range", payload: closePayload(999, nil), wantErr: true},
		{name: "code above wire range", payload: closePayload(5000, nil), wantErr: true},
		{name: "reserved 1004", payload: closePayload(1004, nil), wantErr: true},
		{name: "reserved 1005", payload: closePayload(1005, nil), wantErr: true},
		{name: "reserved 1006", payload: closePayload(1006, nil), wantErr: true},
		{name: "reserved 1015", payload: closePayload(1015, nil), wantErr: true},
		{name: "registered code", payload: closePayload(1000, []byte("done")), wantCode: 1000, wantReason: "done"},
		{name: "application code", payload: closePayload(4000, []byte("custom")), wantCode: 4000, wantReason: "custom"},
		{name: "upper in-range code", payload: closePayload(4999, nil), wantCode: 4999},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, reason, err := ParseClose(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseClose error = %v, wantErr %t", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if code != tt.wantCode || reason != tt.wantReason {
				t.Errorf("ParseClose = (%d, %q), want (%d, %q)", code, reason, tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestWSRejectsMalformedControlFrames(t *testing.T) {
	for _, tt := range []struct {
		name  string
		heads []byte
	}{
		{name: "fragmented close", heads: []byte{OpClose, 0}},
		{name: "oversized ping", heads: []byte{0x80 | OpPing, 126, 0, 126}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			local, peer := net.Pipe()
			defer local.Close()
			defer peer.Close()
			c := NewConn(local, nil, false, 1<<20)
			writeDone := make(chan error, 1)
			go func() {
				_, err := peer.Write(tt.heads)
				writeDone <- err
			}()

			if _, _, _, err := c.readFrame(); err == nil {
				t.Fatal("readFrame accepted a malformed control frame")
			}
			select {
			case err := <-writeDone:
				if err != nil {
					t.Errorf("writing control header: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("control header writer did not complete")
			}
		})
	}
}

// A peer that announces more than the limit is refused rather than believed.
func TestWSLimit(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	client := NewConn(a, nil, true, 8)
	server := NewConn(b, nil, false, 8)

	go client.WriteMessage(OpBinary, bytes.Repeat([]byte("x"), 64))
	if _, _, err := server.ReadMessage(); err == nil {
		t.Error("a message over the limit was read")
	}
}

// The handshake answer is the hash the standard specifies, which is how each
// end knows the other understood the request.
func TestWSAccept(t *testing.T) {
	// The example from RFC 6455.
	if got := Accept("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Errorf("accept = %q", got)
	}
	key, err := Key()
	if err != nil || len(key) != 24 {
		t.Errorf("key = %q, %v", key, err)
	}
	if again, _ := Key(); again == key {
		t.Error("two keys came out the same")
	}
}

// writeFrameFor writes one frame of a message, for the tests that need the
// pieces rather than the whole.
func (c *Conn) writeFrameFor(t *testing.T, fin bool, op byte, data []byte) {
	t.Helper()
	head := []byte{op, 0x80 | byte(len(data))}
	if fin {
		head[0] |= 0x80
	}
	key := []byte{1, 2, 3, 4}
	body := make([]byte, len(data))
	for i := range data {
		body[i] = data[i] ^ key[i%4]
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.conn.Write(append(append(head, key...), body...)); err != nil {
		t.Errorf("write: %v", err)
	}
}

type closeWriteBarrierConn struct {
	net.Conn
	writeStarted     chan struct{}
	closeStarted     chan struct{}
	closeGate        chan struct{}
	writeStartedOnce sync.Once
	closeStartedOnce sync.Once
	closeGateOnce    sync.Once
	closeCalls       atomic.Int32
}

func newCloseWriteBarrierConn(conn net.Conn, blockClose bool) *closeWriteBarrierConn {
	c := &closeWriteBarrierConn{
		Conn:         conn,
		writeStarted: make(chan struct{}),
		closeStarted: make(chan struct{}),
		closeGate:    make(chan struct{}),
	}
	if !blockClose {
		c.releaseClose()
	}
	return c
}

func (c *closeWriteBarrierConn) Write(p []byte) (int, error) {
	c.writeStartedOnce.Do(func() { close(c.writeStarted) })
	return c.Conn.Write(p)
}

func (c *closeWriteBarrierConn) Close() error {
	c.closeCalls.Add(1)
	c.closeStartedOnce.Do(func() { close(c.closeStarted) })
	<-c.closeGate
	return c.Conn.Close()
}

func (c *closeWriteBarrierConn) releaseClose() {
	c.closeGateOnce.Do(func() { close(c.closeGate) })
}
