package nativeio

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed js/net.js
var netSource string

type netConnectRequest struct {
	Network string `json:"network"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	Path    string `json:"path"`
}

type netDialFunc func(context.Context, string, string) (net.Conn, error)

func (h *Host) installNet() error {
	return h.installNetWithFuncs(nil, nil)
}

// installNetWithDial keeps the public adapter identical in tests while letting
// cancellation tests control when the Go dial operation starts and completes.
func (h *Host) installNetWithDial(dial netDialFunc) error {
	return h.installNetWithFuncs(dial, nil)
}

func (h *Host) installNetWithFuncs(dial netDialFunc, listen netListenFunc) error {
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	if listen == nil {
		listen = func(ctx context.Context, network, address string) (net.Listener, error) {
			return (&net.ListenConfig{}).Listen(ctx, network, address)
		}
	}
	api := h.rt.NewObject()
	if err := api.Set("connect", func(payload string) quickjs.Value {
		return h.startConnect(payload, dial)
	}); err != nil {
		return err
	}
	if err := api.Set("listen", func(payload string) quickjs.Value {
		return h.startListen(payload, listen)
	}); err != nil {
		return err
	}
	factory, err := h.rt.EvalFile("<noe-net>", netSource)
	if err != nil {
		return err
	}
	values, err := factory.Call(api)
	if err != nil {
		return err
	}
	connect, err := values.Get("connect")
	if err != nil {
		return err
	}
	listenExport, err := values.Get("listen")
	if err != nil {
		return err
	}
	return h.rt.SetModuleValues("noe:sockets", map[string]quickjs.Value{"connect": connect, "listen": listenExport})
}

// startConnect returns a JS operation pair. The adapter owns AbortSignal
// subscription; this handle exposes only a start-time cancellation hook, so a
// signal cannot close a connection after ownership has transferred.
func (h *Host) startConnect(payload string, dial netDialFunc) quickjs.Value {
	reject := func(err error) quickjs.Value {
		return h.rejectedNetOperation(err)
	}
	promise := h.rt.NewPromise()
	var request netConnectRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return reject(fmt.Errorf("invalid connect options: %w", err))
	}
	network, address, err := h.connectAddress(request)
	if err != nil {
		return reject(err)
	}

	attemptCtx, cancel := context.WithCancel(h.ctx)
	attempt := &connectAttempt{
		h:      h,
		ctx:    attemptCtx,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if !h.ownConnectAttempt(attempt) {
		return reject(os.ErrClosed)
	}
	work := h.rt.StartAsyncWork()
	h.active.Add(1)
	if !attempt.start(func() {
		defer h.wg.Done()
		conn, dialErr := dial(attemptCtx, network, address)
		attempt.finishDial(conn, dialErr)
		work.Complete(func(closed error) {
			defer h.active.Add(-1)
			if closed != nil {
				_ = attempt.Close()
				return
			}
			conn, dialErr := attempt.handoff()
			if dialErr != nil {
				attempt.finish()
				promise.RejectError(dialErr)
				return
			}
			socket, objectErr := newNetConnection(h, conn)
			if objectErr != nil {
				_ = conn.Close()
				promise.RejectError(objectErr)
				return
			}
			value, objectErr := socket.object()
			if objectErr != nil {
				promise.RejectError(objectErr)
				return
			}
			if err := promise.Resolve(value); err != nil {
				_ = socket.Close()
				promise.RejectError(err)
			}
		})
	}) {
		h.wg.Done()
		work.Complete(func(closed error) {
			defer h.active.Add(-1)
			attempt.finish()
			if closed == nil {
				promise.RejectError(os.ErrClosed)
			}
		})
	}
	operation := h.rt.NewObject()
	if err := errors.Join(operation.Set("promise", promise.Value()), operation.Set("cancel", func() {
		attempt.cancelPending()
	})); err != nil {
		attempt.cancelPending()
		return reject(err)
	}
	return operation
}

func (h *Host) rejectedNetOperation(err error) quickjs.Value {
	promise := h.rt.NewPromise()
	promise.RejectError(err)
	operation := h.rt.NewObject()
	if setErr := errors.Join(operation.Set("promise", promise.Value()), operation.Set("cancel", func() {})); setErr != nil {
		return promise.Value()
	}
	return operation
}

// ownConnectAttempt adds the resource and its Go worker under the same lock
// Host.Close uses before it begins waiting. This prevents a late h.wg.Add from
// racing with runtime shutdown.
func (h *Host) ownConnectAttempt(attempt *connectAttempt) bool {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		_ = attempt.Close()
		return false
	}
	h.resources[attempt] = struct{}{}
	h.wg.Add(1)
	h.mu.Unlock()
	return true
}

func (h *Host) connectAddress(request netConnectRequest) (network, address string, err error) {
	switch request.Network {
	case "tcp":
		if request.Host == "" || strings.IndexByte(request.Host, 0) >= 0 {
			return "", "", errors.New("tcp host must be a nonempty string without NUL")
		}
		if request.Port < 1 || request.Port > 65535 {
			return "", "", errors.New("tcp port must be between 1 and 65535")
		}
		return "tcp", net.JoinHostPort(request.Host, strconv.Itoa(request.Port)), nil
	case "unix":
		if request.Path == "" || strings.IndexByte(request.Path, 0) >= 0 {
			return "", "", errors.New("unix path must be a nonempty string without NUL")
		}
		path := request.Path
		if !filepath.IsAbs(path) {
			base := h.opts.Cwd
			if base == "" {
				base, err = os.Getwd()
				if err != nil {
					return "", "", err
				}
			}
			path = filepath.Join(base, path)
		}
		return "unix", path, nil
	default:
		return "", "", errors.New("network must be tcp or unix")
	}
}

type connectAttempt struct {
	h        *Host
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	started  bool
	closed   bool
	canceled bool
	handed   bool
	done     chan struct{}
	doneOnce sync.Once
	close    sync.Once
	conn     net.Conn
	dialErr  error
	closeErr error
}

func (a *connectAttempt) start(run func()) bool {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return false
	}
	a.started = true
	a.mu.Unlock()
	go run()
	return true
}

func (a *connectAttempt) finishDial(conn net.Conn, err error) {
	a.mu.Lock()
	discard := a.closed || a.canceled || a.ctx.Err() != nil
	if discard {
		if err == nil {
			err = a.ctx.Err()
		}
	} else {
		a.conn = conn
		a.dialErr = err
		conn = nil
	}
	if !discard {
		a.doneOnce.Do(func() { close(a.done) })
	}
	a.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	if discard {
		a.doneOnce.Do(func() { close(a.done) })
	}
}

func (a *connectAttempt) handoff() (net.Conn, error) {
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.canceled || a.ctx.Err() != nil {
		if err := a.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	if a.dialErr != nil {
		return nil, a.dialErr
	}
	if a.conn == nil {
		return nil, errors.New("connect completed without a connection")
	}
	a.handed = true
	conn := a.conn
	a.conn = nil
	a.cancel()
	a.h.release(a)
	return conn, nil
}

func (a *connectAttempt) cancelPending() {
	a.mu.Lock()
	if a.closed || a.handed {
		a.mu.Unlock()
		return
	}
	a.canceled = true
	a.cancel()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (a *connectAttempt) finish() {
	a.mu.Lock()
	a.handed = true
	a.cancel()
	conn := a.conn
	a.conn = nil
	a.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
	a.h.release(a)
}

func (a *connectAttempt) Close() error {
	a.close.Do(func() {
		a.mu.Lock()
		a.closed = true
		a.cancel()
		started := a.started
		if !started {
			a.doneOnce.Do(func() { close(a.done) })
		}
		a.mu.Unlock()
		<-a.done
		a.mu.Lock()
		conn := a.conn
		a.conn = nil
		a.mu.Unlock()
		if conn != nil {
			a.closeErr = conn.Close()
		}
		a.h.release(a)
	})
	return a.closeErr
}

type directionalNetConn interface {
	net.Conn
	CloseRead() error
	CloseWrite() error
}

type netConnection struct {
	h           *Host
	conn        directionalNetConn
	work        *quickjs.AsyncWork
	mu          sync.Mutex
	closed      bool
	readClosed  bool
	writeClosed bool
	readIO      sync.WaitGroup
	writeIO     sync.WaitGroup
	io          sync.WaitGroup
	close       sync.Once
	closeDone   chan struct{}
	closeErr    error
}

func newNetConnection(h *Host, conn net.Conn) (*netConnection, error) {
	directional, ok := conn.(directionalNetConn)
	if !ok {
		return nil, fmt.Errorf("%T does not support directional socket shutdown", conn)
	}
	socket := &netConnection{
		h:         h,
		conn:      directional,
		work:      h.rt.StartAsyncWork(),
		closeDone: make(chan struct{}),
	}
	h.active.Add(1)
	if !h.own(socket) {
		return nil, os.ErrClosed
	}
	return socket, nil
}

func (c *netConnection) object() (quickjs.Value, error) {
	o := c.h.rt.NewObject()
	if err := errors.Join(
		o.Set("read", func() *quickjs.Promise { return c.read() }),
		o.Set("write", func(chunk quickjs.Value) *quickjs.Promise { return c.write(chunk) }),
		o.Set("cancelRead", func() *quickjs.Promise { return c.cancelRead() }),
		o.Set("closeWrite", func() *quickjs.Promise { return c.closeWrite() }),
		o.Set("abortWrite", func() *quickjs.Promise { return c.abortWrite() }),
		o.Set("close", func() *quickjs.Promise { return c.closePromise() }),
	); err != nil {
		_ = c.Close()
		return quickjs.Value{}, err
	}
	return o, nil
}

func (c *netConnection) read() *quickjs.Promise {
	return c.h.async(func() (any, error) {
		if err := c.beginIO(true); err != nil {
			return nil, err
		}
		defer c.io.Done()
		defer c.readIO.Done()
		buf := make([]byte, 64<<10)
		for {
			n, err := c.conn.Read(buf)
			if n > 0 {
				return append([]byte(nil), buf[:n]...), nil
			}
			if errors.Is(err, io.EOF) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
		}
	}, func(value any) (any, error) {
		if value == nil {
			return nil, nil
		}
		return c.h.rt.NewBytes(value.([]byte)), nil
	}, nil)
}

func (c *netConnection) write(chunk quickjs.Value) *quickjs.Promise {
	buf, ok := chunk.Bytes()
	if !ok {
		p := c.h.rt.NewPromise()
		p.RejectError(errors.New("write requires Uint8Array"))
		return p
	}
	data := append([]byte(nil), buf...)
	return c.h.async(func() (any, error) {
		if err := c.beginIO(false); err != nil {
			return nil, err
		}
		defer c.io.Done()
		defer c.writeIO.Done()
		for len(data) != 0 {
			n, err := c.conn.Write(data)
			data = data[n:]
			if err != nil {
				return nil, err
			}
			if n == 0 {
				return nil, io.ErrShortWrite
			}
		}
		return nil, nil
	}, nil, nil)
}

func (c *netConnection) beginIO(read bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return net.ErrClosed
	}
	if read && c.readClosed {
		return net.ErrClosed
	}
	if !read && c.writeClosed {
		return net.ErrClosed
	}
	c.io.Add(1)
	if read {
		c.readIO.Add(1)
	} else {
		c.writeIO.Add(1)
	}
	return nil
}

func (c *netConnection) cancelRead() *quickjs.Promise {
	return c.h.async(func() (any, error) {
		c.mu.Lock()
		if c.closed || c.readClosed {
			c.mu.Unlock()
			return nil, nil
		}
		c.readClosed = true
		c.mu.Unlock()
		err := c.conn.CloseRead()
		c.readIO.Wait()
		if errors.Is(err, net.ErrClosed) {
			return nil, nil
		}
		return nil, err
	}, nil, nil)
}

func (c *netConnection) closeWrite() *quickjs.Promise {
	return c.h.async(func() (any, error) {
		c.mu.Lock()
		if c.closed || c.writeClosed {
			c.mu.Unlock()
			return nil, nil
		}
		c.writeClosed = true
		c.mu.Unlock()
		err := c.conn.CloseWrite()
		if errors.Is(err, net.ErrClosed) {
			return nil, nil
		}
		return nil, err
	}, nil, nil)
}

func (c *netConnection) abortWrite() *quickjs.Promise {
	return c.h.async(func() (any, error) {
		c.mu.Lock()
		if c.closed || c.writeClosed {
			c.mu.Unlock()
			return nil, nil
		}
		c.writeClosed = true
		c.mu.Unlock()
		deadlineErr := c.conn.SetWriteDeadline(time.Now())
		closeErr := c.conn.CloseWrite()
		c.writeIO.Wait()
		return nil, errors.Join(deadlineErr, closeErr)
	}, nil, nil)
}

func (c *netConnection) closePromise() *quickjs.Promise {
	return c.h.async(func() (any, error) { return nil, c.Close() }, nil, nil)
}

func (c *netConnection) Close() error {
	c.close.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.readClosed = true
		c.writeClosed = true
		c.mu.Unlock()
		c.closeErr = c.conn.Close()
		c.io.Wait()
		c.h.release(c)
		c.work.Done()
		c.h.active.Add(-1)
		close(c.closeDone)
	})
	<-c.closeDone
	if errors.Is(c.closeErr, net.ErrClosed) {
		return nil
	}
	return c.closeErr
}
