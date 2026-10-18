//go:build lowlevel2_spike

package netspike

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

const socketChildEnv = "NOE_LOWLEVEL2_SOCKET_CHILD"

type directionalConn interface {
	net.Conn
	CloseRead() error
	CloseWrite() error
}

func asDirectional(t *testing.T, conn net.Conn) directionalConn {
	t.Helper()
	dc, ok := conn.(directionalConn)
	if !ok {
		t.Fatalf("%T does not expose directional close", conn)
	}
	return dc
}

func closeWrite(conn net.Conn) error {
	c, ok := conn.(interface{ CloseWrite() error })
	if !ok {
		return fmt.Errorf("%T does not expose CloseWrite", conn)
	}
	return c.CloseWrite()
}

func setSmallWriteBuffer(t *testing.T, conn net.Conn) {
	t.Helper()
	setter, ok := conn.(interface{ SetWriteBuffer(int) error })
	if !ok {
		t.Fatalf("%T does not expose SetWriteBuffer", conn)
	}
	if err := setter.SetWriteBuffer(4096); err != nil {
		t.Fatalf("SetWriteBuffer: %v", err)
	}
}

func listen(t *testing.T, network string) net.Listener {
	t.Helper()
	if network == "unix" {
		return listenUnix(t)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func listenUnix(t *testing.T) net.Listener {
	t.Helper()
	path := filepath.Join(t.TempDir(), "socket.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func connectedPair(t *testing.T, network string) (directionalConn, directionalConn) {
	t.Helper()
	ln := listen(t, network)
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	client, err := net.Dial(network, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	select {
	case server := <-accepted:
		t.Cleanup(func() { _ = server.Close() })
		return asDirectional(t, client), asDirectional(t, server)
	case err := <-acceptErr:
		t.Fatalf("accept: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("accept timed out")
	}
	return nil, nil
}

// listenerResource models host ownership: opening a listener keeps QuickJS
// alive until the Go resource is closed. It does not need a JS timer or a JS
// callback to release that work.
type listenerResource struct {
	listener net.Listener
	work     *quickjs.AsyncWork
	done     chan struct{}
	once     sync.Once
	closeErr error
}

func ownListener(rt *quickjs.Runtime, listener net.Listener) *listenerResource {
	h := &listenerResource{listener: listener, work: rt.StartAsyncWork(), done: make(chan struct{})}
	rt.OnClose(func() { _ = h.Close() })
	return h
}

func (h *listenerResource) Close() error {
	h.once.Do(func() {
		close(h.done)
		h.closeErr = h.listener.Close()
		h.work.Done()
	})
	return h.closeErr
}

func (h *listenerResource) Accept(rt *quickjs.Runtime) quickjs.Value {
	return rt.Go(func(context.Context) (any, error) {
		conn, err := h.listener.Accept()
		if err != nil {
			return nil, err
		}
		_ = conn.Close()
		return nil, nil
	})
}

type socketBridge struct {
	rt            *quickjs.Runtime
	conn          directionalConn
	readStarted   chan struct{}
	readDone      chan struct{}
	readErr       chan error
	writeStarted  chan struct{}
	writeDone     chan struct{}
	abortStarted  chan struct{}
	readOnce      sync.Once
	readDoneOnce  sync.Once
	writeDoneOnce sync.Once
	writeOnce     sync.Once
	abortOnce     sync.Once
}

func (b *socketBridge) object() (quickjs.Value, error) {
	b.rt.OnClose(func() { _ = b.conn.Close() })
	o := b.rt.NewObject()
	set := func(name string, value any) error { return o.Set(name, value) }
	if err := errors.Join(
		set("read", func() quickjs.Value {
			return b.rt.Go(func(context.Context) (any, error) {
				if b.readDone != nil {
					defer b.readDoneOnce.Do(func() { close(b.readDone) })
				}
				buf := make([]byte, 64<<10)
				if b.readStarted != nil {
					b.readOnce.Do(func() { close(b.readStarted) })
				}
				n, err := b.conn.Read(buf)
				if b.readErr != nil {
					b.readErr <- err
				}
				if n > 0 {
					return append([]byte(nil), buf[:n]...), nil
				}
				if errors.Is(err, io.EOF) {
					return nil, nil
				}
				return nil, err
			})
		}),
		set("write", func(chunk quickjs.Value) quickjs.Value {
			buf, ok := chunk.Bytes()
			if !ok {
				return b.rt.Go(func(context.Context) (any, error) { return nil, errors.New("write requires Uint8Array") })
			}
			copyOfChunk := append([]byte(nil), buf...)
			return b.rt.Go(func(context.Context) (any, error) {
				if b.writeDone != nil {
					defer b.writeDoneOnce.Do(func() { close(b.writeDone) })
				}
				for len(copyOfChunk) != 0 {
					if b.writeStarted != nil {
						b.writeOnce.Do(func() { close(b.writeStarted) })
					}
					n, err := b.conn.Write(copyOfChunk)
					copyOfChunk = copyOfChunk[n:]
					if err != nil {
						return nil, err
					}
					if n == 0 {
						return nil, io.ErrShortWrite
					}
				}
				return nil, nil
			})
		}),
		set("cancelRead", func() error { return b.conn.CloseRead() }),
		set("closeWrite", func() error { return b.conn.CloseWrite() }),
		set("abortWrite", func() error {
			if b.abortStarted != nil {
				b.abortOnce.Do(func() { close(b.abortStarted) })
			}
			// A write deadline interrupts an in-flight Write; CloseWrite then
			// exposes EOF to the peer while preserving this connection's reader.
			return errors.Join(b.conn.SetWriteDeadline(time.Now()), b.conn.CloseWrite())
		}),
		set("fullClose", func() error { return b.conn.Close() }),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

const streamsFactory = `((socket) => {
  const readable = new ReadableStream({
    async pull(controller) {
      const chunk = await socket.read();
      if (chunk === null) controller.close();
      else controller.enqueue(new Uint8Array(chunk));
    },
    cancel() { return socket.cancelRead(); },
  }, {highWaterMark: 0});
  const writable = new WritableStream({
    write(chunk) {
      if (!(chunk instanceof Uint8Array)) throw new TypeError("write requires Uint8Array");
      return socket.write(chunk);
    },
    close() { return socket.closeWrite(); },
    abort() { return socket.abortWrite(); },
  });
  return {readable, writable, close() { return socket.fullClose(); }};
})`

func installStreams(t *testing.T, rt *quickjs.Runtime, bridge *socketBridge) error {
	t.Helper()
	socket, err := bridge.object()
	if err != nil {
		return err
	}
	factory, err := rt.Eval(streamsFactory)
	if err != nil {
		return err
	}
	streams, err := factory.Call(socket)
	if err != nil {
		return err
	}
	return rt.Set("streams", streams)
}

func runJS(t *testing.T, rt *quickjs.Runtime, loop *stdlib.Loop, src string) {
	t.Helper()
	if _, err := rt.Eval(src); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("JS loop: %v", err)
	}
	if done, err := rt.Get("testDone"); err == nil {
		if _, err := done.Await(ctx); err != nil {
			t.Fatalf("JS operation: %v", err)
		}
	}
}

func newRuntime(t *testing.T) (*quickjs.Runtime, *stdlib.Loop) {
	t.Helper()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	if err := stdlib.Install(rt, stdlib.Config{Stdout: io.Discard, Stderr: io.Discard, Loop: loop}); err != nil {
		_ = rt.Close()
		t.Fatal(err)
	}
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, loop
}

func TestWebStreamsHalfCloseTCPAndUnix(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			ln := listen(t, network)
			serverDone := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					serverDone <- fmt.Errorf("accept: %w", err)
					return
				}
				defer conn.Close()
				var lines []string
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					lines = append(lines, scanner.Text())
				}
				if err := scanner.Err(); err != nil {
					serverDone <- fmt.Errorf("read request: %w", err)
					return
				}
				if !bytes.Equal([]byte(strings.Join(lines, "\n")+"\n"), []byte("{\"n\":1}\n{\"n\":2}\n")) {
					serverDone <- fmt.Errorf("request lines: %q", lines)
					return
				}
				if _, err := conn.Write([]byte{0x00, 0xff}); err != nil {
					serverDone <- fmt.Errorf("write binary prefix: %w", err)
					return
				}
				if _, err := io.WriteString(conn, "{\"records\":2}\n"); err != nil {
					serverDone <- fmt.Errorf("write response: %w", err)
					return
				}
				if err := closeWrite(conn); err != nil {
					serverDone <- fmt.Errorf("close server write: %w", err)
					return
				}
				serverDone <- nil
			}()

			client, err := net.Dial(network, ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			rt, loop := newRuntime(t)
			if err := installStreams(t, rt, &socketBridge{rt: rt, conn: asDirectional(t, client)}); err != nil {
				t.Fatal(err)
			}
			runJS(t, rt, loop, `
globalThis.testDone = (async () => {
  const writer = streams.writable.getWriter();
  await writer.write(new TextEncoder().encode('{"n":1}\n'));
  await writer.write(new TextEncoder().encode('{"n":2}\n'));
  await writer.close(); // sends FIN for this direction; readable remains open
  const reader = streams.readable.getReader();
  const chunks = [];
  for (;;) { const part = await reader.read(); if (part.done) break; chunks.push(part.value); }
  const size = chunks.reduce((n, chunk) => n + chunk.length, 0);
  const response = new Uint8Array(size);
  let offset = 0;
  for (const chunk of chunks) { response.set(chunk, offset); offset += chunk.length; }
  if (response.length < 3 || response[0] !== 0 || response[1] !== 255)
    throw Error('binary response prefix was not preserved');
  const text = new TextDecoder().decode(response.subarray(2));
  if (text !== '{"records":2}\n') throw Error('unexpected response: ' + text);
})();`)
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("server did not observe client EOF")
			}
		})
	}
}

func TestReadCancelWriteAbortAndFullClose(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			t.Run("read-cancel-preserves-write", func(t *testing.T) {
				client, server := connectedPair(t, network)
				readStarted := make(chan struct{})
				readDone := make(chan error, 1)
				go func() {
					buf := make([]byte, 1)
					close(readStarted)
					_, err := client.Read(buf)
					readDone <- err
				}()
				<-readStarted
				select {
				case err := <-readDone:
					t.Fatalf("Read completed before CloseRead: %v", err)
				default:
				}
				if err := client.CloseRead(); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-readDone:
					if err == nil {
						t.Fatal("pending read unexpectedly succeeded")
					}
				case <-time.After(time.Second):
					t.Fatal("CloseRead did not wake pending Read")
				}
				if _, err := client.Write([]byte{0xa5}); err != nil {
					t.Fatalf("write after CloseRead: %v", err)
				}
				got := make([]byte, 1)
				if _, err := io.ReadFull(server, got); err != nil || got[0] != 0xa5 {
					t.Fatalf("peer read = %v, %v", got, err)
				}
			})

			t.Run("write-abort-preserves-read", func(t *testing.T) {
				client, server := connectedPair(t, network)
				setSmallWriteBuffer(t, client)
				serverDone := make(chan error, 1)
				allowDrain := make(chan struct{})
				go func() {
					<-allowDrain
					if _, err := io.Copy(io.Discard, server); err != nil {
						serverDone <- err
						return
					}
					_, err := io.WriteString(server, "still-readable")
					serverDone <- errors.Join(err, closeWrite(server))
				}()
				writeDone := make(chan error, 1)
				writeStarted := make(chan struct{})
				payload := bytes.Repeat([]byte{0x5a}, 32<<20)
				go func() {
					firstWrite := true
					for len(payload) != 0 {
						if firstWrite {
							close(writeStarted)
							firstWrite = false
						}
						n, err := client.Write(payload)
						payload = payload[n:]
						if err != nil {
							writeDone <- err
							return
						}
						if n == 0 {
							writeDone <- io.ErrShortWrite
							return
						}
					}
					writeDone <- nil
				}()
				<-writeStarted
				select {
				case err := <-writeDone:
					t.Fatalf("large Write completed before abort deadline: %v", err)
				default:
				}
				if err := errors.Join(client.SetWriteDeadline(time.Now()), client.CloseWrite()); err != nil {
					t.Fatal(err)
				}
				close(allowDrain)
				select {
				case err := <-writeDone:
					if err == nil {
						t.Fatal("large pending write was not interrupted")
					}
				case <-time.After(2 * time.Second):
					t.Fatal("write deadline did not wake pending Write")
				}
				if err := <-serverDone; err != nil {
					t.Fatalf("server: %v", err)
				}
				if err := client.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
				got, err := io.ReadAll(client)
				if err != nil {
					t.Fatal(err)
				}
				if string(got) != "still-readable" {
					t.Fatalf("response = %q", got)
				}
			})

			t.Run("full-close-wakes-read", func(t *testing.T) {
				client, _ := connectedPair(t, network)
				readStarted := make(chan struct{})
				readDone := make(chan error, 1)
				go func() {
					buf := make([]byte, 1)
					close(readStarted)
					_, err := client.Read(buf)
					readDone <- err
				}()
				<-readStarted
				select {
				case err := <-readDone:
					t.Fatalf("Read completed before full Close: %v", err)
				default:
				}
				if err := client.Close(); err != nil {
					t.Fatal(err)
				}
				select {
				case err := <-readDone:
					if err == nil {
						t.Fatal("pending read unexpectedly succeeded")
					}
				case <-time.After(time.Second):
					t.Fatal("full Close did not wake pending Read")
				}
			})
		})
	}
}

func TestReadableCancelPreservesWritable(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			ln := listen(t, network)
			serverDone := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()
				got, err := io.ReadAll(conn)
				if err == nil && string(got) != "write survives read cancel" {
					err = fmt.Errorf("request = %q", got)
				}
				serverDone <- err
			}()
			client, err := net.Dial(network, ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			rt, loop := newRuntime(t)
			bridge := &socketBridge{
				rt: rt, conn: asDirectional(t, client),
				readStarted: make(chan struct{}), readDone: make(chan struct{}), readErr: make(chan error, 1),
			}
			if err := installStreams(t, rt, bridge); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`
globalThis.reader = streams.readable.getReader();
globalThis.pendingRead = reader.read();
`); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bridge.readStarted:
			case <-time.After(time.Second):
				t.Fatal("read did not start")
			}
			select {
			case <-bridge.readDone:
				t.Fatal("Go Read completed before ReadableStream.cancel")
			default:
			}
			runJS(t, rt, loop, `globalThis.testDone = (async () => {
  await reader.cancel();
  const result = await pendingRead;
  if (!result.done) throw Error('pending read did not resolve as cancelled');
  const writer = streams.writable.getWriter();
  await writer.write(new TextEncoder().encode('write survives read cancel'));
  await writer.close();
})();`)
			select {
			case <-bridge.readDone:
			default:
				t.Fatal("Go Read did not terminate after ReadableStream.cancel")
			}
			if err := <-bridge.readErr; err == nil {
				t.Fatal("Go Read returned without an error after read-side shutdown")
			}
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("server did not receive after read cancellation")
			}
		})
	}
}

func TestWebStreamsExplicitFullClose(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			client, server := connectedPair(t, network)
			serverDone := make(chan error, 1)
			go func() {
				_, err := io.Copy(io.Discard, server)
				serverDone <- err
			}()
			rt, loop := newRuntime(t)
			bridge := &socketBridge{
				rt: rt, conn: client,
				readStarted: make(chan struct{}), readDone: make(chan struct{}), readErr: make(chan error, 1),
			}
			if err := installStreams(t, rt, bridge); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`
globalThis.reader = streams.readable.getReader();
globalThis.pendingRead = reader.read().catch(error => ({error}));
`); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bridge.readStarted:
			case <-time.After(time.Second):
				t.Fatal("read did not start")
			}
			select {
			case <-bridge.readDone:
				t.Fatal("Go Read completed before explicit full close")
			default:
			}
			runJS(t, rt, loop, `globalThis.testDone = (async () => {
  await streams.close();
  const result = await pendingRead;
  if (!result || !result.error) throw Error('full close did not reject the pending read');
})();`)
			select {
			case <-bridge.readDone:
			default:
				t.Fatal("Go Read did not terminate after explicit full close")
			}
			if err := <-bridge.readErr; err == nil {
				t.Fatal("Go Read returned without error after full close")
			}
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("peer did not observe full close")
			}
		})
	}
}

func TestWritableAbortInterruptsPendingWriteAndPreservesReadable(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			ln := listen(t, network)
			allowDrain := make(chan struct{})
			serverDone := make(chan error, 1)
			go func() {
				conn, err := ln.Accept()
				if err != nil {
					serverDone <- err
					return
				}
				defer conn.Close()
				<-allowDrain
				if _, err := io.Copy(io.Discard, conn); err != nil {
					serverDone <- err
					return
				}
				_, err = io.WriteString(conn, "read survives write abort")
				serverDone <- errors.Join(err, closeWrite(conn))
			}()
			client, err := net.Dial(network, ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			setSmallWriteBuffer(t, client)
			rt, loop := newRuntime(t)
			bridge := &socketBridge{
				rt: rt, conn: asDirectional(t, client),
				writeStarted: make(chan struct{}), writeDone: make(chan struct{}), abortStarted: make(chan struct{}),
			}
			if err := installStreams(t, rt, bridge); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`
globalThis.writer = streams.writable.getWriter();
globalThis.writePromise = writer.write(new Uint8Array(32 * 1024 * 1024));
globalThis.writePromiseSettled = false;
globalThis.writePromise.then(
  () => { writePromiseSettled = true; },
  () => { writePromiseSettled = true; },
);
`); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bridge.writeStarted:
			case <-time.After(time.Second):
				t.Fatal("write did not start")
			}
			select {
			case <-bridge.writeDone:
				t.Fatal("native Write completed before abort")
			default:
			}
			if _, err := rt.Eval(`if (writePromiseSettled) throw Error('write promise settled before abort');`); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`globalThis.abortPromise = writer.abort(new Error('stop'));`); err != nil {
				t.Fatal(err)
			}
			select {
			case <-bridge.abortStarted:
				close(allowDrain)
			case <-time.After(time.Second):
				close(allowDrain)
				t.Fatal("WritableStream.abort did not reach the Go endpoint while Write was pending")
			}
			select {
			case <-bridge.writeDone:
			case <-time.After(time.Second):
				t.Fatal("native Write did not terminate after abort")
			}
			runJS(t, rt, loop, `globalThis.testDone = (async () => {
  await abortPromise;
  const writeResult = await writePromise.then(
    () => ({ok: true}),
    error => ({error}),
  );
  if (!writeResult.error || !(writeResult.error instanceof Error))
    throw Error('write promise did not reject with an Error after abort');
  const reader = streams.readable.getReader();
  const parts = [];
  for (;;) { const p = await reader.read(); if (p.done) break; parts.push(new TextDecoder().decode(p.value)); }
  if (parts.join('') !== 'read survives write abort') throw Error('response = ' + parts.join(''));
})();`)
			select {
			case err := <-serverDone:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("server did not finish after aborted write")
			}
		})
	}
}

func TestListenerCloseUnblocksAcceptAndKeepsLoopAlive(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			ln := listen(t, network)
			rt, loop := newRuntime(t)
			h := ownListener(rt, ln)
			accept := h.Accept(rt)
			if err := rt.Set("acceptPromise", accept); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`acceptPromise.catch(() => {});`); err != nil {
				t.Fatal(err)
			}
			closed := make(chan struct{})
			go func() {
				time.Sleep(20 * time.Millisecond)
				_ = h.Close()
				close(closed)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := loop.Run(ctx); err != nil {
				t.Fatalf("loop after listener close: %v", err)
			}
			<-closed
			if _, err := accept.Await(ctx); err == nil {
				t.Fatal("Accept resolved after listener close")
			}
		})
	}
}

func TestOpenListenerAloneKeepsLoopAliveUntilGoClose(t *testing.T) {
	ln := listen(t, "tcp")
	rt, loop := newRuntime(t)
	h := ownListener(rt, ln)
	if _, err := rt.Eval(`globalThis.listenerOnly = true`); err != nil {
		t.Fatal(err)
	}
	closed := make(chan struct{})
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = h.Close()
		close(closed)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := loop.Run(ctx); err != nil {
		t.Fatalf("loop: %v", err)
	}
	select {
	case <-closed:
	default:
		t.Fatal("loop stopped before listener close")
	}
}

func TestRuntimeCloseReclaimsSocketWithoutJSCallback(t *testing.T) {
	client, server := connectedPair(t, "tcp")
	rt, _ := newRuntime(t)
	bridge := &socketBridge{
		rt: rt, conn: client,
		readStarted: make(chan struct{}), readDone: make(chan struct{}), readErr: make(chan error, 1),
	}
	if err := installStreams(t, rt, bridge); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`globalThis.pendingRead = streams.readable.getReader().read();`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-bridge.readStarted:
	case <-time.After(time.Second):
		t.Fatal("pending socket read did not start")
	}
	select {
	case <-bridge.readDone:
		t.Fatal("Go Read completed before runtime close")
	default:
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-bridge.readDone:
	case <-time.After(time.Second):
		t.Fatal("runtime close did not release the Go socket read")
	}
	if err := <-bridge.readErr; err == nil {
		t.Fatal("Go Read returned without error after runtime close")
	}
	if err := server.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Read(make([]byte, 1)); err == nil {
		t.Fatal("peer did not observe connection close")
	}
}

func TestUnixSocketPathCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("socket path not created: %v", err)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path after normal Close = %v, want not-exist", err)
	}
}

func TestUnixForcedKillLeavesSocketPath(t *testing.T) {
	if path := os.Getenv(socketChildEnv); path != "" {
		ln, err := net.Listen("unix", path)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		fmt.Fprintln(os.Stdout, "READY")
		_, _ = ln.Accept()
		return
	}

	path := filepath.Join(t.TempDir(), "killed.sock")
	cmd := exec.Command(os.Args[0], "-test.run=^TestUnixForcedKillLeavesSocketPath$")
	cmd.Env = append(os.Environ(), socketChildEnv+"="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- line
	}()
	select {
	case line := <-ready:
		if line != "READY\n" {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			t.Fatalf("child readiness %q; stderr=%s", line, stderr.String())
		}
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("child did not listen; stderr=%s", stderr.String())
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("killed child exited successfully")
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("socket path after forced child kill: %v", err)
	}
	if info.Mode()&os.ModeSocket == 0 {
		t.Fatalf("residual path mode %v is not a socket", info.Mode())
	}
}
