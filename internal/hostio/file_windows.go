//go:build windows

package hostio

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

var cancelSynchronousIO = windows.NewLazySystemDLL("kernel32.dll").NewProc("CancelSynchronousIo")

// beforeFileIOCall is a test seam for the interval after publishing the
// operation thread and before entering ReadFile/WriteFile. Cancellation must
// remain effective across that interval.
var beforeFileIOCall func()

type fileReader interface {
	ReadContext(context.Context, []byte) (int, error)
	Close() error
}

type fileWriter interface {
	WriteContext(context.Context, []byte) (int, error)
	Close() error
}

type windowsFile struct {
	handle windows.Handle
	close  sync.Once
	err    error
}

func newFileReader(source *os.File) (fileReader, error) {
	handle, err := duplicateFile(source)
	if err != nil {
		return nil, err
	}
	return &windowsFile{handle: handle}, nil
}

func newFileWriter(destination *os.File) (fileWriter, error) {
	handle, err := duplicateFile(destination)
	if err != nil {
		return nil, err
	}
	return &windowsFile{handle: handle}, nil
}

func duplicateFile(source *os.File) (windows.Handle, error) {
	if source == nil {
		return windows.InvalidHandle, errors.New("nil host file")
	}
	raw, err := source.SyscallConn()
	if err != nil {
		return windows.InvalidHandle, err
	}
	var duplicate windows.Handle
	var dupErr error
	err = raw.Control(func(handle uintptr) {
		dupErr = windows.DuplicateHandle(
			windows.CurrentProcess(), windows.Handle(handle), windows.CurrentProcess(),
			&duplicate, 0, false, windows.DUPLICATE_SAME_ACCESS,
		)
	})
	if err != nil {
		return windows.InvalidHandle, err
	}
	if dupErr != nil {
		return windows.InvalidHandle, dupErr
	}
	return duplicate, nil
}

func (f *windowsFile) ReadContext(ctx context.Context, p []byte) (int, error) {
	n, err := f.ioContext(ctx, p, false)
	if errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_HANDLE_EOF) {
		return 0, io.EOF
	}
	return n, err
}

func (f *windowsFile) WriteContext(ctx context.Context, p []byte) (int, error) {
	return f.ioContext(ctx, p, true)
}

type ioResult struct {
	n   int
	err error
}

func (f *windowsFile) ioContext(ctx context.Context, p []byte, write bool) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	resultCh := make(chan ioResult, 1)
	threadCh := make(chan windows.Handle, 1)
	releaseWorker := make(chan struct{})
	workerDone := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer close(workerDone)
		defer runtime.UnlockOSThread()
		thread, err := duplicateCurrentThread()
		if err != nil {
			threadCh <- windows.InvalidHandle
			resultCh <- ioResult{err: err}
			<-releaseWorker
			return
		}
		threadCh <- thread
		if beforeFileIOCall != nil {
			beforeFileIOCall()
		}
		var count uint32
		if write {
			err = windows.WriteFile(f.handle, p, &count, nil)
		} else {
			err = windows.ReadFile(f.handle, p, &count, nil)
		}
		runtime.KeepAlive(p)
		resultCh <- ioResult{n: int(count), err: err}
		// Keep this OS thread reserved until the caller acknowledges that it
		// has stopped issuing CancelSynchronousIo against the duplicated handle.
		<-releaseWorker
	}()

	thread := <-threadCh
	defer func() {
		close(releaseWorker)
		<-workerDone
		if thread != windows.InvalidHandle {
			_ = windows.CloseHandle(thread)
		}
	}()
	if thread == windows.InvalidHandle {
		return resultFrom(resultCh)
	}
	select {
	case result := <-resultCh:
		return result.n, result.err
	case <-ctx.Done():
		for {
			// CancelSynchronousIo can report ERROR_NOT_FOUND when the worker
			// thread has not entered ReadFile/WriteFile yet. Keep retrying until
			// the syscall completes or cancellation reaches the pending syscall.
			select {
			case result := <-resultCh:
				return cancelledResult(ctx, result)
			default:
			}
			err := cancelSynchronousIOCall(thread)
			if err == nil {
				return cancelledResult(ctx, <-resultCh)
			}
			if !errors.Is(err, windows.ERROR_NOT_FOUND) {
				// Keep waiting for the native call to finish before returning, so
				// the adapter never leaves a background operation using p.
				result := <-resultCh
				if result.n > 0 {
					return result.n, ctx.Err()
				}
				return 0, err
			}
			timer := time.NewTimer(time.Millisecond)
			select {
			case result := <-resultCh:
				timer.Stop()
				return cancelledResult(ctx, result)
			case <-timer.C:
			}
		}
	}
}

func cancelledResult(ctx context.Context, result ioResult) (int, error) {
	if result.n > 0 {
		return result.n, ctx.Err()
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return result.n, result.err
}

func resultFrom(resultCh <-chan ioResult) (int, error) {
	result := <-resultCh
	return result.n, result.err
}

func duplicateCurrentThread() (windows.Handle, error) {
	var thread windows.Handle
	err := windows.DuplicateHandle(
		windows.CurrentProcess(), windows.CurrentThread(), windows.CurrentProcess(),
		&thread, 0, false, windows.DUPLICATE_SAME_ACCESS,
	)
	return thread, err
}

func cancelSynchronousIOCall(thread windows.Handle) error {
	if err := cancelSynchronousIO.Find(); err != nil {
		return err
	}
	result, _, callErr := cancelSynchronousIO.Call(uintptr(thread))
	if result != 0 {
		return nil
	}
	if callErr != windows.ERROR_SUCCESS {
		return callErr
	}
	return windows.ERROR_NOT_FOUND
}

func (f *windowsFile) Close() error {
	f.close.Do(func() { f.err = windows.CloseHandle(f.handle) })
	return f.err
}
