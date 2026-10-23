package stdlib

import (
	"context"
	"errors"
	"io"
	"sync"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

// uploadBody bridges one owner-side stream reader to the transport. Only plain
// Go bytes cross the pipe; a write acknowledges consumption by the transport,
// not by the remote application. There is at most one owner-side write pending.
type uploadBody struct {
	reader *io.PipeReader
	writer *io.PipeWriter
	mu     sync.Mutex
	ended  bool
	// sourceErr distinguishes a producer failure from an intentional stop and
	// preserves its cause if the transport reports only context cancellation.
	sourceErr error
	notify    func()
	cancel    context.CancelFunc

	// busy belongs to the runtime's goroutine, unlike the fields above.
	busy bool
}

func newUploadBody(rt *quickjs.Runtime, loop *Loop, ctx context.Context, cancel context.CancelFunc) (*uploadBody, quickjs.Value, error) {
	r, w := io.Pipe()
	stopped := rt.NewPromise()
	u := &uploadBody{reader: r, writer: w, cancel: cancel}
	u.notify = func() { loop.postExecution(func() { stopped.Resolve(nil) }) }
	context.AfterFunc(ctx, func() { u.abort(ctx.Err()) })
	o := rt.NewObject()
	err := setAll(o, map[string]any{
		"stopped": stopped.Value(),
		"write": func(v quickjs.Value) *quickjs.Promise {
			p := rt.NewPromise()
			if u.isEnded() {
				p.Resolve(false)
				return p
			}
			if u.busy {
				p.RejectError(errors.New("an upload write is already pending"))
				return p
			}
			b, ok := v.Bytes()
			if !ok {
				p.RejectError(errors.New("an upload chunk must be a Uint8Array"))
				return p
			}
			// Bytes may refer to engine memory. Copy on the owner before IO.
			chunk := append([]byte(nil), b...)
			u.busy = true
			loop.Begin()
			if err := loop.Go(func(context.Context) {
				defer loop.Done()
				_, err := u.writer.Write(chunk)
				loop.postExecution(func() {
					u.busy = false
					if u.isEnded() {
						p.Resolve(false)
					} else if err != nil {
						p.RejectError(err)
					} else {
						p.Resolve(true)
					}
				})
			}); err != nil {
				loop.Done()
				u.busy = false
				p.RejectError(err)
			}
			return p
		},
		"close": func() { u.stop() },
		"fail":  func(message string) { u.fail(errors.New(message)) },
	})
	if err != nil {
		u.abort(err)
		cancel()
		return nil, quickjs.Value{}, err
	}
	return u, o, nil
}

func (u *uploadBody) Read(b []byte) (int, error) { return u.reader.Read(b) }

// Close is the transport's decision to stop reading this request body. It may
// be called concurrently, including while a redirect is starting another hop.
func (u *uploadBody) Close() error {
	u.stop()
	return u.reader.Close()
}

func (u *uploadBody) isEnded() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.ended
}

func (u *uploadBody) failure() error {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.sourceErr
}

// stop deliberately ends just the upload. The transport must see EOF, not an
// ErrClosedPipe that would tear down an early response on the same connection.
// The reader is left for the transport to close; it owns Request.Body.
func (u *uploadBody) stop() {
	u.mu.Lock()
	if u.ended {
		u.mu.Unlock()
		return
	}
	u.ended = true
	u.writer.Close()
	u.mu.Unlock()
	u.notify()
}

// fail races with an intentional stop: a late result from a cancelled producer
// must not turn a successful early response into a failed request.
func (u *uploadBody) fail(err error) {
	u.mu.Lock()
	if u.ended {
		u.mu.Unlock()
		return
	}
	u.ended = true
	u.sourceErr = err
	u.writer.CloseWithError(err)
	u.reader.CloseWithError(err)
	u.mu.Unlock()
	u.cancel()
	u.notify()
}

// abort releases Go IO independently of whether the runtime can still run a
// posted callback. A response that has ended can also close an EOF'd upload.
func (u *uploadBody) abort(err error) {
	u.mu.Lock()
	first := !u.ended
	u.ended = true
	u.writer.CloseWithError(err)
	u.reader.CloseWithError(err)
	u.mu.Unlock()
	if first {
		u.notify()
	}
}
