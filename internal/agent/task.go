package agent

import (
	"context"
	"fmt"

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
	ctx, cancel := context.WithCancel(i.ctx)
	h.mu.Lock()
	i.taskCancel = cancel
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
	b := h.broker
	h.mu.Unlock()
	connection := &Connection{URL: b.url, Token: b.token, Scope: op.scope, ModelWindow: cloneInt64(i.modelWindow), ModelMaxOutputTokens: cloneInt64(i.modelMaxOutputTokens), Defaults: Config{
		Provider: i.config.Provider, Model: i.config.Model, MaxTokens: i.config.MaxTokens,
		Effort: i.config.Effort, Hooks: cloneHookModules(i.config.hookModules()),
		ContextBudget: cloneContextBudget(i.config.ContextBudget), contextBudgetPresent: i.config.contextBudgetPresent || i.config.ContextBudget != nil,
		ContextPolicy: cloneContextPolicy(i.config.ContextPolicy), contextPolicyPresent: i.config.contextPolicyPresent || i.config.ContextPolicy != nil,
	}}
	h.mu.Lock()
	groupOpen := h.executionGroupOpenLocked(i.group)
	h.mu.Unlock()
	if !groupOpen {
		op.Close()
		return taskOutcome{CommandID: cmd.id, State: "cancelled", Error: "execution group is stopping"}, nil, false
	}
	result, err := (Runner{Executable: h.opts.Executable, Cwd: h.opts.Cwd, Connection: connection, ImportMap: h.opts.ImportMap}).RunTask(ctx, i.configJSON, cmd.instruction)
	op.Close() // Joins finish/model handlers before reading the authenticated report.
	h.mu.Lock()
	run, report := task.run, task.report
	finalAnswer := ""
	finalCommitted := false
	taskInterrupted := false
	var interruptSettled []chan struct{}
	if run != nil {
		finalCommitted = run.finalClosed
		if finalCommitted {
			finalAnswer = run.finalAnswer
		}
		taskInterrupted = i.interruptedRunID == run.id
		if accepted := i.runInterruptAccepted[run.id]; accepted != nil {
			interruptSettled = append(interruptSettled, accepted)
			delete(i.runInterruptAccepted, run.id)
		}
	}
	instanceInterrupted := i.reason == "interrupted"
	if i.interruptSettled != nil {
		interruptSettled = append(interruptSettled, i.interruptSettled)
	}
	h.mu.Unlock()
	for _, accepted := range interruptSettled {
		<-accepted
	}
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
	case taskInterrupted:
		outcome.State = "cancelled"
		outcome.Error = "agent task interrupted"
	case i.ctx.Err() != nil:
		outcome.State = "cancelled"
		outcome.Error = "agent owner cancelled"
	case err != nil:
		fatal = true
		outcome.Error = err.Error()
	case report == nil:
		outcome.Error = "task process exited without an authenticated completion report"
	case result.ExitCode != 0 || result.Reason != "completed":
		outcome.Error = fmt.Sprintf("task process failed: %s (exit %d)", result.Reason, result.ExitCode)
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
	if result.Stdout != "" {
		i.event("stdout", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stdout})
	}
	if result.Stderr != "" {
		i.event("stderr", events.Event{"source": "agent_task", "runId": outcome.RunID, "text": result.Stderr})
	}
	return outcome, run, fatal
}
