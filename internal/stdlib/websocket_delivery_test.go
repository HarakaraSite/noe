package stdlib

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib/internal/wsproto"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// A successful handshake may already be queued when the owner closes the
// socket or its execution. Neither path may attach that transport afterwards.
func TestWebSocketQueuedOpeningAfterClose(t *testing.T) {
	for _, closeScope := range []bool{false, true} {
		t.Run(fmt.Sprintf("scope-close=%v", closeScope), func(t *testing.T) {
			peerClosed := make(chan struct{})
			deliverHandshake := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, rw, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer conn.Close()
				defer close(peerClosed)
				<-deliverHandshake
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n\r\n", wsproto.Accept(r.Header.Get("Sec-WebSocket-Key")))
				rw.Write([]byte{0x81, 2, 'o', 'k', 0x88, 2, 3, 232})
				if err := rw.Flush(); err != nil {
					t.Error(err)
					return
				}
				var b [1]byte
				_, _ = conn.Read(b[:])
			}))
			defer server.Close()
			rt := quickjs.New()
			loop := NewLoop(rt)
			scope, err := NewExecutionScope(loop, t.Context())
			if err != nil {
				rt.Close()
				t.Fatal(err)
			}
			defer func() { scope.Close(); loop.Close(); rt.Close() }()
			if err := Install(rt, Config{Loop: loop, Sockets: &WebSockets{
				Allow: func(*url.URL) error { return nil }, HandshakeTimeout: time.Second,
			}}); err != nil {
				t.Fatal(err)
			}
			_, err = rt.Eval(fmt.Sprintf(`
				globalThis.events = [];
				globalThis.socket = new WebSocket(%q);
				for (const name of ["open", "message", "error", "close"])
				  socket.addEventListener(name, () => events.push(name));
			`, "ws"+strings.TrimPrefix(server.URL, "http")))
			// Eval drains ready host jobs before returning. Keep the handshake
			// pending until then so it cannot attach and start socket workers.
			close(deliverHandshake)
			if err != nil {
				t.Fatal(err)
			}
			// Only the opening goroutine is admitted so far. Waiting for it
			// queues its successful result without driving the JS owner.
			openingDone := make(chan struct{})
			go func() { scope.workers.Wait(); close(openingDone) }()
			select {
			case <-openingDone:
			case <-time.After(2 * time.Second):
				t.Fatal("opening did not finish while owner delivery was paused")
			}
			if closeScope {
				scope.Close()
			} else if _, err := rt.Eval(`socket.close()`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if err := loop.Run(ctx); err != nil {
				t.Fatal(err)
			}
			value, err := rt.Eval(`events.join(",")`)
			if err != nil {
				t.Fatal(err)
			}
			want := "error,close"
			if closeScope {
				want = ""
			}
			if value.String() != want {
				t.Fatalf("events=%q, want %q", value.String(), want)
			}
			select {
			case <-peerClosed:
			case <-time.After(time.Second):
				t.Fatal("queued connection was not closed")
			}
			if rt.Context().Err() != nil || loop.Context().Err() != nil {
				t.Fatal("execution cleanup closed the borrowed owner")
			}
		})
	}
}
