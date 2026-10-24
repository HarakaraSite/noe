package agent

import (
	"context"
	"reflect"
	"strings"
)

// taskCompletionWake coalesces just the channels that exceed reflect.Select's
// Go runtime case ceiling. Its registration, notification and removal all use
// Host.mu; it has no goroutine and introduces no task-count limit.
type taskCompletionWake struct {
	done     chan struct{}
	notified bool
}

// waitTasks ends the request itself on steer, leaving the operation, owner run,
// child instances and retained outcomes untouched. No helper goroutine or timer
// outlives the request. It selects a per-run wake channel alongside existing
// command done channels; Go select-case overflow uses request-local completion
// registrations, removed on every wake or cancellation.
func (h *Host) waitTasks(op *Operation, req WaitTasksRequest, requestContext context.Context) (WaitTasksResult, error) {
	if len(req.Tasks) == 0 {
		return WaitTasksResult{}, newOperationError(400, "request", "tasks must be nonempty")
	}
	seen := make(map[TaskReference]struct{}, len(req.Tasks))
	for _, ref := range req.Tasks {
		if strings.TrimSpace(ref.AgentID) == "" || strings.TrimSpace(ref.CommandID) == "" {
			return WaitTasksResult{}, newOperationError(400, "request", "agentId and commandId are required")
		}
		if _, duplicate := seen[ref]; duplicate {
			return WaitTasksResult{}, newOperationError(400, "request", "tasks must be unique")
		}
		seen[ref] = struct{}{}
	}

	h.mu.Lock()
	// Admit every ID before inspecting completion or steer. A valid early
	// result must not hide an invalid or unowned member of the set.
	commands := make([]*command, len(req.Tasks))
	for index, ref := range req.Tasks {
		instance := h.instances[ref.AgentID]
		if instance == nil || !h.canManageLocked(op, instance) {
			h.mu.Unlock()
			return WaitTasksResult{}, newOperationError(409, "agent", "unknown or unowned agent")
		}
		cmd := instance.commands[ref.CommandID]
		if cmd == nil {
			h.mu.Unlock()
			return WaitTasksResult{}, newOperationError(409, "command", "unknown task command")
		}
		commands[index] = cmd
	}
	run := h.currentOwnerRunLocked(op)
	if run == nil && (op.task != nil || op.parentRunID != "") {
		h.mu.Unlock()
		return WaitTasksResult{}, newOperationError(409, "scope", "agent request scope has no active owner run")
	}

	for {
		if op.closed || h.closed || op.ctx.Err() != nil {
			h.mu.Unlock()
			return WaitTasksResult{}, newOperationError(409, "cancelled", "agent operation cancelled")
		}
		if requestContext.Err() != nil {
			h.mu.Unlock()
			return WaitTasksResult{}, errRequestCancelled
		}
		if run != nil && (h.runs[run.id] != run || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group)) {
			h.mu.Unlock()
			return WaitTasksResult{}, newOperationError(409, "scope", "agent request scope has ended")
		}

		result := WaitTasksResult{
			Completed: make([]CompletedTask, 0, len(commands)),
			Pending:   make([]TaskReference, 0, len(commands)),
		}
		cases := []reflect.SelectCase{
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(op.ctx.Done())},
			{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(requestContext.Done())},
		}
		// Go allows 65536 cases in one reflect.Select. Reserve cancellation,
		// owner/steer and one overflow wake case, not a limit on request size.
		const directTaskCases = 65536 - 5
		var overflow []*command
		for index, cmd := range commands {
			select {
			case <-cmd.done:
				outcome := *cmd.outcome
				outcome.omitAnswer = req.IncludeAnswer != nil && !*req.IncludeAnswer
				result.Completed = append(result.Completed, CompletedTask{AgentID: req.Tasks[index].AgentID, Outcome: outcome})
			default:
				result.Pending = append(result.Pending, req.Tasks[index])
				if len(cases) < 2+directTaskCases {
					cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(cmd.done)})
				} else {
					overflow = append(overflow, cmd)
				}
			}
		}

		publishing := false
		if run != nil {
			result.RunID = run.id
			for _, steer := range run.steerOrder {
				if steer.status != "accepted" && steer.status != "delivered" {
					continue
				}
				select {
				case <-steer.accepted:
					// Steer wins even when all command results are ready.
					result.Reason = "steer"
				default:
					// Do not release an all-completed wait ahead of an
					// accepted steer whose observer is still publishing.
					publishing = true
				}
			}
		}
		if result.Reason == "steer" || len(result.Pending) == 0 && !publishing {
			if result.Reason == "" {
				result.Reason = "completed"
			}
			h.mu.Unlock()
			return result, nil
		}
		if run != nil {
			if run.steerWake == nil {
				run.steerWake = make(chan struct{})
			}
			cases = append(cases,
				reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(run.steerWake)},
				reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(run.ctx.Done())})
		}
		var completionWake *taskCompletionWake
		if len(overflow) != 0 {
			completionWake = &taskCompletionWake{done: make(chan struct{})}
			for _, cmd := range overflow {
				if cmd.doneWaiters == nil {
					cmd.doneWaiters = make(map[*taskCompletionWake]struct{})
				}
				cmd.doneWaiters[completionWake] = struct{}{}
			}
			cases = append(cases, reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(completionWake.done)})
		}
		h.mu.Unlock()
		reflect.Select(cases)
		h.mu.Lock()
		for _, cmd := range overflow {
			delete(cmd.doneWaiters, completionWake)
			if len(cmd.doneWaiters) == 0 {
				cmd.doneWaiters = nil
			}
		}
		// Rescan all channels and pending steer state after every wake instead
		// of allowing select's random choice to decide the return reason.
	}
}
