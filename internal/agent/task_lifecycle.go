package agent

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// command retains one task's identity, admission gates and immutable outcome.
// Mutable state is protected by Host.mu and changed through *Locked operations;
// event publication, execution and terminal waits remain outside that lock.
type command struct {
	id              string
	activityID      string
	causedBy        []string
	origin          activityOrigin
	instruction     string
	operation       string
	status          string
	active          bool
	cancelRequested bool
	cancelAccepted  chan struct{}
	finalCommitted  bool // protected by Host.mu; survives removal of the run during finish cleanup
	accepted        chan struct{}
	done            chan struct{}
	doneWaiters     map[*taskCompletionWake]struct{} // Host.mu; only dynamic-select overflow registrations
	outcome         *taskOutcome                     // immutable after completion; protected by Host.mu
}

func newTaskCommand(id, activityID, instruction string) *command {
	return &command{
		id: id, activityID: activityID, instruction: instruction,
		operation: "queue", status: "accepted",
		accepted: make(chan struct{}), done: make(chan struct{}),
	}
}

// enqueueCommandLocked registers both the scheduling reservation and the
// retained command identity before acceptance is published.
func (i *agentInstance) enqueueCommandLocked(cmd *command) {
	i.commands[cmd.id] = cmd
	i.queue = append(i.queue, cmd)
}

// selectCommandLocked reserves the queue head while serve waits for its
// acceptance event. Selection is not task execution and remains cancellable.
func (i *agentInstance) selectCommandLocked() *command {
	cmd := i.queue[0]
	i.queue = i.queue[1:]
	i.selected = cmd
	return cmd
}

// commandStartDisposition is only a result of the selected-to-running
// operation, not a second stored representation of command state.
type commandStartDisposition uint8

const (
	commandStarted commandStartDisposition = iota
	commandCancelledBeforeStart
	commandReturnedToQueue
)

func (i *agentInstance) startSelectedCommandLocked(cmd *command) commandStartDisposition {
	i.selected = nil
	if cmd.cancelRequested {
		i.settleCommandLocked(cmd, taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "agent task cancelled"})
		return commandCancelledBeforeStart
	}
	if i.state == "stopping" || i.ctx.Err() != nil {
		// Whole-instance interruption took its queue snapshot after selection.
		// Return this accepted command to the head so shutdown settles it
		// without starting a task, and keeps the existing stop event order.
		cmd.status = "cancelled"
		i.queue = append([]*command{cmd}, i.queue...)
		return commandReturnedToQueue
	}
	cmd.status = "running"
	cmd.active = true
	i.state = "running"
	if i.group != nil {
		i.group.counters.ActiveTasks++
	}
	return commandStarted
}

// settleQueuedCommandsLocked consumes the pending reservations as one
// transition. Its caller owns the instance stop reason and publication order.
func (i *agentInstance) settleQueuedCommandsLocked(reason string) []*command {
	queued := i.queue
	i.queue = nil
	for _, cmd := range queued {
		i.settleCommandLocked(cmd, taskOutcome{CommandID: cmd.id, State: "cancelled", Error: reason})
	}
	return queued
}

// finishCommandLocked reaps execution accounting and advances the instance's
// last-result view without replacing the command's already committed result.
func (i *agentInstance) finishCommandLocked(cmd *command, outcome taskOutcome, fatal bool) (taskOutcome, bool) {
	if cmd.active {
		cmd.active = false
		if i.group != nil && i.group.counters.ActiveTasks > 0 {
			i.group.counters.ActiveTasks--
		}
		i.host.maybeCloseGroupLocked(i.group)
	}
	if cmd.outcome == nil {
		if cmd.cancelRequested {
			outcome = taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "agent task cancelled"}
			fatal = false
		}
		i.settleCommandLocked(cmd, outcome)
	} else {
		outcome = *cmd.outcome
	}
	i.lastResult = cmd.outcome
	i.currentRunID = ""
	return outcome, fatal
}

// taskCancellation carries only the lock-external part of one accepted
// cancellation. publishWG is reserved under Host.mu, including before run
// creation, so instance/owner shutdown joins its acceptance publication.
type taskCancellation struct {
	instance *agentInstance
	cancel   context.CancelFunc
	accepted chan struct{}
	fields   events.Event
}

func (i *agentInstance) acceptTaskCancellationLocked(op *Operation, cmd *command) (*taskCancellation, error) {
	if cmd.outcome != nil {
		return nil, newOperationError(409, "command", "task command has already finished")
	}
	if cmd.cancelRequested {
		return nil, nil // An accepted retry must not allocate another activity.
	}
	current := i.selected == cmd || cmd.active
	if cmd.finalCommitted && current {
		return nil, newOperationError(409, "command", "task final answer is already committed")
	}
	if i.currentRunID != "" {
		if run := i.host.runs[i.currentRunID]; run != nil && run.finalClosed && current {
			return nil, newOperationError(409, "command", "task final answer is already committed")
		}
	}
	fields, err := i.host.controlActivityLocked(op, "task")
	if err != nil {
		return nil, newOperationError(500, "broker", "could not allocate cancellation activity")
	}
	queueIndex := -1
	for index, candidate := range i.queue {
		if candidate == cmd {
			queueIndex = index
			break
		}
	}
	if queueIndex < 0 && !current {
		return nil, newOperationError(409, "command", "task command is not cancellable in its current state")
	}
	cmd.cancelRequested = true
	accepted := make(chan struct{})
	cmd.cancelAccepted = accepted
	fields["commandId"] = cmd.id
	cancellation := &taskCancellation{instance: i, accepted: accepted, fields: fields}
	if queueIndex >= 0 {
		i.queue = append(i.queue[:queueIndex], i.queue[queueIndex+1:]...)
		i.host.maybeCloseGroupLocked(i.group)
		i.settleCommandLocked(cmd, taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "agent task cancelled"})
		i.publishCommandOutcomeAsync(cmd)
	} else {
		cancellation.cancel = i.taskCancel
		if run := i.host.runs[i.currentRunID]; run != nil {
			run.interrupting = true
			i.interruptedRunID = run.id
			i.runInterruptAccepted[run.id] = accepted
			fields["runId"] = run.id
		}
	}
	i.publishWG.Add(1)
	return cancellation, nil
}

func (c *taskCancellation) publish() {
	if c.cancel != nil {
		c.cancel()
	}
	go func() {
		defer c.instance.publishWG.Done()
		c.instance.event("interrupt.accepted", c.fields)
		close(c.accepted)
	}()
}

// bindRunLocked connects the authenticated task scope, run and current-run
// view together. The caller still owns conversation/context initialization.
func (task *managedTask) bindRunLocked(run *agentRun) {
	run.instance = task.instance
	task.run = run
	task.instance.currentRunID = run.id
}

// commitFinalAnswerLocked closes both the run gate and the managed command's
// cancellation gate. The command boundary survives removal from Host.runs.
func (run *agentRun) commitFinalAnswerLocked(answer string) []<-chan struct{} {
	run.finalClosed = true
	run.finalAnswer = answer
	if run.instance != nil && run.operation.task != nil && run.operation.task.run == run && run.operation.task.command != nil {
		run.operation.task.command.finalCommitted = true
	}
	settled := make([]<-chan struct{}, 0, len(run.steerOrder))
	for _, command := range run.steerOrder {
		if command.status == "applied" {
			settled = append(settled, command.settled)
		}
	}
	return settled
}

// publishCommittedFinal preserves applied-steer -> final-event -> final-waiter
// ordering. No observer or channel wait runs while Host.mu is held.
func (h *Host) publishCommittedFinal(run *agentRun, answer string, settled []<-chan struct{}) {
	for _, eventDone := range settled {
		<-eventDone
	}
	h.observeRun("agent.final", run.id, run, events.Event{
		"status": "completed", "text": answer,
	})
	h.mu.Lock()
	if !run.finalEventDone {
		run.finalEventDone = true
		close(run.finalDone)
	}
	h.mu.Unlock()
}

func (task *managedTask) commitOutcomeLocked(result Result, err error) (taskOutcome, *agentRun, bool) {
	i, cmd := task.instance, task.command
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
	return outcome, run, fatal
}

func (i *agentInstance) commandEvent(cmd *command, status, runID string) {
	fields := events.Event{"commandId": cmd.id, "status": status, "runId": runID}
	addActivityFields(fields, cmd.activityID, cmd.causedBy, cmd.origin)
	operation := cmd.operation
	if operation == "" {
		operation = "queue"
	}
	fields["operation"] = operation
	if status == "accepted" {
		fields["instruction"] = cmd.instruction
	}
	i.event("command."+status, fields)
}

func (i *agentInstance) settleCommandLocked(cmd *command, outcome taskOutcome) {
	cmd.status = outcome.State
	cmd.outcome = &outcome
}

// Publish the existing terminal event before waking result waiters. A command's
// result remains available after later tasks overwrite the instance snapshot.
func (i *agentInstance) publishCommandOutcome(cmd *command) {
	<-cmd.accepted
	if cmd.cancelAccepted != nil {
		<-cmd.cancelAccepted
	}
	i.commandEvent(cmd, cmd.outcome.State, cmd.outcome.RunID)
	i.host.mu.Lock()
	cmd.publishDoneLocked()
	i.host.mu.Unlock()
}

// publishDoneLocked publishes existing completion readiness and any overflow
// registrations atomically. Event publication must already have finished.
func (cmd *command) publishDoneLocked() {
	close(cmd.done)
	for waiter := range cmd.doneWaiters {
		if !waiter.notified {
			close(waiter.done)
			waiter.notified = true
		}
	}
	cmd.doneWaiters = nil
}

// publishCommandOutcomeAsync is for terminal events published outside serve.
// Callers add while holding Host.mu so instance shutdown cannot race a zero
// counter Wait with a new publisher.
func (i *agentInstance) publishCommandOutcomeAsync(cmd *command) {
	i.publishWG.Add(1)
	go func() {
		defer i.publishWG.Done()
		i.publishCommandOutcome(cmd)
	}()
}
