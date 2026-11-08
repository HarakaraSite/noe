package stdlib

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

var errInflatedLimit = errors.New("the decompressed data is larger than this runtime will produce at once")
var errTrailingCompressedData = errors.New("additional data follows the compressed stream")

// streamInflater owns only Go data on its workers. Its owner starts it lazily,
// delivers promises, and removes lifetime hooks after the pump and operations
// finish. An unbuffered output channel admits one decoded chunk per read.
type streamInflater struct {
	rt     *quickjs.Runtime
	loop   *Loop
	format string
	limit  int64

	base   context.Context
	ctx    context.Context
	cancel context.CancelFunc
	input  *io.PipeReader
	writer *io.PipeWriter
	output chan []byte
	done   chan struct{}
	work   sync.WaitGroup

	progressMu      sync.Mutex
	progressBytes   int64
	progressChanged chan struct{}
	acceptedBytes   int64
	// result is written before done closes and read only after observing done.
	result error

	// These fields belong to the runtime owner.
	started, inputClosed, readBusy, writeBusy, finished bool
	pending                                             int
	unhook, scopeUnhook                                 func()
	stopParent                                          func() bool
	notice                                              *quickjs.Promise
}

func (c *compressHost) unpacker(format string) (quickjs.Value, error) {
	u := &streamInflater{rt: c.rt, loop: c.loop, format: format, limit: maxInflated, notice: c.rt.NewPromise()}
	o := c.rt.NewObject()
	if err := setAll(o, map[string]any{
		"read": u.read, "write": u.write, "close": u.closeInput,
		"abort": func() { u.abort() }, "finished": u.notice.Value(),
	}); err != nil {
		return quickjs.Value{}, err
	}
	return o, nil
}

func (u *streamInflater) start() error {
	if u.started {
		return nil
	}
	u.base = u.rt.Context()
	if u.loop != nil {
		u.base = loopContext(u.loop)
	}
	if err := u.base.Err(); err != nil {
		return err
	}
	u.ctx, u.cancel = context.WithCancel(u.base)
	u.input, u.writer = io.Pipe()
	u.output, u.done = make(chan []byte), make(chan struct{})
	u.progressMu.Lock()
	u.progressBytes = 0
	u.progressChanged = make(chan struct{})
	u.progressMu.Unlock()
	u.acceptedBytes = 0
	u.started = true
	u.unhook = u.rt.OnClose(func() { u.stop(); u.work.Wait(); u.detach() })
	if u.loop != nil && u.loop.executionScope() != nil {
		var err error
		u.scopeUnhook, err = u.loop.executionScope().AddCleanupHandle(func() {
			u.stop()
			u.work.Wait()
			u.detach()
		})
		if err != nil {
			u.stop()
			u.detach()
			u.result = err
			close(u.output)
			close(u.done)
			u.finished = true
			u.notice.RejectError(err)
			return err
		}
	}
	u.stopParent = context.AfterFunc(u.base, u.stop)
	notification := u.rt.StartAsyncWork()
	notification.Unref()
	u.work.Add(1)
	go func() {
		defer u.work.Done()
		u.result = u.pump()
		u.input.CloseWithError(u.result)
		close(u.output)
		close(u.done)
		notification.Complete(func(err error) {
			if err != nil {
				return
			}
			u.finished = true
			if u.base.Err() == nil {
				u.settle(u.notice, nil, u.result)
			}
			u.maybeDetach()
		})
	}()
	return nil
}

func (u *streamInflater) pump() error {
	r, source, err := streamReaderFor(u.format, u.input, u.inputProgress)
	if err != nil {
		return err
	}
	defer r.Close()
	left := u.limit
	// Short streams need not allocate a full output chunk on their first read.
	size := int64(4 << 10)
	for {
		if left < size {
			size = left + 1
		}
		buf := make([]byte, size)
		n, err := r.Read(buf)
		if int64(n) > left {
			return errInflatedLimit
		}
		if n > 0 {
			left -= int64(n)
			select {
			case u.output <- buf[:n]:
			case <-u.ctx.Done():
				return u.ctx.Err()
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				// The format has a single member and no trailing bytes. Reading
				// exactly one byte also waits for input close, so a later write
				// cannot silently append data after the compressed member.
				if _, trailingErr := source.ReadByte(); trailingErr == nil {
					return errTrailingCompressedData
				} else if !errors.Is(trailingErr, io.EOF) {
					return trailingErr
				}
				return nil
			} else {
				err = fmt.Errorf("the data could not be decompressed: %w", err)
			}
			return err
		}
		size = chunkSize
	}
}

// inputProgress marks bytes accepted by header validation or taken from the
// bounded input buffer. A write is not complete merely because the pipe copied
// its bytes into that buffer: it must wait until the codec consumes the chunk.
func (u *streamInflater) inputProgress(bytes int64) {
	u.progressMu.Lock()
	if bytes > u.progressBytes {
		u.progressBytes = bytes
		close(u.progressChanged)
		u.progressChanged = make(chan struct{})
	}
	u.progressMu.Unlock()
}

func (u *streamInflater) waitForInput(target int64) error {
	for {
		u.progressMu.Lock()
		if u.progressBytes >= target {
			u.progressMu.Unlock()
			return nil
		}
		changed := u.progressChanged
		u.progressMu.Unlock()
		select {
		case <-changed:
		case <-u.done:
			u.progressMu.Lock()
			reached := u.progressBytes >= target
			u.progressMu.Unlock()
			if reached {
				return nil
			}
			if u.result != nil {
				return u.result
			}
			return io.ErrUnexpectedEOF
		}
	}
}

func (u *streamInflater) operation(p *quickjs.Promise, complete func(), run func() ([]byte, error)) {
	work := u.rt.StartAsyncWork()
	u.pending++
	u.work.Add(1)
	go func() {
		defer u.work.Done()
		data, err := run()
		work.Complete(func(closed error) {
			if closed != nil {
				return
			}
			u.pending--
			if complete != nil {
				complete()
			}
			if u.base.Err() == nil {
				u.settle(p, data, err)
			}
			u.maybeDetach()
		})
	}()
}

func (u *streamInflater) settle(p *quickjs.Promise, data []byte, err error) {
	if err != nil {
		name, message := "Error", err.Error()
		if errors.Is(err, errInflatedLimit) {
			name, message = "RangeError", errInflatedLimit.Error()
		}
		p.Reject(u.rt.NewError(name, message))
	} else if data != nil {
		p.Resolve(u.rt.NewBytes(data))
	} else {
		p.Resolve(nil)
	}
}

func (u *streamInflater) read() *quickjs.Promise {
	p := u.rt.NewPromise()
	if err := u.start(); err != nil {
		p.RejectError(err)
		return p
	}
	if u.readBusy {
		p.RejectError(errors.New("a decompression read is already pending"))
		return p
	}
	u.readBusy = true
	u.operation(p, func() { u.readBusy = false }, func() ([]byte, error) {
		data, ok := <-u.output
		if ok {
			return data, nil
		}
		<-u.done
		return nil, u.result
	})
	return p
}

func (u *streamInflater) write(value quickjs.Value) *quickjs.Promise {
	p := u.rt.NewPromise()
	data, ok := value.Bytes()
	if !ok {
		p.RejectError(errors.New("a decompression stream takes bytes"))
		return p
	}
	if err := u.start(); err != nil {
		p.RejectError(err)
		return p
	}
	if u.inputClosed || u.writeBusy {
		p.RejectError(errors.New("the decompression input is closed or busy"))
		return p
	}
	data = bytes.Clone(data)
	target := u.acceptedBytes + int64(len(data))
	u.acceptedBytes = target
	u.writeBusy = true
	u.operation(p, func() { u.writeBusy = false }, func() ([]byte, error) {
		_, err := u.writer.Write(data)
		if err != nil {
			<-u.done
			if u.result != nil {
				err = u.result
			}
			return nil, err
		}
		return nil, u.waitForInput(target)
	})
	return p
}

func (u *streamInflater) closeInput() *quickjs.Promise {
	p := u.rt.NewPromise()
	if err := u.start(); err != nil {
		p.RejectError(err)
		return p
	}
	u.inputClosed = true
	u.writer.Close()
	u.operation(p, nil, func() ([]byte, error) {
		<-u.done
		return nil, u.result
	})
	return p
}

func (u *streamInflater) stop() {
	u.cancel()
	u.input.CloseWithError(context.Canceled)
	u.writer.CloseWithError(context.Canceled)
}

func (u *streamInflater) abort() {
	if !u.started {
		return
	}
	u.stop()
	u.work.Wait()
	u.detach()
}

func (u *streamInflater) maybeDetach() {
	if u.finished && u.pending == 0 {
		u.detach()
	}
}

func (u *streamInflater) detach() {
	if u.unhook != nil {
		u.unhook()
		u.unhook = nil
	}
	if u.scopeUnhook != nil {
		u.scopeUnhook()
		u.scopeUnhook = nil
	}
	if u.stopParent != nil {
		u.stopParent()
		u.stopParent = nil
	}
	u.cancel()
}
