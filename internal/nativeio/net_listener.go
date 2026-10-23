package nativeio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type netListenFunc func(context.Context, string, string) (net.Listener, error)

func (h *Host) startListen(payload string, listen netListenFunc) quickjs.Value {
	reject := func(err error) quickjs.Value {
		return h.rejectedNetOperation(err)
	}
	promise := h.rt.NewPromise()
	var request netConnectRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return reject(fmt.Errorf("invalid listen options: %w", err))
	}
	network, address, path, err := h.listenAddress(request)
	if err != nil {
		return reject(err)
	}

	listenCtx, cancel := context.WithCancel(h.ctx)
	attempt := &listenAttempt{h: h, ctx: listenCtx, cancel: cancel, done: make(chan struct{})}
	if !h.ownListenAttempt(attempt) {
		return reject(os.ErrClosed)
	}
	work := h.rt.StartAsyncWork()
	if !attempt.start(func() {
		defer h.wg.Done()
		var bound *boundNetListener
		var listenErr error
		if network == "unix" {
			bound, listenErr = listenUnixSocket(listenCtx, path, listen)
		} else {
			var listener net.Listener
			listener, listenErr = listen(listenCtx, network, address)
			if listenErr == nil {
				bound, listenErr = newBoundNetListener(listener, network, "")
			}
		}
		attempt.finishListen(bound, listenErr)
		work.Complete(func(closed error) {
			if closed != nil || h.ctx.Err() != nil {
				_ = attempt.Close()
				return
			}
			bound, listenErr := attempt.handoff()
			if listenErr != nil {
				attempt.finish()
				promise.RejectError(listenErr)
				return
			}
			handle, handleErr := newNetListener(h, bound)
			if handleErr != nil {
				_ = bound.Close()
				promise.RejectError(handleErr)
				return
			}
			value, handleErr := handle.object()
			if handleErr != nil {
				_ = handle.Close()
				promise.RejectError(handleErr)
				return
			}
			if err := promise.Resolve(value); err != nil {
				_ = handle.Close()
				promise.RejectError(err)
			}
		})
	}) {
		h.wg.Done()
		work.Complete(func(closed error) {
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

func (h *Host) ownListenAttempt(attempt *listenAttempt) bool {
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

func (h *Host) listenAddress(request netConnectRequest) (network, address, path string, err error) {
	switch request.Network {
	case "tcp":
		if request.Host == "" || strings.IndexByte(request.Host, 0) >= 0 {
			return "", "", "", errors.New("tcp host must be a nonempty string without NUL")
		}
		if request.Port < 0 || request.Port > 65535 {
			return "", "", "", errors.New("tcp port must be between 0 and 65535")
		}
		return "tcp", net.JoinHostPort(request.Host, strconv.Itoa(request.Port)), "", nil
	case "unix":
		if request.Path == "" || strings.IndexByte(request.Path, 0) >= 0 {
			return "", "", "", errors.New("unix path must be a nonempty string without NUL")
		}
		path = request.Path
		if !filepath.IsAbs(path) {
			base := h.opts.Cwd
			if base == "" {
				base, err = os.Getwd()
				if err != nil {
					return "", "", "", err
				}
			}
			path = filepath.Join(base, path)
		}
		maxPath := len(syscall.RawSockaddrUnix{}.Path) - 1
		if len(path) > maxPath {
			return "", "", "", fmt.Errorf("unix path must be at most %d bytes", maxPath)
		}
		return "unix", path, path, nil
	default:
		return "", "", "", errors.New("network must be tcp or unix")
	}
}

type listenAttempt struct {
	h         *Host
	ctx       context.Context
	cancel    context.CancelFunc
	mu        sync.Mutex
	started   bool
	closed    bool
	canceled  bool
	handed    bool
	done      chan struct{}
	doneOnce  sync.Once
	close     sync.Once
	listener  *boundNetListener
	listenErr error
	closeErr  error
}

func (a *listenAttempt) start(run func()) bool {
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

func (a *listenAttempt) finishListen(listener *boundNetListener, err error) {
	a.mu.Lock()
	discard := a.closed || a.canceled || a.ctx.Err() != nil
	if discard {
		if err == nil {
			err = a.ctx.Err()
		}
	} else {
		a.listener = listener
		a.listenErr = err
		listener = nil
	}
	if !discard {
		a.doneOnce.Do(func() { close(a.done) })
	}
	a.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	if discard {
		a.doneOnce.Do(func() { close(a.done) })
	}
}

func (a *listenAttempt) handoff() (*boundNetListener, error) {
	<-a.done
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.canceled || a.ctx.Err() != nil {
		if err := a.ctx.Err(); err != nil {
			return nil, err
		}
		return nil, context.Canceled
	}
	if a.listenErr != nil {
		return nil, a.listenErr
	}
	if a.listener == nil {
		return nil, errors.New("listen completed without a listener")
	}
	a.handed = true
	listener := a.listener
	a.listener = nil
	a.cancel()
	a.h.release(a)
	return listener, nil
}

func (a *listenAttempt) cancelPending() {
	a.mu.Lock()
	if a.closed || a.handed {
		a.mu.Unlock()
		return
	}
	a.canceled = true
	a.cancel()
	listener := a.listener
	a.listener = nil
	a.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
}

func (a *listenAttempt) finish() {
	a.mu.Lock()
	a.handed = true
	a.cancel()
	listener := a.listener
	a.listener = nil
	a.mu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
	a.h.release(a)
}

func (a *listenAttempt) Close() error {
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
		listener := a.listener
		a.listener = nil
		a.mu.Unlock()
		if listener != nil {
			a.closeErr = listener.Close()
		}
		a.h.release(a)
	})
	return a.closeErr
}

// boundNetListener owns the underlying socket and, for Unix sockets, the
// pathname identity created by bind. The standard UnixListener unlink hook is
// disabled so close cannot delete a replacement at the same path.
type boundNetListener struct {
	net.Listener
	network  string
	path     string // Published path returned to callers and cleaned on close.
	stage    string // Private pathname used to bind, if its temporary link remains.
	stageDir string
	alias    string
	aliasDir string
	aliasID  os.FileInfo
	identity os.FileInfo
	close    sync.Once
	closeErr error
}

func newBoundNetListener(listener net.Listener, network, path string) (*boundNetListener, error) {
	bound := &boundNetListener{Listener: listener, network: network, path: path}
	if network != "unix" {
		return bound, nil
	}
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("unix listener has unexpected type %T", listener)
	}
	unixListener.SetUnlinkOnClose(false)
	identity, err := os.Lstat(path)
	if err != nil {
		_ = listener.Close()
		return nil, err
	}
	bound.identity = identity
	return bound, nil
}

// listenUnixSocket binds under an owner-only directory on the target
// filesystem, snapshots that private socket, then publishes it with a
// no-overwrite hard link. This avoids taking the identity from a replaceable
// public pathname after bind. A short symlink alias is used only when the
// private bind path exceeds the platform's sockaddr_un pathname limit.
func listenUnixSocket(ctx context.Context, publicPath string, listen netListenFunc) (*boundNetListener, error) {
	stageDir, err := os.MkdirTemp(filepath.Dir(publicPath), ".noe-net-")
	if err != nil {
		return nil, fmt.Errorf("create private Unix socket staging directory: %w", err)
	}
	stagePath := filepath.Join(stageDir, "s")
	listener, err := listen(ctx, "unix", stagePath)
	var aliasPath, aliasDir string
	var aliasID os.FileInfo
	if isUnixAddressTooLong(err) {
		if listener != nil {
			_ = listener.Close()
			listener = nil
		}
		aliasDir, aliasPath, aliasID, err = createUnixStageAlias(stageDir)
		if err == nil {
			listener, err = listen(ctx, "unix", filepath.Join(aliasPath, "s"))
		}
	}
	if err != nil {
		if listener != nil {
			_ = listener.Close()
		}
		_ = cleanupPrivateStage(stagePath, stageDir)
		_ = cleanupUnixAlias(aliasPath, aliasDir, aliasID)
		return nil, fmt.Errorf("bind private Unix socket: %w", err)
	}
	bound, err := newBoundNetListener(listener, "unix", stagePath)
	if err != nil {
		_ = cleanupPrivateStage(stagePath, stageDir)
		_ = cleanupUnixAlias(aliasPath, aliasDir, aliasID)
		return nil, err
	}
	bound.path = publicPath
	bound.stage = stagePath
	bound.stageDir = stageDir
	bound.alias = aliasPath
	bound.aliasDir = aliasDir
	bound.aliasID = aliasID
	if err := os.Link(stagePath, publicPath); err != nil {
		closeErr := bound.Close()
		return nil, errors.Join(fmt.Errorf("publish Unix socket pathname with a no-overwrite hard link: %w", err), closeErr)
	}
	if err := cleanupPrivateStage(stagePath, stageDir); err != nil {
		closeErr := bound.Close()
		return nil, errors.Join(fmt.Errorf("remove private Unix socket staging pathname: %w", err), closeErr)
	}
	bound.stage = ""
	bound.stageDir = ""
	if err := cleanupUnixAlias(aliasPath, aliasDir, aliasID); err != nil {
		closeErr := bound.Close()
		return nil, errors.Join(fmt.Errorf("remove private Unix socket alias: %w", err), closeErr)
	}
	bound.alias = ""
	bound.aliasDir = ""
	bound.aliasID = nil
	return bound, nil
}

func isUnixAddressTooLong(err error) bool {
	return errors.Is(err, syscall.EINVAL)
}

func createUnixStageAlias(stageDir string) (aliasDir, aliasPath string, aliasID os.FileInfo, err error) {
	candidates := []string{"/tmp", os.TempDir()}
	var failures []error
	seen := make(map[string]struct{}, len(candidates))
	for _, root := range candidates {
		root = filepath.Clean(root)
		if _, ok := seen[root]; ok {
			continue
		}
		seen[root] = struct{}{}
		aliasDir, err = os.MkdirTemp(root, ".noe-alias-")
		if err != nil {
			failures = append(failures, err)
			continue
		}
		aliasPath = filepath.Join(aliasDir, "d")
		if err = os.Symlink(stageDir, aliasPath); err != nil {
			failures = append(failures, err)
			_ = os.Remove(aliasDir)
			aliasDir, aliasPath = "", ""
			continue
		}
		aliasID, err = os.Lstat(aliasPath)
		if err == nil {
			return aliasDir, aliasPath, aliasID, nil
		}
		_ = os.Remove(aliasPath)
		_ = os.Remove(aliasDir)
		failures = append(failures, err)
		aliasDir, aliasPath = "", ""
	}
	return "", "", nil, errors.Join(append([]error{errors.New("no short temporary directory is available for a Unix socket alias")}, failures...)...)
}

func cleanupPrivateStage(stagePath, stageDir string) error {
	var errs []error
	if stagePath != "" {
		if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	if stageDir != "" {
		if err := os.Remove(stageDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func cleanupUnixAlias(aliasPath, aliasDir string, aliasID os.FileInfo) error {
	var errs []error
	if aliasPath != "" {
		current, err := os.Lstat(aliasPath)
		if os.IsNotExist(err) {
			// Already gone.
		} else if err != nil {
			errs = append(errs, err)
		} else if aliasID == nil || os.SameFile(aliasID, current) {
			if err := os.Remove(aliasPath); err != nil && !os.IsNotExist(err) {
				errs = append(errs, err)
			}
		}
	}
	if aliasDir != "" {
		if err := os.Remove(aliasDir); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (l *boundNetListener) Close() error {
	l.close.Do(func() {
		l.closeErr = l.Listener.Close()
		if l.network != "unix" || l.identity == nil {
			return
		}
		l.closeErr = errors.Join(l.closeErr, removeIfSameFile(l.path, l.identity))
		l.closeErr = errors.Join(l.closeErr, removeIfSameFile(l.stage, l.identity))
		l.closeErr = errors.Join(l.closeErr, cleanupPrivateStage("", l.stageDir))
		l.closeErr = errors.Join(l.closeErr, cleanupUnixAlias(l.alias, l.aliasDir, l.aliasID))
	})
	if errors.Is(l.closeErr, net.ErrClosed) {
		return nil
	}
	return l.closeErr
}

func removeIfSameFile(path string, identity os.FileInfo) error {
	if path == "" {
		return nil
	}
	current, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if os.SameFile(identity, current) {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

type netListener struct {
	h         *Host
	bound     *boundNetListener
	work      *quickjs.AsyncWork
	mu        sync.Mutex
	closed    bool
	accepts   sync.WaitGroup
	close     sync.Once
	closeDone chan struct{}
	closeErr  error
}

func newNetListener(h *Host, bound *boundNetListener) (*netListener, error) {
	listener := &netListener{h: h, bound: bound, work: h.rt.StartAsyncWork(), closeDone: make(chan struct{})}
	if !h.own(listener) {
		return nil, os.ErrClosed
	}
	return listener, nil
}

func (l *netListener) object() (quickjs.Value, error) {
	o := l.h.rt.NewObject()
	address := l.h.rt.NewObject()
	var err error
	switch addr := l.bound.Addr().(type) {
	case *net.TCPAddr:
		host := addr.IP.String()
		if host == "<nil>" {
			host = ""
		}
		err = errors.Join(address.Set("network", "tcp"), address.Set("host", host), address.Set("port", addr.Port))
	case *net.UnixAddr:
		path := l.bound.path
		if path == "" {
			path = addr.Name
		}
		err = errors.Join(address.Set("network", "unix"), address.Set("path", path))
	default:
		return quickjs.Value{}, fmt.Errorf("unsupported listener address %T", l.bound.Addr())
	}
	if err == nil {
		err = errors.Join(o.Set("address", address), o.Set("accept", func() *quickjs.Promise { return l.accept() }), o.Set("close", func() *quickjs.Promise { return l.closePromise() }))
	}
	if err != nil {
		_ = l.Close()
		return quickjs.Value{}, err
	}
	return o, nil
}

func (l *netListener) accept() *quickjs.Promise {
	promise := l.h.rt.NewPromise()
	work := l.h.rt.StartAsyncWork()
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		work.Complete(func(closed error) {
			if closed == nil {
				promise.RejectError(net.ErrClosed)
			}
		})
		return promise
	}
	l.accepts.Add(1)
	l.h.mu.Lock()
	if l.h.closed {
		l.h.mu.Unlock()
		l.accepts.Done()
		l.mu.Unlock()
		work.Complete(func(closed error) {
			if closed == nil {
				promise.RejectError(os.ErrClosed)
			}
		})
		return promise
	}
	l.h.wg.Add(1)
	l.h.mu.Unlock()
	l.mu.Unlock()
	go func() {
		defer l.h.wg.Done()
		defer l.accepts.Done()
		conn, err := l.bound.Accept()
		work.Complete(func(closed error) {
			if closed != nil {
				if conn != nil {
					_ = conn.Close()
				}
				return
			}
			l.completeAccept(promise, conn, err)
		})
	}()
	return promise
}

// completeAccept runs on the runtime owner after the Go Accept result has
// been queued. An explicit close that happened before this delivery still
// rejects the pending accept and disposes of its not-yet-handed-off socket.
func (l *netListener) completeAccept(promise *quickjs.Promise, conn net.Conn, err error) {
	l.mu.Lock()
	listenerClosed := l.closed
	l.mu.Unlock()
	if listenerClosed {
		if conn != nil {
			_ = conn.Close()
		}
		promise.RejectError(net.ErrClosed)
		return
	}
	if err != nil {
		promise.RejectError(err)
		return
	}
	socket, socketErr := newNetConnection(l.h, conn)
	if socketErr != nil {
		_ = conn.Close()
		promise.RejectError(socketErr)
		return
	}
	value, socketErr := socket.object()
	if socketErr != nil {
		_ = socket.Close()
		promise.RejectError(socketErr)
		return
	}
	if resolveErr := promise.Resolve(value); resolveErr != nil {
		_ = socket.Close()
		promise.RejectError(resolveErr)
	}
}

func (l *netListener) closePromise() *quickjs.Promise {
	promise := l.h.rt.NewPromise()
	work := l.h.rt.StartAsyncWork()
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.h.mu.Lock()
	if l.h.closed {
		l.h.mu.Unlock()
		work.Complete(func(closed error) {
			if closed == nil {
				promise.RejectError(os.ErrClosed)
			}
		})
		return promise
	}
	l.h.wg.Add(1)
	l.h.mu.Unlock()
	go func() {
		defer l.h.wg.Done()
		err := l.Close()
		work.Complete(func(closed error) {
			if closed != nil {
				return
			}
			if err != nil {
				promise.RejectError(err)
			} else {
				_ = promise.Resolve(nil)
			}
		})
	}()
	return promise
}

func (l *netListener) Close() error {
	l.close.Do(func() {
		l.mu.Lock()
		l.closed = true
		l.mu.Unlock()
		l.closeErr = l.bound.Close()
		l.accepts.Wait()
		l.h.release(l)
		l.work.Done()
		close(l.closeDone)
	})
	<-l.closeDone
	return l.closeErr
}
