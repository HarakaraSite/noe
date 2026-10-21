package jsexec

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/hostio"
)

// executionIO adapts borrowed streams once, before installing any consumer.
// Even ordinary console/process writes use the execution's cancellable path.
type executionIO struct {
	ctx      context.Context
	fail     func(error)
	reader   hostio.Reader
	writers  []*executionWriter
	mu       sync.Mutex
	err      error
	once     sync.Once
	closeErr error
}

type executionReader struct{ owner *executionIO }
type executionWriter struct {
	owner *executionIO
	sink  *hostio.SharedWriter
}

func bindIO(ctx context.Context, env Environment, fail func(error)) (Environment, *executionIO, error) {
	input, err := hostio.NewReader(env.Stdin)
	if err != nil {
		return Environment{}, nil, err
	}
	owned := &executionIO{ctx: ctx, fail: fail, reader: input}
	stdout := &executionWriter{owner: owned, sink: hostio.NewSharedWriter(env.Stdout)}
	owned.writers = append(owned.writers, stdout)
	stderr := stdout
	a, b := reflect.ValueOf(env.Stdout), reflect.ValueOf(env.Stderr)
	same := a.IsValid() && b.IsValid() && a.Comparable() && b.Comparable() && env.Stdout == env.Stderr
	if !same {
		stderr = &executionWriter{owner: owned, sink: hostio.NewSharedWriter(env.Stderr)}
		owned.writers = append(owned.writers, stderr)
	}
	env.Stdin, env.Stdout, env.Stderr = &executionReader{owner: owned}, stdout, stderr
	return env, owned, nil
}

func (o *executionIO) record(ctx context.Context, err error) {
	if err == nil || errors.Is(err, io.EOF) {
		return
	}
	// Cancelling a particular stream operation is handled by that operation.
	// Ending the execution during ordinary cleanup is also expected.
	if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		return
	}
	o.mu.Lock()
	o.err = errors.Join(o.err, err)
	o.mu.Unlock()
	if o.fail != nil {
		o.fail(err)
	}
}
func (o *executionIO) Err() error { o.mu.Lock(); defer o.mu.Unlock(); return o.err }
func (o *executionIO) Close() error {
	o.once.Do(func() {
		o.closeErr = o.reader.Close()
		for _, writer := range o.writers {
			o.closeErr = errors.Join(o.closeErr, writer.sink.Close())
		}
	})
	return o.closeErr
}

func ioContext(caller, execution context.Context) (context.Context, func()) {
	if caller == nil {
		caller = context.Background()
	}
	ctx, cancel := context.WithCancel(caller)
	stop := context.AfterFunc(execution, cancel)
	if execution.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
func (r *executionReader) Read(p []byte) (int, error) { return r.ReadContext(r.owner.ctx, p) }
func (r *executionReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	ctx, stop := ioContext(ctx, r.owner.ctx)
	defer stop()
	n, err := r.owner.reader.ReadContext(ctx, p)
	r.owner.record(ctx, err)
	return n, err
}
func (w *executionWriter) Write(p []byte) (int, error) { return w.WriteContext(w.owner.ctx, p) }
func (w *executionWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	ctx, stop := ioContext(ctx, w.owner.ctx)
	defer stop()
	n, err := w.sink.WriteContext(ctx, p)
	w.owner.record(ctx, err)
	return n, err
}
