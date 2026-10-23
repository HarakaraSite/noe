package agent

import (
	"context"
)

func (h *Host) retainToolResult(ctx context.Context, run *agentRun, result *Result) error {
	h.mu.Lock()
	if h.closed || run.operation.closed || run.ctx.Err() != nil {
		h.mu.Unlock()
		return context.Canceled
	}
	if run.toolResults == nil {
		run.toolResults = new(toolResultStore)
	}
	store := run.toolResults
	h.mu.Unlock()
	if err := store.retain(ctx, result); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		result.Diagnostics = append(result.Diagnostics, ExecutionDiagnostic{Code: "JS_HOST", Phase: "serialize",
			Message: "Complete tool result could not be retained: " + err.Error()})
		result.Reason, result.Phase = "failed", "serialize"
		if result.ExitCode == 0 {
			result.ExitCode = 1
		}
	}
	return nil
}

func (h *Host) readToolResult(ctx context.Context, op *Operation, request toolArtifactRequest) (toolResultPage, error) {
	h.mu.Lock()
	run := h.currentOwnerRunLocked(op)
	if run == nil || op.closed || run.ctx.Err() != nil || run.finalClosed || h.closed {
		h.mu.Unlock()
		return toolResultPage{}, newOperationError(409, "scope", "tool results belong to the current active task")
	}
	store := run.toolResults
	h.mu.Unlock()
	if store == nil {
		return toolResultPage{}, newOperationError(404, "result", "unknown tool result reference")
	}
	return store.read(ctx, request)
}

func closeResultStreams(result *Result) {
	for _, stream := range []interface{ Close() error }{result.StdoutFull, result.StderrFull} {
		if stream != nil {
			// Fallback for cancellation before retention. These files contain
			// temporary captured output and have no remaining consumers.
			_ = stream.Close()
		}
	}
	result.StdoutFull, result.StderrFull = nil, nil
}
