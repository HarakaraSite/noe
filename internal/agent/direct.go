package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
)

func (h *Host) callDirect(ctx context.Context, caller *Operation, scope, method, payload string) (string, error) {
	value, err := h.withDirectRequest(ctx, caller, scope, func(merged context.Context, target *Operation) (any, error) {
		value, err := h.dispatchDirect(merged, target, method, payload)
		if err != nil {
			return nil, err
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, &directResponseEncodingError{err: fmt.Errorf("encode direct agent response: %w", err)}
		}
		return string(encoded), nil
	})
	if err != nil {
		return "", err
	}
	response, ok := value.(string)
	if !ok {
		return "", errors.New("direct agent response is not encoded")
	}
	return response, nil
}

// withDirectRequest is the shared acceptance boundary for the JSON adapter and
// the typed Go client. It owns target lookup, close joining, cancellation, and
// conversion of operation failures into safe request errors.
func (h *Host) withDirectRequest(ctx context.Context, caller *Operation, scope string, work func(context.Context, *Operation) (any, error)) (any, error) {
	target, err := h.directOperation(caller, scope)
	if err != nil {
		return nil, err
	}
	if !caller.beginRequest() {
		return nil, ErrOperationClosed
	}
	defer caller.endRequest()
	if target != caller {
		if !target.beginRequest() {
			return nil, ErrOperationClosed
		}
		defer target.endRequest()
	}
	merged, cancel := context.WithCancel(ctx)
	stopCaller := context.AfterFunc(caller.ctx, cancel)
	stopTarget := func() bool { return false }
	if target != caller {
		stopTarget = context.AfterFunc(target.ctx, cancel)
	}
	defer func() {
		stopCaller()
		stopTarget()
		cancel()
	}()
	if caller.initErr != nil {
		return nil, caller.initErr
	}
	if target.initErr != nil {
		return nil, target.initErr
	}
	value, err := work(merged, target)
	if err != nil {
		var encodingErr *directResponseEncodingError
		if errors.As(err, &encodingErr) {
			return nil, encodingErr.err
		}
		if merged.Err() != nil {
			return nil, merged.Err()
		}
		var operationErr *operationError
		if errors.As(err, &operationErr) {
			return nil, &RequestError{Code: operationErr.code, Message: operationErr.message, cause: err}
		}
		if errors.Is(err, errRequestCancelled) {
			if merged.Err() != nil {
				return nil, merged.Err()
			}
			return nil, &RequestError{Code: "cancelled", Message: "agent request cancelled", cause: err}
		}
		return nil, err
	}
	return value, nil
}

// directResponseEncodingError keeps the JSON adapter's historical precedence:
// a response encoding failure is returned even if cancellation races with it.
type directResponseEncodingError struct{ err error }

func (e *directResponseEncodingError) Error() string { return e.err.Error() }
func (e *directResponseEncodingError) Unwrap() error { return e.err }

func (h *Host) directOperation(caller *Operation, scope string) (*Operation, error) {
	if caller == nil || caller.host != h {
		return nil, ErrOperationClosed
	}
	if scope == "" || scope == caller.scope {
		return caller, nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	owner := h.currentOwnerRunLocked(caller)
	for candidate := range h.operations {
		if candidate.scope != scope || !candidate.hookScope || candidate.closed || candidate.ctx.Err() != nil {
			continue
		}
		if owner != nil && candidate.ownerRun == owner {
			return candidate, nil
		}
		if owner == nil && candidate.ownerRun != nil && candidate.ownerRun.operation == caller {
			return candidate, nil
		}
	}
	return nil, &RequestError{Code: "scope", Message: "unknown or unowned hook scope"}
}

func (h *Host) dispatchDirect(ctx context.Context, op *Operation, method, payload string) (any, error) {
	decode := func(target any) error {
		return json.Unmarshal([]byte(payload), target)
	}
	switch method {
	case "list_agents":
		if !op.control {
			return nil, newOperationError(403, "scope", "host management is only available to a trusted Go operation")
		}
		return h.listAgents(op)
	case "list_models":
		if h.opts.Models == nil {
			return nil, newOperationError(502, "provider", "agent model service is unavailable")
		}
		var query model.ModelQuery
		if err := decode(&query); err != nil {
			return nil, newOperationError(400, "request", "invalid model catalog query")
		}
		return h.opts.Models.ListModels(ctx, query)
	case "decide":
		var raw json.RawMessage
		if err := decode(&raw); err != nil {
			return nil, newOperationError(400, "request", "invalid decision request")
		}
		request, err := decision.ParseRequest(raw)
		if err != nil {
			return nil, newOperationError(400, "request", err.Error())
		}
		return h.decide(ctx, op, request)
	case "start":
		var request startRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid start request")
		}
		return h.start(ctx, op, request)
	case "finish":
		var request finishRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid finish request")
		}
		return h.finish(ctx, op, request)
	case "model":
		var request modelRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid model request")
		}
		return h.model(ctx, op, request)
	case "begin_model_step":
		var request beginModelStepRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId and nonempty messages are required")
		}
		return h.beginModelStep(op, request)
	case "context_prepare":
		var request contextPrepareRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId, modelStepId, and nonempty messages are required")
		}
		return h.contextPrepare(op, request)
	case "context_get":
		var request contextAccessRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid context request")
		}
		return h.contextGet(op, request)
	case "context_set_runtime_info":
		var request contextSetRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid context runtime info")
		}
		return h.contextSetRuntimeInfo(op, request)
	case "begin_hook_scope":
		var request hookScopeRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid hook scope request")
		}
		return h.beginHookScope(op, request)
	case "end_hook_scope":
		var request hookScopeRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid hook scope request")
		}
		return h.endHookScope(op, request)
	case "execution_get":
		var request struct{}
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid execution request")
		}
		return h.executionGet(op)
	case "run_js":
		var request runJSRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid run_js request")
		}
		return h.runJS(ctx, op, request)
	case "tool_result":
		var request toolResultRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid tool result report")
		}
		return h.recordToolResult(op, request)
	case "conversation_checkpoint":
		var request conversationCheckpointRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId and snapshot are required")
		}
		return h.conversationCheckpoint(op, request)
	case "discard_images":
		var request discardImagesRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid image discard")
		}
		return h.discardImages(op, request)
	case "apply_steers":
		var request applySteersRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId and commandIds are required")
		}
		return h.applySteers(op, request)
	case "commit_final":
		var request commitFinalRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId and a nonempty answer are required")
		}
		return h.commitFinal(ctx, op, request)
	case "steer_agent":
		var request SteerRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "runId and instruction are required")
		}
		return h.steer(op, request)
	case "spawn_agent":
		var request SpawnRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid agent configuration")
		}
		return h.spawn(ctx, op, request)
	case "observe_agent", "wait_agent":
		var request InstanceRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "agentId is required")
		}
		return h.observeInstance(op, request, method == "wait_agent", ctx)
	case "queue_agent":
		var request QueueRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "agentId and instruction are required")
		}
		return h.queue(op, request)
	case "wait_task":
		var request TaskRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "agentId and commandId are required")
		}
		return h.waitTask(op, request, ctx)
	case "read_task_result":
		var request TaskResultRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "invalid task result query")
		}
		return h.readTaskResult(op, request)
	case "cancel_task":
		var request CancelTaskRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "agentId and commandId are required")
		}
		return h.cancelTask(op, request)
	case "stop_agent":
		var request StopAgentRequest
		if err := decode(&request); err != nil {
			return nil, newOperationError(400, "request", "agentId is required")
		}
		return h.stopAgent(op, request)
	default:
		return nil, &RequestError{Code: "method", Message: "agent operation is not available: " + method}
	}
}

func validOperationMethod(method string) bool {
	switch method {
	case "list_agents", "list_models", "decide", "start", "finish", "model", "begin_model_step",
		"context_prepare", "context_get", "context_set_runtime_info", "begin_hook_scope", "end_hook_scope",
		"execution_get", "run_js", "tool_result", "conversation_checkpoint", "discard_images", "apply_steers",
		"commit_final", "steer_agent", "spawn_agent", "observe_agent", "wait_agent", "queue_agent", "wait_task",
		"read_task_result", "cancel_task", "stop_agent":
		return true
	}
	return false
}
