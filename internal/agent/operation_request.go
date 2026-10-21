package agent

import (
	"context"
	"errors"
)

// WithRequest runs work inside the operation and owning run's lifetime. The
// request remains in the close join until work returns, including any local
// output save performed after a provider call.
func (op *Operation) WithRequest(ctx context.Context, work func(context.Context) (any, error)) (any, error) {
	if work == nil {
		return nil, errors.New("agent request work is required")
	}
	merged, release, _, err := op.beginRequestContext(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	value, err := work(merged)
	if contextErr := merged.Err(); contextErr != nil {
		return nil, contextErr
	}
	return value, err
}

// beginRequestContext keeps an accepted native request in Operation.Close's
// join and combines its caller, operation, and owning run cancellation.
func (op *Operation) beginRequestContext(ctx context.Context) (context.Context, func(), *agentRun, error) {
	if ctx == nil {
		return nil, nil, nil, errors.New("agent request requires a context")
	}
	if op == nil || op.host == nil {
		return nil, nil, nil, ErrOperationClosed
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if !op.beginRequest() {
		return nil, nil, nil, ErrOperationClosed
	}
	h := op.host
	h.mu.Lock()
	if op.closed || h.closed || op.ctx.Err() != nil {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, ErrOperationClosed
	}
	run := h.currentOwnerRunLocked(op)
	if (op.task != nil || op.parentRunID != "") && run == nil {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, newOperationError(409, "scope", "agent request scope has no active owner run")
	}
	if run != nil && (h.runs[run.id] != run || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group)) {
		h.mu.Unlock()
		op.endRequest()
		return nil, nil, nil, newOperationError(409, "scope", "agent request scope has ended")
	}
	h.mu.Unlock()

	merged, cancel := context.WithCancel(ctx)
	stopOperation := context.AfterFunc(op.ctx, cancel)
	stopRun := func() bool { return false }
	if run != nil {
		stopRun = context.AfterFunc(run.ctx, cancel)
	}
	release := func() {
		stopRun()
		stopOperation()
		cancel()
		op.endRequest()
	}
	if err := merged.Err(); err != nil {
		release()
		return nil, nil, nil, err
	}
	return merged, release, run, nil
}
