package stdlib

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"

	quickjs "github.com/go-quickjs/go-quickjs"
)

//go:embed filehandle.js
var fileHandleJS string

// fileHost owns handles and workers for one filesystem installation. It is
// independent of the event loop's liveness and never owns a borrowed VM.
type fileHost struct {
	rt          *quickjs.Runtime
	fs          *fsHost
	ctx         context.Context
	cancel      context.CancelFunc
	stopRuntime func() bool
	mu          sync.Mutex
	closed      bool
	resources   map[*fileHandle]struct{}
	wg          sync.WaitGroup
	closeDone   chan struct{}
}

func newFileHost(f *fsHost) (*fileHost, error) {
	parent := f.rt.Context()
	if f.cfg.Loop != nil {
		parent = loopContext(f.cfg.Loop)
	}
	ctx, cancel := context.WithCancel(parent)
	h := &fileHost{rt: f.rt, fs: f, ctx: ctx, cancel: cancel, resources: make(map[*fileHandle]struct{}), closeDone: make(chan struct{})}
	h.stopRuntime = context.AfterFunc(f.rt.Context(), cancel)
	f.rt.OnClose(h.Close)
	if scope := f.cfg.Loop.executionScope(); scope != nil {
		if err := scope.AddCleanup(h.Close); err != nil {
			h.Close()
			return nil, err
		}
	}
	return h, nil
}

func (h *fileHost) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		<-h.closeDone
		return
	}
	h.closed = true
	h.cancel()
	h.stopRuntime()
	files := make([]*fileHandle, 0, len(h.resources))
	for f := range h.resources {
		files = append(files, f)
	}
	h.mu.Unlock()
	for _, f := range files {
		_ = f.Close()
	}
	h.wg.Wait()
	close(h.closeDone)
}

func (h *fileHost) own(f *fileHandle) bool {
	h.mu.Lock()
	if h.closed || h.ctx.Err() != nil {
		h.mu.Unlock()
		_ = f.Close()
		return false
	}
	h.resources[f] = struct{}{}
	h.mu.Unlock()
	return true
}
func (h *fileHost) release(f *fileHandle) { h.mu.Lock(); delete(h.resources, f); h.mu.Unlock() }
func (h *fileHost) admitWorker() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.ctx.Err() != nil {
		return false
	}
	h.wg.Add(1)
	return true
}
func filePosition(value quickjs.Value) (int64, error) {
	if value.IsNull() || value.IsUndefined() {
		return -1, nil
	}
	return fileInteger(value, "position")
}

const maxSafeFileInteger = int64(1<<53 - 1)

type fileBackend interface {
	ReadAt([]byte, int64) (int, error)
	Read([]byte) (int, error)
	WriteAt([]byte, int64) (int, error)
	Write([]byte) (int, error)
	Close() error
}

func (h *fileHost) openFile(path, mode string) *quickjs.Promise {
	return h.openFileUsing(path, mode, openFileBackend)
}

func openFileBackend(path string, flags int, perm os.FileMode) (fileBackend, error) {
	return os.OpenFile(path, flags, perm)
}

func (h *fileHost) openFileUsing(path, mode string, open func(string, int, os.FileMode) (fileBackend, error)) *quickjs.Promise {
	if path == "" || strings.ContainsRune(path, 0) {
		return h.rejectedFilePromise("path must be a nonempty string without NUL", "ERR_INVALID_ARG")
	}
	flags := os.O_RDONLY
	switch mode {
	case "r":
	case "r+":
		flags = os.O_RDWR
	default:
		return h.rejectedFilePromise(`mode must be "r" or "r+"`, "ERR_INVALID_ARG")
	}
	if flags != os.O_RDONLY {
		if err := h.fs.backend.CheckWritable(); err != nil {
			return h.rejectedFilePromise(err.Error(), "ERR_READ_ONLY")
		}
	}
	resolved, err := h.fs.backend.Resolve(path, true)
	if err != nil {
		return h.rejectedFileErrorPromise(err)
	}
	return h.asyncFile(func() (any, error) {
		if err := h.ctx.Err(); err != nil {
			return nil, fileError(err, "ERR_FILE_CLOSED")
		}
		file, err := open(resolved, flags, 0)
		if err != nil {
			return nil, fileError(err, fileErrorCode(err))
		}
		f := newFileHandle(h, file)
		if err := h.ctx.Err(); err != nil {
			_ = f.Close()
			return nil, fileError(err, "ERR_FILE_CLOSED")
		}
		if !h.own(f) {
			return nil, fileError(os.ErrClosed, "ERR_FILE_CLOSED")
		}
		return f, nil
	}, func(value any) (any, error) {
		return value.(*fileHandle).object()
	}, func(value any) {
		if f, ok := value.(*fileHandle); ok && f != nil {
			_ = f.Close()
		}
	}, nil)
}

type fileHandle struct {
	h          *fileHost
	file       fileBackend
	mu         sync.Mutex
	closing    bool
	closed     bool
	closeOnce  sync.Once
	finishOnce sync.Once
	closeErr   error
	ops        sync.WaitGroup
}

func newFileHandle(h *fileHost, file fileBackend) *fileHandle {
	return &fileHandle{h: h, file: file}
}

func (f *fileHandle) object() (quickjs.Value, error) {
	o := f.h.rt.NewObject()
	if err := errors.Join(
		o.Set("read", f.read),
		o.Set("write", f.write),
		o.Set("close", f.closeValue),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

func (f *fileHandle) read(position, length quickjs.Value) *quickjs.Promise {
	offset, err := filePosition(position)
	if err != nil {
		return f.h.rejectedFilePromise(err.Error(), "ERR_INVALID_ARG")
	}
	count, err := fileInteger(length, "length")
	if err != nil || count > int64(int(^uint(0)>>1)) {
		if err == nil {
			err = errors.New("length is too large for this platform")
		}
		return f.h.rejectedFilePromise(err.Error(), "ERR_INVALID_ARG")
	}
	if offset > math.MaxInt64-count {
		return f.h.rejectedFilePromise("position plus length overflows", "ERR_INVALID_ARG")
	}
	file, err := f.beginIO()
	if err != nil {
		return f.h.rejectedFileErrorPromise(err)
	}
	return f.h.asyncFile(func() (any, error) {
		buf, allocErr := makeFileReadBuffer(int(count))
		if allocErr != nil {
			return nil, fileError(allocErr, "ERR_INVALID_ARG")
		}
		if count == 0 {
			return fileReadResult{data: []byte{}}, nil
		}
		var n int
		var readErr error
		if offset < 0 {
			n, readErr = file.Read(buf)
		} else {
			n, readErr = file.ReadAt(buf, offset)
		}
		if n < 0 {
			n = 0
		}
		if n > len(buf) {
			n = len(buf)
		}
		data := append([]byte(nil), buf[:n]...)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, &fileIOError{cause: readErr, code: fileErrorCode(readErr), data: data, bytesRead: n, hasBytesRead: true}
		}
		return fileReadResult{data: data, bytesRead: n}, nil
	}, func(value any) (any, error) {
		result := value.(fileReadResult)
		o := f.h.rt.NewObject()
		if err := errors.Join(
			o.Set("data", f.h.rt.NewBytes(result.data)),
			o.Set("bytesRead", result.bytesRead),
		); err != nil {
			return nil, err
		}
		return o, nil
	}, nil, f.ops.Done)
}

// makeFileReadBuffer converts only Go's recoverable make-slice range panic
// into an input error. Other panics remain visible to the runtime.
func makeFileReadBuffer(length int) (buffer []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			panicErr, ok := recovered.(runtime.Error)
			if !ok || !strings.HasSuffix(panicErr.Error(), "makeslice: len out of range") {
				panic(recovered)
			}
			buffer = nil
			err = errors.New("length is too large for a read buffer")
		}
	}()
	return make([]byte, length), nil
}

func (f *fileHandle) write(position, input quickjs.Value) *quickjs.Promise {
	offset, err := filePosition(position)
	if err != nil {
		return f.h.rejectedFilePromise(err.Error(), "ERR_INVALID_ARG")
	}
	bytes, ok := input.Bytes()
	if input.IsString() {
		bytes, ok = []byte(input.String()), true
	}
	if !ok {
		return f.h.rejectedFilePromise("bytes must be a Uint8Array", "ERR_INVALID_ARG")
	}
	data := append([]byte(nil), bytes...)
	if int64(len(data)) > maxSafeFileInteger || offset > math.MaxInt64-int64(len(data)) {
		return f.h.rejectedFilePromise("position plus byte length overflows", "ERR_INVALID_ARG")
	}
	file, err := f.beginIO()
	if err != nil {
		return f.h.rejectedFileErrorPromise(err)
	}
	return f.h.asyncFile(func() (any, error) {
		var n int
		var writeErr error
		if offset < 0 {
			n, writeErr = file.Write(data)
		} else {
			n, writeErr = file.WriteAt(data, offset)
		}
		if n < 0 {
			n = 0
		}
		if n > len(data) {
			n = len(data)
		}
		if writeErr == nil && n != len(data) {
			writeErr = io.ErrShortWrite
		}
		if writeErr != nil {
			return nil, &fileIOError{cause: writeErr, code: fileErrorCode(writeErr), bytesWritten: n, hasBytesWritten: true}
		}
		return n, nil
	}, nil, nil, f.ops.Done)
}

func (f *fileHandle) closeValue() *quickjs.Promise {
	f.beginClose()
	return f.h.asyncFile(func() (any, error) { return nil, f.finishClose(false) }, nil, nil, nil)
}

func (f *fileHandle) beginIO() (fileBackend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closing {
		return nil, fileError(os.ErrClosed, "ERR_FILE_CLOSED")
	}
	f.ops.Add(1)
	return f.file, nil
}

// Close is the Go-only shutdown path. Close the OS resource before joining
// operations so cancellation can unblock an outstanding read or write.
func (f *fileHandle) Close() error {
	f.beginClose()
	return f.finishClose(true)
}

func (f *fileHandle) beginClose() {
	f.mu.Lock()
	f.closing = true
	f.mu.Unlock()
}

func (f *fileHandle) closeFile() {
	f.closeOnce.Do(func() { f.closeErr = f.file.Close() })
}

func (f *fileHandle) finishClose(force bool) error {
	if force {
		f.closeFile()
	}
	f.ops.Wait()
	f.closeFile()
	f.finishOnce.Do(func() {
		f.mu.Lock()
		f.closed = true
		f.mu.Unlock()
		f.h.release(f)
	})
	return f.closeErr
}

type fileReadResult struct {
	data      []byte
	bytesRead int
}

type fileIOError struct {
	cause           error
	code            any
	data            []byte
	bytesRead       int
	hasBytesRead    bool
	bytesWritten    int
	hasBytesWritten bool
}

func (e *fileIOError) Error() string { return e.cause.Error() }
func (e *fileIOError) Unwrap() error { return e.cause }

func fileError(err error, code any) error {
	if err == nil {
		return nil
	}
	return &fileIOError{cause: err, code: code}
}

func fileErrorCode(err error) any {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return int64(errno)
	}
	if errors.Is(err, os.ErrClosed) {
		return "ERR_FILE_CLOSED"
	}
	if errors.Is(err, io.ErrShortWrite) {
		return "ERR_IO"
	}
	return "ERR_IO"
}

func fileInteger(value quickjs.Value, name string) (int64, error) {
	if value.Kind() != quickjs.KindNumber {
		return 0, fmt.Errorf("%s must be a nonnegative safe integer", name)
	}
	number := value.Float()
	if math.IsNaN(number) || math.IsInf(number, 0) || number < 0 || math.Trunc(number) != number || number > float64(maxSafeFileInteger) {
		return 0, fmt.Errorf("%s must be a nonnegative safe integer", name)
	}
	return int64(number), nil
}

func (h *fileHost) asyncFile(run func() (any, error), convert func(any) (any, error), drop func(any), finished func()) *quickjs.Promise {
	p := h.rt.NewPromise()
	if !h.admitWorker() {
		if finished != nil {
			finished()
		}
		err := h.ctx.Err()
		if err == nil {
			err = os.ErrClosed
		}
		_ = p.Reject(h.fileErrorValue(err))
		return p
	}
	w := h.rt.StartAsyncWork()
	go func() {
		defer h.wg.Done()
		value, err := func() (any, error) {
			if finished != nil {
				defer finished()
			}
			return run()
		}()
		w.Complete(func(closed error) {
			if closed != nil || h.ctx.Err() != nil {
				if drop != nil {
					drop(value)
				}
				return
			}
			if err != nil {
				_ = p.Reject(h.fileErrorValue(err))
				return
			}
			original := value
			if convert != nil {
				value, err = convert(original)
				if err != nil {
					if drop != nil {
						drop(original)
					}
					p.RejectError(err)
					return
				}
			}
			if err := p.Resolve(value); err != nil {
				if drop != nil {
					drop(original)
				}
				p.RejectError(err)
			}
		})
	}()
	return p
}

func (h *fileHost) rejectedFilePromise(message string, code any) *quickjs.Promise {
	return h.rejectedFileErrorPromise(&fileIOError{cause: errors.New(message), code: code})
}

func (h *fileHost) rejectedFileErrorPromise(err error) *quickjs.Promise {
	p := h.rt.NewPromise()
	_ = p.Reject(h.fileErrorValue(err))
	return p
}

func (h *fileHost) fileErrorValue(err error) quickjs.Value {
	value := h.rt.NewError("Error", err.Error())
	var detail *fileIOError
	if errors.As(err, &detail) {
		if detail.code != nil {
			_ = value.Set("code", detail.code)
		}
		if detail.hasBytesRead {
			_ = value.Set("data", h.rt.NewBytes(detail.data))
			_ = value.Set("bytesRead", detail.bytesRead)
		}
		if detail.hasBytesWritten {
			_ = value.Set("bytesWritten", detail.bytesWritten)
		}
	} else if err != nil {
		_ = value.Set("code", fileErrorCode(err))
	}
	return value
}
