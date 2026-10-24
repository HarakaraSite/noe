package agent

import (
	"context"
	"errors"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

type managedTask struct {
	instance *agentInstance
	command  *command
	run      *agentRun
	report   *finishRequest
}

func (i *agentInstance) execute(cmd *command) (taskOutcome, *agentRun, bool) {
	h := i.host
	h.mu.Lock()
	if cmd.cancelRequested || i.ctx.Err() != nil || h.ctx.Err() != nil {
		h.mu.Unlock()
		return taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "agent task cancelled"}, nil, false
	}
	h.mu.Unlock()
	ctx, cancel := context.WithCancel(i.ctx)
	h.mu.Lock()
	i.taskCancel = cancel
	if cmd.cancelRequested {
		cancel()
	}
	h.mu.Unlock()
	defer func() {
		cancel()
		h.mu.Lock()
		i.taskCancel = nil
		h.mu.Unlock()
	}()
	op := h.NewOperation(ctx)
	task := &managedTask{instance: i, command: cmd}
	h.mu.Lock()
	op.task = task
	op.group = i.group
	if i.parent != nil {
		op.parentRunID = i.parent.id
	}
	h.mu.Unlock()
	h.mu.Lock()
	groupOpen := h.executionGroupOpenLocked(i.group)
	h.mu.Unlock()
	if !groupOpen {
		op.Close()
		return taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "execution group is stopping"}, nil, false
	}
	var result Result
	var err error
	h.mu.Lock()
	if cmd.cancelRequested || ctx.Err() != nil {
		err = context.Canceled
	}
	h.mu.Unlock()
	if err == nil {
		if h.opts.Execute == nil {
			err = errors.New("agent execution runtime is unavailable")
			result = failedResult()
			appendExecutionDiagnostic(&result, err, "setup")
		} else {
			result, err = h.opts.Execute(ctx, Execution{
				Operation: op, Kind: ExecutionTask,
				Config: append([]byte(nil), i.configJSON...), Instruction: cmd.instruction,
			})
		}
	} else {
		result = Result{ExitCode: -1, Reason: "cancelled"}
		appendExecutionDiagnostic(&result, err, "setup")
	}
	op.Close() // Joins finish/model handlers before reading the authenticated report.
	if err != nil {
		appendExecutionDiagnostic(&result, err, "execute")
	}
	appendReasonDiagnostic(&result, "execute")
	h.mu.Lock()
	var interruptSettled []chan struct{}
	if run := task.run; run != nil {
		if accepted := i.runInterruptAccepted[run.id]; accepted != nil {
			interruptSettled = append(interruptSettled, accepted)
			delete(i.runInterruptAccepted, run.id)
		}
	}
	if i.interruptSettled != nil {
		interruptSettled = append(interruptSettled, i.interruptSettled)
	}
	h.mu.Unlock()
	for _, accepted := range interruptSettled {
		<-accepted
	}
	h.mu.Lock()
	outcome, run, fatal := task.commitOutcomeLocked(result, err)
	h.mu.Unlock()
	if result.Stdout != "" {
		i.event("stdout", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stdout})
	}
	if result.Stderr != "" {
		i.event("stderr", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stderr})
	}
	if len(result.Diagnostics) > 0 {
		fields := events.Event{
			"source": "agent_task", "commandId": cmd.id,
			"diagnostics": result.Diagnostics,
		}
		if outcome.RunID != "" {
			fields["runId"] = outcome.RunID
		}
		i.event("execution.diagnostic", fields)
	}
	return outcome, run, fatal
}

func taskProcessFailure(result Result, fallback string) string {
	if detail := executionDiagnosticMessage(result.Diagnostics); detail != "" {
		return fallback + ": " + detail
	}
	if detail := strings.TrimSpace(result.Stderr); detail != "" {
		return fallback + ": " + detail
	}
	return fallback
}

func executionDiagnosticMessage(diagnostics []executionresult.Diagnostic) string {
	messages := make([]string, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		if message := strings.TrimSpace(diagnostic.Message); message != "" {
			messages = append(messages, message)
		}
	}
	return strings.Join(messages, "; ")
}
