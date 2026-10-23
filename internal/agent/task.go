package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
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
	} else {
		cmd.executionStarted = true
	}
	h.mu.Unlock()
	if err == nil {
		if h.opts.Execute == nil {
			err = errors.New("agent execution runtime is unavailable")
			result = failedResult()
		} else {
			result, err = h.opts.Execute(ctx, Execution{
				Operation: op, Kind: ExecutionTask,
				Config: append([]byte(nil), i.configJSON...), Instruction: cmd.instruction,
			})
		}
	} else {
		result = Result{ExitCode: -1, Reason: "cancelled"}
	}
	op.Close() // Joins finish/model handlers before reading the authenticated report.
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
	// Cancellation admission and terminal outcome commit share Host.mu. If a
	// cancellation wins this lock first, it becomes the command's outcome; if
	// this commit wins, CancelTask observes the committed outcome and rejects.
	run, report := task.run, task.report
	finalAnswer := ""
	finalCommitted := false
	taskInterrupted := false
	if run != nil {
		finalCommitted = run.finalClosed
		if finalCommitted {
			finalAnswer = run.finalAnswer
		}
		taskInterrupted = i.interruptedRunID == run.id
	}
	instanceInterrupted := i.reason == "interrupted"
	outcome := taskOutcome{CommandID: cmd.id, State: "failed"}
	if run != nil {
		outcome.RunID = run.id
		outcome.ErrorDetails = run.refusal
	}
	fatal := false
	switch {
	case instanceInterrupted:
		outcome.State = "cancelled"
		outcome.Error = "agent interrupted"
	case cmd.cancelRequested:
		outcome.State = "cancelled"
		outcome.Error = "agent task cancelled"
	case taskInterrupted:
		outcome.State = "cancelled"
		outcome.Error = "agent task interrupted"
	case i.ctx.Err() != nil:
		outcome.State = "cancelled"
		outcome.Error = "agent owner cancelled"
	case err != nil:
		fatal = true
		outcome.Error = err.Error()
	case report != nil && sanitizeRunStatus(report.Status) == "failed" && report.Error != "":
		// The scoped finish report carries the task's semantic error. The
		// in-process executor also exits nonzero after that report, so selecting
		// the process summary first would erase the useful JavaScript failure.
		outcome.State = "failed"
		outcome.Error = report.Error
	case report == nil:
		outcome.Error = "task process exited without an authenticated completion report"
	case result.ExitCode != 0 || result.Reason != "completed":
		outcome.Error = taskProcessFailure(result, fmt.Sprintf("task process failed: %s (exit %d)", result.Reason, result.ExitCode))
	default:
		outcome.State = sanitizeRunStatus(report.Status)
		outcome.Error = report.Error
	}
	if report != nil {
		outcome.Answer = report.Answer
	}
	if finalCommitted {
		// Keep the committed answer observable even if the child process exits
		// before it can send its completion report.
		outcome.Answer = finalAnswer
	}
	i.settleCommandLocked(cmd, outcome)
	h.mu.Unlock()
	if result.Stdout != "" {
		i.event("stdout", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stdout})
	}
	if result.Stderr != "" {
		i.event("stderr", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stderr})
	}
	return outcome, run, fatal
}

func taskProcessFailure(result Result, fallback string) string {
	if detail := strings.TrimSpace(result.Stderr); detail != "" {
		return fallback + ": " + detail
	}
	return fallback
}
