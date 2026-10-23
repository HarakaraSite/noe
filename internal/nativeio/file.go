package nativeio

import (
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

//go:embed js/file.js
var fileSource string

const maxSafeFileInteger = int64(1<<53 - 1)

type fileBackend interface {
	ReadAt([]byte, int64) (int, error)
	WriteAt([]byte, int64) (int, error)
	Close() error
}

// installFS registers the runtime's positional file API. Open handles are
// host-owned resources, but do not create AsyncWork until an operation starts.
func (h *Host) installFS() error {
	factory, err := h.rt.EvalFile("<noe-fs>", fileSource)
	if err != nil {
		return err
	}
	native := h.rt.NewObject()
	if err := native.Set("open", h.openFile); err != nil {
		return err
	}
	api, err := factory.Call(native)
	if err != nil {
		return err
	}
	open, err := api.Get("open")
	if err != nil {
		return err
	}
	return h.rt.SetModuleValues("noe:fs-handle", map[string]quickjs.Value{"open": open})
}

func (h *Host) openFile(path, mode string) *quickjs.Promise {
	return h.openFileUsing(path, mode, openFileBackend)
}

func openFileBackend(path string, flags int, perm os.FileMode) (fileBackend, error) {
	return os.OpenFile(path, flags, perm)
}

func (h *Host) openFileUsing(path, mode string, open func(string, int, os.FileMode) (fileBackend, error)) *quickjs.Promise {
	if path == "" || containsNUL(path) {
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
	if flags != os.O_RDONLY && h.opts.FileSystem != nil {
		if err := h.opts.FileSystem.CheckWritable(); err != nil {
			return h.rejectedFilePromise(err.Error(), "ERR_READ_ONLY")
		}
	}
	resolved, err := h.resolveFilePath(path)
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
	})
}

func (h *Host) resolveFilePath(path string) (string, error) {
	if h.opts.FileSystem != nil {
		resolved, err := h.opts.FileSystem.Resolve(path, true)
		if err != nil {
			return "", fileError(err, fileErrorCode(err))
		}
		return resolved, nil
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	cwd := h.opts.Cwd
	if cwd == "" {
		var err error
		cwd, err = os.Getwd()
		if err != nil {
			return "", fileError(err, fileErrorCode(err))
		}
	}
	resolved, err := filepath.Abs(filepath.Join(cwd, path))
	if err != nil {
		return "", fileError(err, fileErrorCode(err))
	}
	return resolved, nil
}

type fileHandle struct {
	h        *Host
	file     fileBackend
	mu       sync.Mutex
	closing  bool
	closed   bool
	closeCh  chan struct{}
	closeErr error
	ops      sync.WaitGroup
}

func newFileHandle(h *Host, file fileBackend) *fileHandle {
	return &fileHandle{h: h, file: file, closeCh: make(chan struct{})}
}

func (f *fileHandle) object() (quickjs.Value, error) {
	o := f.h.rt.NewObject()
	if err := errors.Join(
		o.Set("readAt", f.readAt),
		o.Set("writeAt", f.writeAt),
		o.Set("close", f.closeValue),
	); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

func (f *fileHandle) readAt(position, length quickjs.Value) *quickjs.Promise {
	offset, err := fileInteger(position, "position")
	if err != nil {
		return f.h.rejectedFileErrorPromise(err)
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
		defer f.ops.Done()
		buf, allocErr := makeFileReadBuffer(int(count))
		if allocErr != nil {
			return nil, fileError(allocErr, "ERR_INVALID_ARG")
		}
		if count == 0 {
			return fileReadResult{data: []byte{}}, nil
		}
		n, readErr := file.ReadAt(buf, offset)
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
		return fileReadResult{data: data, bytesRead: n, eof: errors.Is(readErr, io.EOF)}, nil
	}, func(value any) (any, error) {
		result := value.(fileReadResult)
		o := f.h.rt.NewObject()
		if err := errors.Join(
			o.Set("data", f.h.rt.NewBytes(result.data)),
			o.Set("bytesRead", result.bytesRead),
			o.Set("eof", result.eof),
		); err != nil {
			return nil, err
		}
		return o, nil
	}, nil)
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

func (f *fileHandle) writeAt(position, input quickjs.Value) *quickjs.Promise {
	offset, err := fileInteger(position, "position")
	if err != nil {
		return f.h.rejectedFileErrorPromise(err)
	}
	bytes, ok := input.Bytes()
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
		defer f.ops.Done()
		n, writeErr := file.WriteAt(data, offset)
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
	}, nil, nil)
}

func (f *fileHandle) closeValue() *quickjs.Promise {
	started := f.beginClose()
	return f.h.asyncFile(func() (any, error) { return nil, f.finishClose(started) }, nil, nil)
}

func (f *fileHandle) beginIO() (fileBackend, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closing || f.closed {
		return nil, fileError(os.ErrClosed, "ERR_FILE_CLOSED")
	}
	f.ops.Add(1)
	return f.file, nil
}

func (f *fileHandle) Close() error {
	return f.finishClose(f.beginClose())
}

// beginClose is called on the owner for JS close() so it rejects later I/O
// calls immediately, before the asynchronous OS close work is scheduled.
func (f *fileHandle) beginClose() bool {
	f.mu.Lock()
	if f.closing || f.closed {
		f.mu.Unlock()
		return false
	}
	f.closing = true
	f.mu.Unlock()
	return true
}

func (f *fileHandle) finishClose(started bool) error {
	if !started {
		<-f.closeCh
		f.mu.Lock()
		err := f.closeErr
		f.mu.Unlock()
		return err
	}
	f.mu.Lock()
	file := f.file
	f.mu.Unlock()

	err := file.Close()
	if err != nil {
		err = fileError(err, fileErrorCode(err))
	}
	f.ops.Wait()

	f.mu.Lock()
	f.closed = true
	f.closeErr = err
	close(f.closeCh)
	f.mu.Unlock()
	f.h.release(f)
	return err
}

type fileReadResult struct {
	data      []byte
	bytesRead int
	eof       bool
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

func (h *Host) asyncFile(run func() (any, error), convert func(any) (any, error), drop func(any)) *quickjs.Promise {
	p := h.rt.NewPromise()
	if !h.admitWorker() {
		p.RejectError(h.operationError())
		return p
	}
	w := h.rt.StartAsyncWork()
	go func() {
		defer h.wg.Done()
		value, err := run()
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
			if convert != nil {
				original := value
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
				p.RejectError(err)
			}
		})
	}()
	return p
}

func (h *Host) rejectedFilePromise(message string, code any) *quickjs.Promise {
	return h.rejectedFileErrorPromise(&fileIOError{cause: errors.New(message), code: code})
}

func (h *Host) rejectedFileErrorPromise(err error) *quickjs.Promise {
	p := h.rt.NewPromise()
	_ = p.Reject(h.fileErrorValue(err))
	return p
}

func (h *Host) fileErrorValue(err error) quickjs.Value {
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

func containsNUL(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] == 0 {
			return true
		}
	}
	return false
}
