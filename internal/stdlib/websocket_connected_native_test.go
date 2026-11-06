package stdlib

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib/internal/wsproto"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type websocketReadBarrier struct {
	net.Conn
	read     chan struct{}
	once     sync.Once
	hold     chan struct{}
	held     chan struct{}
	heldOnce sync.Once
	seen     int
}

func (c *websocketReadBarrier) Read(p []byte) (int, error) {
	c.once.Do(func() { close(c.read) })
	n, err := c.Conn.Read(p)
	c.seen += n
	if c.hold != nil && c.seen >= 8 {
		c.heldOnce.Do(func() { close(c.held); <-c.hold })
	}
	return n, err
}

// Native close progress must not depend on the owner running another JS job,
// including when an ordinary message is queued before the peer's close reply.
func TestWebSocketClosingProgressWithoutOwnerDelivery(t *testing.T) {
	for _, tc := range []struct{ ordinaryRead, readFailure bool }{{}, {ordinaryRead: true}, {readFailure: true}} {
		t.Run(fmt.Sprintf("ordinary-read=%v/read-failure=%v", tc.ordinaryRead, tc.readFailure), func(t *testing.T) {
			rt := quickjs.New()
			loop := NewLoop(rt)
			scope, err := NewExecutionScope(loop, t.Context())
			if err != nil {
				rt.Close()
				t.Fatal(err)
			}
			defer func() { scope.Close(); loop.Close(); rt.Close() }()
			host := &wsHost{rt: rt, cfg: &WebSockets{Loop: loop}}
			socket, err := host.newSocket()
			if err != nil {
				t.Fatal(err)
			}
			a, b := net.Pipe()
			defer b.Close()
			observed := &websocketReadBarrier{Conn: a, read: make(chan struct{})}
			var releaseRead func()
			if tc.readFailure {
				observed.hold, observed.held = make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				releaseRead = func() { releaseOnce.Do(func() { close(observed.hold) }) }
				defer releaseRead()
			}
			if err := socket.start(wsproto.NewConn(observed, nil, true, 1024)); err != nil {
				t.Fatal(err)
			}
			peer := wsproto.NewConn(b, nil, false, 1024)
			peerDone := make(chan error, 1)
			go func() {
				op, payload, err := peer.ReadMessage()
				if err == nil && op != wsproto.OpClose {
					err = fmt.Errorf("peer received opcode %d", op)
				}
				if err == nil && !tc.readFailure {
					err = peer.WriteMessage(wsproto.OpText, []byte("discard while closing"))
				}
				if err == nil {
					err = peer.WriteMessage(wsproto.OpClose, payload)
				}
				peerDone <- err
			}()
			if tc.ordinaryRead {
				socket.receive()
				select {
				case <-observed.read:
				case <-time.After(time.Second):
					t.Fatal("ordinary read did not begin")
				}
			}
			code, _ := rt.Eval(`4001`)
			reason, _ := rt.Eval(`"done"`)
			socket.closeWith(code, reason)
			if tc.readFailure {
				select {
				case <-observed.held:
				case <-time.After(time.Second):
					t.Fatal("peer close bytes did not reach the in-flight native read")
				}
				// The bytes have arrived but the drain has not parsed them yet.
				// A failure callback must wait for this first peer result.
				socket.failClose(errors.New("injected closing failure"))
				if socket.settled {
					t.Fatal("failure discarded an in-flight closing read")
				}
				releaseRead()
			}
			// No RunJobs or Loop.Run is allowed before this barrier: it models
			// a synchronous JS listener that stays busy after close().
			select {
			case <-socket.handshakeDone:
			case <-time.After(time.Second):
				t.Fatal("closing exchange waited for owner delivery")
			}
			if err := <-peerDone; err != nil {
				t.Fatal(err)
			}
			if err := rt.Set("closed", socket.closing.Value()); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`globalThis.result = "pending"; closed.then(v => { result = v.code + "/" + v.reason + "/" + v.clean });`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := loop.Run(ctx); err != nil {
				t.Fatal(err)
			}
			result, err := rt.Eval(`result`)
			want := "4001/done/true"
			if tc.readFailure {
				want = "4001/done/false"
			}
			if err != nil || result.String() != want {
				t.Fatalf("outcome=%s error=%v", result.String(), err)
			}
		})
	}
}
