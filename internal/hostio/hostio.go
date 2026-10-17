// Package hostio adapts borrowed host I/O for operations that need explicit
// cancellation. Closing an adapter never closes the io.Reader or io.Writer it
// was given.
package hostio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
)

var (
	// ErrClosed is returned after an adapter has been closed.
	ErrClosed = errors.New("host I/O adapter is closed")
	// ErrUnsupported means the supplied value has no cancellation-aware adapter.
	ErrUnsupported = errors.New("host I/O value does not support cancellable operations")
)

// ContextReader is an explicit opt-in for readers that can stop an in-flight
// read when ctx is canceled. Implementations must not close a borrowed source
// merely because one read was canceled.
type ContextReader interface {
	ReadContext(context.Context, []byte) (int, error)
}

// ContextWriter is an explicit opt-in for writers that can stop an in-flight
// write when ctx is canceled.
type ContextWriter interface {
	WriteContext(context.Context, []byte) (int, error)
}

// Reader is a borrowed reader with an explicitly cancellable read operation.
type Reader interface {
	ReadContext(context.Context, []byte) (int, error)
	Close() error
}

// Writer is a borrowed writer with an explicitly cancellable write operation.
type Writer interface {
	WriteContext(context.Context, []byte) (int, error)
	Close() error
}

// BorrowedReader serializes reads and keeps its source open when closed.
// Native *os.File support is initialized on the first ReadContext call so
// runtimes that never use the cancellable stream need not adapt their stdin.
type BorrowedReader struct {
	source io.Reader
	gate   chan struct{}
	mu     sync.Mutex
	closed bool
	ctx    context.Context
	cancel context.CancelFunc
	close  sync.Once
	done   chan struct{}
	err    error
	file   fileReader
}

// NewReader creates a lazy adapter over source. A nil source is treated as an
// empty input stream.
func NewReader(source io.Reader) (Reader, error) {
	if source == nil {
		source = strings.NewReader("")
	}
	ctx, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &BorrowedReader{source: source, gate: gate, ctx: ctx, cancel: cancel, done: make(chan struct{})}, nil
}

// ReadContext reads from the borrowed source. It does not start a background
// read, so cancellation cannot strand an operation goroutine.
func (r *BorrowedReader) ReadContext(ctx context.Context, p []byte) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, stop := combine(ctx, r.ctx)
	defer stop()
	select {
	case <-opCtx.Done():
		return 0, opCtx.Err()
	case <-r.gate:
	}
	defer func() { r.gate <- struct{}{} }()
	if err := r.checkOpen(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if source, ok := r.source.(ContextReader); ok {
		return source.ReadContext(opCtx, p)
	}
	if supportedMemoryReader(r.source) {
		return r.source.Read(p)
	}
	if r.file == nil {
		file, ok := r.source.(*os.File)
		if !ok {
			return 0, ErrUnsupported
		}
		adapted, err := newFileReader(file)
		if err != nil {
			return 0, err
		}
		r.file = adapted
	}
	return r.file.ReadContext(opCtx, p)
}

// Close cancels any active read and releases adapter-owned resources without
// closing the original source.
func (r *BorrowedReader) Close() error {
	r.close.Do(func() {
		r.mu.Lock()
		r.closed = true
		r.cancel()
		r.mu.Unlock()
		<-r.gate
		if r.file != nil {
			r.err = r.file.Close()
		}
		r.gate <- struct{}{}
		close(r.done)
	})
	<-r.done
	return r.err
}

func (r *BorrowedReader) checkOpen() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrClosed
	}
	return nil
}

// SharedWriter serializes ordinary writes and cancellable writes through one
// borrowed destination. Its WriteContext method initializes file support only
// when first needed; ordinary Write preserves the supplied writer's behavior.
type SharedWriter struct {
	destination io.Writer
	gate        chan struct{}
	mu          sync.Mutex
	closed      bool
	ctx         context.Context
	cancel      context.CancelFunc
	close       sync.Once
	done        chan struct{}
	err         error
	file        fileWriter
}

// NewSharedWriter creates a lazily adapted, serialized writer. A nil writer is
// treated as io.Discard.
func NewSharedWriter(destination io.Writer) *SharedWriter {
	if destination == nil {
		destination = io.Discard
	}
	ctx, cancel := context.WithCancel(context.Background())
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &SharedWriter{destination: destination, gate: gate, ctx: ctx, cancel: cancel, done: make(chan struct{})}
}

// NewWriter creates a serialized borrowed writer. Runtimes that need one
// writer shared by multiple subsystems should create it once with
// NewSharedWriter and pass that same value to each subsystem.
func NewWriter(destination io.Writer) (Writer, error) {
	return NewSharedWriter(destination), nil
}

// Write serializes a legacy write with every context-aware write.
func (w *SharedWriter) Write(p []byte) (int, error) {
	select {
	case <-w.ctx.Done():
		return 0, ErrClosed
	case <-w.gate:
	}
	defer func() { w.gate <- struct{}{} }()
	if err := w.checkOpen(); err != nil {
		return 0, err
	}
	return w.destination.Write(p)
}

// WriteContext writes p fully unless the destination returns an error or ctx
// is canceled. A context-aware custom writer must implement ContextWriter;
// arbitrary blocking io.Writer values are not run in hidden goroutines.
func (w *SharedWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	opCtx, stop := combine(ctx, w.ctx)
	defer stop()
	select {
	case <-opCtx.Done():
		return 0, opCtx.Err()
	case <-w.gate:
	}
	defer func() { w.gate <- struct{}{} }()
	if err := w.checkOpen(); err != nil {
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if destination, ok := w.destination.(ContextWriter); ok {
		return writeAll(opCtx, destination, p)
	}
	if supportedMemoryWriter(w.destination) {
		return writeAll(opCtx, directWriter{w.destination}, p)
	}
	if w.file == nil {
		file, ok := w.destination.(*os.File)
		if !ok {
			return 0, ErrUnsupported
		}
		adapted, err := newFileWriter(file)
		if err != nil {
			return 0, err
		}
		w.file = adapted
	}
	return writeAll(opCtx, w.file, p)
}

// Close cancels in-flight writes and releases adapter-owned resources without
// closing the original destination.
func (w *SharedWriter) Close() error {
	w.close.Do(func() {
		w.mu.Lock()
		w.closed = true
		w.cancel()
		w.mu.Unlock()
		<-w.gate
		if w.file != nil {
			w.err = w.file.Close()
		}
		w.gate <- struct{}{}
		close(w.done)
	})
	<-w.done
	return w.err
}

func (w *SharedWriter) checkOpen() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return ErrClosed
	}
	return nil
}

type directWriter struct{ io.Writer }

func (w directWriter) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return w.Write(p)
}

func writeAll(ctx context.Context, w ContextWriter, p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, err := w.WriteContext(ctx, p)
		if n < 0 || n > len(p) {
			return written, errors.New("host I/O writer returned an invalid count")
		}
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
		if n == 0 {
			return written, io.ErrShortWrite
		}
	}
	return written, nil
}

func combine(parent, lifetime context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	stop := context.AfterFunc(lifetime, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

func supportedMemoryReader(source io.Reader) bool {
	switch source.(type) {
	case *bytes.Buffer, *bytes.Reader, *strings.Reader:
		return true
	default:
		return false
	}
}

func supportedMemoryWriter(destination io.Writer) bool {
	switch destination.(type) {
	case *bytes.Buffer, *strings.Builder:
		return true
	default:
		return destination == io.Discard
	}
}
