package stdlib

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// fsWatchHost owns every native watcher created by one fs installation. Its
// context follows the same VM, loop, and execution-scope lifetime as fileHost.
type fsWatchHost struct {
	fs     *fsHost
	rt     *quickjs.Runtime
	ctx    context.Context
	cancel context.CancelFunc

	mu        sync.Mutex
	closed    bool
	resources map[*fsWatch]struct{}
	closeDone chan struct{}
}

func newFSWatchHost(f *fsHost) (*fsWatchHost, error) {
	if f == nil || f.rt == nil || f.backend == nil {
		return nil, errors.New("filesystem watch needs a runtime and filesystem backend")
	}
	parent := f.rt.Context()
	if f.cfg != nil && f.cfg.Loop != nil {
		if !f.cfg.Loop.Owns(f.rt) {
			return nil, errors.New("filesystem watch loop belongs to another runtime")
		}
		parent = loopContext(f.cfg.Loop)
	}
	ctx, cancel := context.WithCancel(parent)
	h := &fsWatchHost{
		fs: f, rt: f.rt, ctx: ctx, cancel: cancel,
		resources: make(map[*fsWatch]struct{}), closeDone: make(chan struct{}),
	}
	if f.cfg != nil && f.cfg.Loop != nil {
		if scope := f.cfg.Loop.executionScope(); scope != nil {
			err := scope.AddCleanup(h.Close)
			if err != nil {
				h.Close()
				return nil, err
			}
		}
	}
	f.rt.OnClose(h.Close)
	// Keep the stop function local and discard it. Close can be called by this
	// callback itself, so publishing a mutable stop field would race an already
	// cancelled parent; the callback is one-shot and the closed host is inert.
	context.AfterFunc(ctx, h.Close)
	if ctx.Err() != nil {
		h.Close()
		return nil, errors.New("filesystem watch host is closed")
	}
	return h, nil
}

// Close is Go-only: it is safe from a runtime, loop, or execution-scope
// cleanup hook and never touches a JavaScript value.
func (h *fsWatchHost) Close() {
	h.mu.Lock()
	if h.closed {
		done := h.closeDone
		h.mu.Unlock()
		<-done
		return
	}
	h.closed = true
	resources := make([]*fsWatch, 0, len(h.resources))
	for resource := range h.resources {
		resources = append(resources, resource)
	}
	h.resources = nil
	h.mu.Unlock()

	h.cancel()
	for _, resource := range resources {
		resource.close(false)
	}
	close(h.closeDone)
}

func (h *fsWatchHost) own(resource *fsWatch) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return false
	}
	h.resources[resource] = struct{}{}
	return true
}

func (h *fsWatchHost) release(resource *fsWatch) {
	h.mu.Lock()
	delete(h.resources, resource)
	h.mu.Unlock()
}

// open is called on the runtime owner. It resolves through the shared
// filesystem backend, so Root, Cwd, read-only observation, and symlink rules
// are identical to the rest of fs.
func (h *fsWatchHost) open(pathname string, persistent bool, maxQueue int, ignoreOverflow, bytes bool) (quickjs.Value, error) {
	if pathname == "" || strings.ContainsRune(pathname, 0) {
		return quickjs.Value{}, fileError(errors.New("watch path must be a nonempty string without NUL"), "ERR_INVALID_ARG")
	}
	if maxQueue < 1 {
		return quickjs.Value{}, fileError(errors.New("watch maxQueue must be a positive integer"), "ERR_INVALID_ARG")
	}
	h.mu.Lock()
	closed := h.closed || h.ctx.Err() != nil
	h.mu.Unlock()
	if closed {
		return quickjs.Value{}, fileError(os.ErrClosed, "ERR_FS_WATCH_CLOSED")
	}

	full, err := h.fs.resolve(pathname)
	if err != nil {
		return quickjs.Value{}, h.resolveError(pathname, err)
	}
	if _, err := os.Stat(full); err != nil {
		return quickjs.Value{}, h.pathError(pathname, full, "watch", err, "")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return quickjs.Value{}, fileError(err, filesystemErrorCode(err))
	}
	if err := watcher.Add(full); err != nil {
		_ = watcher.Close()
		return quickjs.Value{}, h.pathError(pathname, full, "watch", err, "")
	}

	resource := newFSWatch(h, watcher, pathname, maxQueue, ignoreOverflow, bytes, persistent)
	go resource.readEvents()
	if !h.own(resource) {
		resource.close(false)
		return quickjs.Value{}, fileError(os.ErrClosed, "ERR_FS_WATCH_CLOSED")
	}
	object := h.rt.NewObject()
	if err := errors.Join(
		object.Set("next", resource.next),
		object.Set("close", func() error { resource.close(true); return nil }),
		object.Set("ref", func() { resource.setRef(true) }),
		object.Set("unref", func() { resource.setRef(false) }),
	); err != nil {
		resource.close(false)
		return quickjs.Value{}, err
	}
	return object, nil
}

func (h *fsWatchHost) resolveError(requested string, cause error) error {
	// Backend policy errors can include host paths in their text. Keep the
	// original Go cause for errors.Is while exposing only the request and a
	// stable, script-facing description.
	message := "invalid or inaccessible path"
	if errors.Is(cause, fs.ErrNotExist) {
		message = fs.ErrNotExist.Error()
	} else if errors.Is(cause, fs.ErrPermission) {
		message = fs.ErrPermission.Error()
	}
	return h.pathErrorMessage(requested, h.safePath(requested), "watch", cause, filesystemErrorCode(cause), message)
}

func (h *fsWatchHost) pathError(requested, full, op string, cause error, code string) error {
	// fsnotify and the OS normally wrap path failures in PathError. Keep its
	// errno/cause but remove the physical pathname from the exposed cause chain.
	var pathErr *os.PathError
	if errors.As(cause, &pathErr) {
		cause = pathErr.Err
	}
	var linkErr *os.LinkError
	if errors.As(cause, &linkErr) {
		cause = linkErr.Err
	}
	message := ""
	if code == "ERR_FS_EVENT_OVERFLOW" {
		message = "filesystem watch event queue overflow"
	} else if errors.Is(cause, fsnotify.ErrEventOverflow) {
		code = "ERR_FS_EVENT_OVERFLOW"
		message = "filesystem event source overflow"
	}
	if code == "" {
		code = filesystemErrorCode(cause)
	}
	return h.pathErrorMessage(requested, full, op, cause, code, message)
}

func (h *fsWatchHost) pathErrorMessage(requested, pathForField, op string, cause error, code, message string) error {
	if message == "" {
		message = cause.Error()
	}
	pathErr := &fsWatchPathError{op: op, path: pathForField, input: requested, message: message, cause: cause}
	return fileError(pathErr, code)
}

// safePath supplies a physical-looking path under Root only so fsHost's
// existing error encoder can map it back to a virtual path. It never escapes
// the configured root, even when Resolve refused the request.
func (h *fsWatchHost) safePath(requested string) string {
	root := h.fs.backend.Root()
	if root == "" {
		return requested
	}
	virtual := strings.ReplaceAll(requested, "\\", "/")
	virtual = path.Clean("/" + strings.TrimLeft(virtual, "/"))
	return filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(virtual, "/")))
}

// fsWatchPathError exposes a safe message while allowing fsHost.errorValue to
// populate path/syscall from a synthetic PathError. Its unwrapped cause never
// contains the backend's physical pathname.
type fsWatchPathError struct {
	op      string
	path    string
	input   string
	message string
	cause   error
}

func (e *fsWatchPathError) Error() string {
	return fmt.Sprintf("%s %q: %s", e.op, e.input, e.message)
}

func (e *fsWatchPathError) Unwrap() error { return e.cause }

func (e *fsWatchPathError) As(target any) bool {
	pathError, ok := target.(**os.PathError)
	if !ok {
		return false
	}
	*pathError = &os.PathError{Op: e.op, Path: e.path, Err: e.cause}
	return true
}

type fsWatchEvent struct {
	typeName string
	filename []byte
}

type fsWatchWaiter struct {
	promise *quickjs.Promise
	event   *fsWatchEvent
	dropped int64
}

type fsWatch struct {
	host           *fsWatchHost
	watcher        *fsnotify.Watcher
	requestedPath  string
	maxQueue       int
	ignoreOverflow bool
	bytes          bool
	work           *quickjs.AsyncWork

	mu                sync.Mutex
	closed            bool
	terminalErr       error
	queue             []fsWatchEvent
	waiters           []*fsWatchWaiter
	inFlight          *fsWatchWaiter
	dropped           int64
	deliveryScheduled bool
	persistent        bool
	readerFinished    bool
	terminalSettled   bool
	watchCloseOnce    sync.Once
	readerDone        chan struct{}
	nativeCloseDone   chan struct{}
}

func newFSWatch(host *fsWatchHost, watcher *fsnotify.Watcher, requested string, maxQueue int, ignoreOverflow, bytes, persistent bool) *fsWatch {
	r := &fsWatch{
		host: host, watcher: watcher, requestedPath: requested, maxQueue: maxQueue,
		ignoreOverflow: ignoreOverflow, bytes: bytes, persistent: persistent,
		work: host.rt.StartAsyncWork(), readerDone: make(chan struct{}), nativeCloseDone: make(chan struct{}),
	}
	if !persistent {
		r.work.Unref()
	}
	return r
}

func (r *fsWatch) next() *quickjs.Promise {
	promise := r.host.rt.NewPromise()
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		_ = promise.Resolve(map[string]any{"done": true})
		return promise
	}
	if r.terminalErr != nil && len(r.waiters) == 0 && r.inFlight == nil && !r.deliveryScheduled {
		err := r.terminalErr
		r.mu.Unlock()
		r.host.fs.reject(promise, err)
		return promise
	}
	r.waiters = append(r.waiters, &fsWatchWaiter{promise: promise})
	r.assignQueuedLocked()
	post := r.scheduleLocked()
	r.mu.Unlock()
	if post {
		r.postDelivery()
	}
	return promise
}

func (r *fsWatch) resolveEvent(promise *quickjs.Promise, event fsWatchEvent, dropped int64) {
	var filename any
	if event.filename != nil {
		if r.bytes {
			filename = r.host.rt.NewBytes(event.filename)
		} else {
			filename = string(event.filename)
		}
	}
	value := map[string]any{"eventType": event.typeName, "filename": filename}
	result := map[string]any{"value": value, "done": false}
	if dropped > 0 {
		result["dropped"] = dropped
	}
	_ = promise.Resolve(result)
}

func (r *fsWatch) setRef(ref bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed || r.terminalErr != nil || r.persistent == ref {
		return
	}
	r.persistent = ref
	if ref {
		r.work.Ref()
	} else {
		r.work.Unref()
	}
}

func (r *fsWatch) close(explicit bool) {
	r.mu.Lock()
	if r.closed {
		done := r.nativeCloseDone
		r.mu.Unlock()
		<-done
		return
	}
	r.closed = true
	r.queue = nil
	waiters := make([]*fsWatchWaiter, 0, len(r.waiters)+1)
	if r.inFlight != nil {
		waiters = append(waiters, r.inFlight)
	}
	waiters = append(waiters, r.waiters...)
	r.waiters = nil
	if r.inFlight != nil {
		r.inFlight = nil
	}
	r.deliveryScheduled = false
	r.dropped = 0
	r.mu.Unlock()

	r.stopWatcher()
	<-r.readerDone
	r.host.release(r)
	r.work.Done()
	close(r.nativeCloseDone)

	if explicit {
		for _, waiter := range waiters {
			_ = waiter.promise.Resolve(map[string]any{"done": true})
		}
	}
}

func (r *fsWatch) stopWatcher() {
	r.watchCloseOnce.Do(func() { _ = r.watcher.Close() })
}

func (r *fsWatch) readEvents() {
	defer func() {
		close(r.readerDone)
		r.mu.Lock()
		r.readerFinished = true
		r.mu.Unlock()
		r.releaseTerminalIfReady()
	}()
	for {
		select {
		case <-r.host.ctx.Done():
			return
		case event, ok := <-r.watcher.Events:
			if !ok {
				r.fail(errors.New("filesystem event source closed"), "ERR_FS_EVENT")
				return
			}
			if normalized, ok := normalizeFSWatchEvent(event); ok {
				if !r.enqueue(normalized) {
					return
				}
			}
		case err, ok := <-r.watcher.Errors:
			if !ok {
				r.fail(errors.New("filesystem event error source closed"), "ERR_FS_EVENT")
				return
			}
			if err != nil {
				code := ""
				if errors.Is(err, fsnotify.ErrEventOverflow) {
					code = "ERR_FS_EVENT_OVERFLOW"
				}
				r.fail(r.host.pathError(r.requestedPath, r.watchPath(), "watch", err, code), "")
				return
			}
		}
	}
}

func (r *fsWatch) watchPath() string {
	full, err := r.host.fs.resolve(r.requestedPath)
	if err != nil {
		return r.host.safePath(r.requestedPath)
	}
	return full
}

func normalizeFSWatchEvent(event fsnotify.Event) (fsWatchEvent, bool) {
	var eventType string
	switch {
	case event.Op&(fsnotify.Create|fsnotify.Remove|fsnotify.Rename) != 0:
		eventType = "rename"
	case event.Op&(fsnotify.Write|fsnotify.Chmod) != 0:
		eventType = "change"
	default:
		return fsWatchEvent{}, false
	}
	var filename []byte
	if event.Name != "" {
		filename = []byte(filepath.Base(event.Name))
		filename = append([]byte(nil), filename...)
	}
	return fsWatchEvent{typeName: eventType, filename: filename}, true
}

func (r *fsWatch) enqueue(event fsWatchEvent) bool {
	r.mu.Lock()
	if r.closed || r.terminalErr != nil {
		r.mu.Unlock()
		return false
	}
	if r.assignEventLocked(event) {
		post := r.scheduleLocked()
		r.mu.Unlock()
		if post {
			r.postDelivery()
		}
		return true
	}
	if len(r.queue) < r.maxQueue {
		r.queue = append(r.queue, event)
		r.mu.Unlock()
		return true
	}
	if r.ignoreOverflow {
		if r.dropped < maxSafeFileInteger {
			r.dropped++
		}
		r.mu.Unlock()
		return true
	}
	err := r.host.pathError(r.requestedPath, r.watchPath(), "watch", errors.New("filesystem watch event queue overflow"), "ERR_FS_WATCH_QUEUE_OVERFLOW")
	r.terminalErr = err
	r.queue = nil
	r.dropped = 0
	post := r.scheduleLocked()
	noWaiters := len(r.waiters) == 0 && r.inFlight == nil && !r.deliveryScheduled
	r.mu.Unlock()
	r.stopWatcher()
	if post {
		r.postDelivery()
	} else if noWaiters {
		r.work.Done()
		r.mu.Lock()
		r.terminalSettled = true
		r.mu.Unlock()
		r.releaseTerminalIfReady()
	}
	return false
}

func (r *fsWatch) fail(err error, code string) {
	if code != "" {
		err = r.host.pathError(r.requestedPath, r.watchPath(), "watch", err, code)
	}
	r.mu.Lock()
	if r.closed || r.terminalErr != nil {
		r.mu.Unlock()
		return
	}
	r.terminalErr = err
	r.queue = nil
	r.dropped = 0
	post := r.scheduleLocked()
	noWaiters := len(r.waiters) == 0 && r.inFlight == nil && !r.deliveryScheduled
	r.mu.Unlock()
	r.stopWatcher()
	if post {
		r.postDelivery()
	} else if noWaiters {
		r.work.Done()
		r.mu.Lock()
		r.terminalSettled = true
		r.mu.Unlock()
		r.releaseTerminalIfReady()
	}
}

func (r *fsWatch) scheduleLocked() bool {
	if r.closed || r.deliveryScheduled {
		return false
	}
	ready := r.terminalErr != nil && len(r.waiters) > 0
	if !ready {
		for _, waiter := range r.waiters {
			if waiter.event == nil {
				break
			}
			ready = true
		}
	}
	if !ready {
		return false
	}
	r.deliveryScheduled = true
	return true
}

func (r *fsWatch) postDelivery() {
	r.work.Post(r.deliver)
}

// deliver runs only on QuickJS's owner. It keeps each current waiter visible
// to close while settling outside mu, because Promise resolution can invoke
// script through thenable assimilation.
func (r *fsWatch) deliver(workErr error) {
	r.mu.Lock()
	if workErr != nil {
		r.deliveryScheduled = false
		r.waiters = nil
		r.inFlight = nil
		r.queue = nil
		r.mu.Unlock()
		return
	}
	if r.host.ctx.Err() != nil {
		r.deliveryScheduled = false
		r.waiters = nil
		r.inFlight = nil
		r.queue = nil
		r.mu.Unlock()
		return
	}
	if r.closed {
		r.deliveryScheduled = false
		r.waiters = nil
		r.inFlight = nil
		r.queue = nil
		r.mu.Unlock()
		return
	}
	r.mu.Unlock()
	for {
		r.mu.Lock()
		if r.closed || r.host.ctx.Err() != nil {
			r.deliveryScheduled = false
			r.waiters = nil
			r.inFlight = nil
			r.queue = nil
			r.mu.Unlock()
			return
		}
		r.assignQueuedLocked()
		if len(r.waiters) == 0 {
			r.deliveryScheduled = false
			if r.terminalErr != nil {
				r.terminalSettled = true
			}
			terminal := r.terminalErr != nil
			work := r.work
			r.mu.Unlock()
			if terminal {
				work.Done()
				r.releaseTerminalIfReady()
			}
			return
		}
		waiter := r.waiters[0]
		if r.terminalErr == nil && waiter.event == nil {
			r.deliveryScheduled = false
			post := r.scheduleLocked()
			r.mu.Unlock()
			if post {
				r.postDelivery()
			}
			return
		}
		r.waiters[0] = nil
		r.waiters = r.waiters[1:]
		terminal := r.terminalErr
		r.inFlight = waiter
		r.mu.Unlock()

		if r.canSettle(waiter) {
			if terminal != nil {
				r.host.fs.reject(waiter.promise, terminal)
			} else {
				r.resolveEvent(waiter.promise, *waiter.event, waiter.dropped)
			}
		}

		r.mu.Lock()
		if r.inFlight == waiter {
			r.inFlight = nil
		}
		closed := r.closed || r.host.ctx.Err() != nil
		r.mu.Unlock()
		if closed {
			return
		}
	}
}

func (r *fsWatch) canSettle(waiter *fsWatchWaiter) bool {
	r.mu.Lock()
	allowed := !r.closed && r.inFlight == waiter && r.host.ctx.Err() == nil
	r.mu.Unlock()
	return allowed
}

func (r *fsWatch) assignQueuedLocked() {
	for len(r.queue) > 0 {
		if !r.assignEventLocked(r.queue[0]) {
			return
		}
		copy(r.queue, r.queue[1:])
		r.queue[len(r.queue)-1] = fsWatchEvent{}
		r.queue = r.queue[:len(r.queue)-1]
	}
}

func (r *fsWatch) assignEventLocked(event fsWatchEvent) bool {
	for _, waiter := range r.waiters {
		if waiter.event != nil {
			continue
		}
		copyEvent := event
		waiter.event = &copyEvent
		waiter.dropped = r.dropped
		r.dropped = 0
		return true
	}
	return false
}

func (r *fsWatch) releaseTerminalIfReady() {
	r.mu.Lock()
	ready := !r.closed && r.terminalErr != nil && r.terminalSettled && r.readerFinished && len(r.waiters) == 0
	r.mu.Unlock()
	if ready {
		r.host.release(r)
	}
}
