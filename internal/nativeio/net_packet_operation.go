package nativeio

import (
	"context"
	"errors"
	"io"
	"sync"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// packetOperation retains pending bind results until owner-thread handoff.
// Native close/cancellation never waits for JS delivery or touches JS values.
type packetOperation struct {
	h      *Host
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	handed bool
	closed bool
	value  any
	err    error
	done   chan struct{}
	close  sync.Once
	drop   func(any)
}

type packetConvert func(any) (value any, owner io.Closer, err error)

func (h *Host) packetOperation(parent context.Context, run func(context.Context) (any, error), convert packetConvert, drop func(any)) quickjs.Value {
	ctx, cancel := context.WithCancel(parent)
	op := &packetOperation{h: h, ctx: ctx, cancel: cancel, done: make(chan struct{}), drop: drop}
	h.mu.Lock()
	if h.closed || ctx.Err() != nil {
		h.mu.Unlock()
		cancel()
		return h.rejectedNetOperation(h.operationError())
	}
	h.resources[op] = struct{}{}
	h.wg.Add(1)
	h.mu.Unlock()
	promise := h.rt.NewPromise()
	work := h.rt.StartAsyncWork()
	go func() {
		defer h.wg.Done()
		value, err := run(ctx)
		op.mu.Lock()
		op.value, op.err = value, err
		close(op.done)
		op.mu.Unlock()
		work.Complete(func(closed error) {
			if closed != nil || h.ctx.Err() != nil {
				_ = op.Close()
				return
			}
			value, err := op.take()
			var owner io.Closer
			if err == nil && convert != nil {
				value, owner, err = convert(value)
			}
			if err != nil {
				if owner != nil {
					_ = owner.Close()
				}
				h.rejectPacketPromise(promise, err)
			} else if err := promise.Resolve(value); err != nil {
				if owner != nil {
					_ = owner.Close()
				}
				promise.RejectError(err)
			}
		})
	}()
	operation := h.rt.NewObject()
	if err := errors.Join(operation.Set("promise", promise.Value()), operation.Set("cancel", op.cancelPending)); err != nil {
		op.cancelPending()
		return h.rejectedNetOperation(err)
	}
	return operation
}

func (op *packetOperation) take() (any, error) {
	op.mu.Lock()
	value, err := op.value, op.err
	op.value = nil
	if op.closed || op.ctx.Err() != nil {
		err = op.ctx.Err()
		if err == nil {
			err = context.Canceled
		}
		if op.drop != nil {
			op.drop(value)
		}
		value = nil
	}
	op.handed = true
	op.cancel()
	op.mu.Unlock()
	op.h.release(op)
	return value, err
}

func (op *packetOperation) cancelPending() {
	op.mu.Lock()
	if !op.handed {
		op.cancel()
	}
	op.mu.Unlock()
}

func (op *packetOperation) Close() error {
	op.close.Do(func() {
		op.mu.Lock()
		op.closed = true
		op.cancel()
		op.mu.Unlock()
		<-op.done
		op.mu.Lock()
		value := op.value
		op.value = nil
		op.mu.Unlock()
		if op.drop != nil {
			op.drop(value)
		}
		op.h.release(op)
	})
	return nil
}

type udpMessageSizeError struct{}

func (*udpMessageSizeError) Error() string { return "UDP datagram exceeds the receive packet buffer" }

func (h *Host) rejectPacketPromise(p *quickjs.Promise, err error) {
	var sizeError *udpMessageSizeError
	if errors.As(err, &sizeError) {
		value := h.rt.NewError("RangeError", sizeError.Error())
		_ = value.Set("code", "ERR_UDP_MESSAGE_TOO_LARGE")
		_ = p.Reject(value)
	} else {
		p.RejectError(err)
	}
}
