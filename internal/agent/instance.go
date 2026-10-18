package agent

import (
	"context"
	"encoding/json"
	"time"

	"noe/internal/events"
	imageapi "noe/internal/image"
)

type command struct {
	id          string
	activityID  string
	causedBy    []string
	origin      activityOrigin
	instruction string
	operation   string
	status      string
	initial     bool
	active      bool
	accepted    chan struct{}
}

type taskOutcome struct {
	RunID        string          `json:"runId"`
	CommandID    string          `json:"commandId"`
	State        string          `json:"state"`
	Answer       string          `json:"answer"`
	Error        string          `json:"error,omitempty"`
	ErrorDetails *imageapi.Error `json:"errorDetails,omitempty"`
}

type instanceSnapshot struct {
	AgentID      string             `json:"agentId"`
	GroupID      string             `json:"groupId,omitempty"`
	ActivityID   string             `json:"activityId,omitempty"`
	CausedBy     []string           `json:"causedBy,omitempty"`
	Origin       *activityOrigin    `json:"origin,omitempty"`
	Execution    *executionSnapshot `json:"execution,omitempty"`
	ParentRunID  string             `json:"parentRunId"`
	Name         string             `json:"name"`
	State        string             `json:"state"`
	Reason       string             `json:"reason"`
	KeepAlive    bool               `json:"keepAlive"`
	CurrentRunID string             `json:"currentRunId"`
	QueueLength  int                `json:"queueLength"`
	LastResult   *taskOutcome       `json:"lastResult"`
	Context      *contextSnapshot   `json:"context,omitempty"`
}

type agentInstance struct {
	host                 *Host
	id                   string
	config               taskConfig
	configJSON           json.RawMessage
	keepAlive            bool
	owner                *Operation
	parent               *agentRun
	group                *executionGroup
	modelWindow          *int64
	modelMaxOutputTokens *int64
	context              *contextSnapshot
	conversation         *conversationSnapshot
	images               map[string]*imageapi.Data
	conversationContext  *contextContinuation
	activityID           string
	causedBy             []string
	origin               activityOrigin
	ctx                  context.Context
	cancel               context.CancelFunc
	done                 chan struct{}
	wake                 chan struct{}
	// Mutable fields below are protected by Host.mu.
	state                   string
	reason                  string
	queue                   []*command
	commands                map[string]*command
	currentRunID            string
	lastResult              *taskOutcome
	taskCancel              context.CancelFunc
	interruptedRunID        string
	runInterruptAccepted    map[string]chan struct{}
	interruptAccepted       chan struct{}
	interruptSettled        chan struct{}
	executionLimitEventDone chan struct{}
	activeCounted           bool
}

func (i *agentInstance) snapshotLocked() instanceSnapshot {
	parentID := ""
	if i.parent != nil {
		parentID = i.parent.id
	}
	snapshot := instanceSnapshot{AgentID: i.id, ActivityID: i.activityID, CausedBy: append([]string(nil), i.causedBy...), ParentRunID: parentID, Name: i.config.Name, State: i.state, Reason: i.reason, KeepAlive: i.keepAlive, CurrentRunID: i.currentRunID, QueueLength: len(i.queue), LastResult: i.lastResult}
	if i.context != nil {
		contextCopy := *i.context
		contextCopy.Previous = cloneContextPrevious(i.context.Previous)
		contextCopy.Estimate = cloneContextEstimate(i.context.Estimate)
		snapshot.Context = &contextCopy
	}
	if i.group != nil {
		snapshot.GroupID = i.group.id
	}
	if i.origin.Kind != "" {
		origin := i.origin
		snapshot.Origin = &origin
	}
	if i.group != nil {
		execution := i.group.snapshotLocked()
		snapshot.Execution = &execution
	}
	return snapshot
}

func (i *agentInstance) event(kind string, fields events.Event) {
	parentID := ""
	if i.parent != nil {
		parentID = i.parent.id
	}
	event := events.Event{"type": kind, "agentId": i.id, "parentRunId": parentID, "name": i.config.Name, "model": i.config.Model}
	addActivityFields(event, i.activityID, i.causedBy, i.origin)
	i.host.decorateExecution(event, i.group)
	for key, value := range fields {
		event[key] = value
	}
	i.host.observe(event)
}
func (i *agentInstance) commandEvent(cmd *command, status, runID string) {
	fields := events.Event{"commandId": cmd.id, "status": status, "runId": runID}
	addActivityFields(fields, cmd.activityID, cmd.causedBy, cmd.origin)
	operation := cmd.operation
	if operation == "" {
		operation = "queue"
	}
	fields["operation"] = operation
	i.event("command."+status, fields)
}

func (i *agentInstance) serve() {
	defer close(i.done)
	defer i.cancel()
	h := i.host
	for {
		h.mu.Lock()
		if i.group != nil && i.group.stop != nil && i.state != "stopped" && i.state != "failed" {
			i.state = "stopping"
			if i.reason == "" {
				i.reason = i.group.stop.Reason
			}
		}
		if i.ctx.Err() != nil || i.state == "stopping" {
			reason := i.reason
			if reason == "" {
				reason = "owner_cancelled"
			}
			i.state = "stopping"
			queued := i.queue
			i.queue = nil
			for _, cmd := range queued {
				cmd.status = "cancelled"
			}
			accepted, settled := i.interruptAccepted, i.interruptSettled
			limitEventDone := i.executionLimitEventDone
			if limitEventDone == nil && i.group != nil {
				if root := h.instances[i.group.id]; root != nil {
					limitEventDone = root.executionLimitEventDone
				}
			}
			h.mu.Unlock()
			if limitEventDone != nil {
				<-limitEventDone
			}
			if accepted != nil {
				<-accepted
			}
			if reason == "interrupted" {
				if settled != nil {
					<-settled
				}
				for _, cmd := range queued {
					<-cmd.accepted
					i.commandEvent(cmd, "cancelled", "")
				}
				i.event("instance.stopping", events.Event{"reason": reason})
			} else {
				i.event("instance.stopping", events.Event{"reason": reason})
				for _, cmd := range queued {
					<-cmd.accepted
					i.commandEvent(cmd, "cancelled", "")
				}
				if settled != nil {
					<-settled
				}
			}
			i.finish("stopped", reason)
			return
		}
		if len(i.queue) == 0 {
			if !i.keepAlive {
				reason := "task_completed"
				if i.lastResult != nil && i.lastResult.State == "cancelled" && i.interruptedRunID == i.lastResult.RunID {
					reason = "task_interrupted"
				} else if i.lastResult != nil && i.lastResult.State != "completed" {
					reason = "task_failed"
				}
				fields := i.finishLocked("stopped", reason)
				h.mu.Unlock()
				i.event("instance.stopped", fields)
				return
			}
			changed := i.state != "idle"
			i.state = "idle"
			h.mu.Unlock()
			if changed {
				i.event("instance.idle", events.Event{})
			}
			select {
			case <-i.ctx.Done():
			case <-i.wake:
			}
			continue
		}
		cmd := i.queue[0]
		i.queue = i.queue[1:]
		h.mu.Unlock()
		<-cmd.accepted // Publish command acceptance before any execution event.
		h.mu.Lock()
		if i.group != nil && i.group.stop != nil {
			i.state = "stopping"
			if i.reason == "" {
				i.reason = i.group.stop.Reason
			}
		}
		if i.state == "stopping" || i.ctx.Err() != nil {
			// The command was already removed from queue when whole-instance
			// interruption took its snapshot. Put it back so the stopping branch
			// emits the accepted command's cancellation without starting a task.
			cmd.status = "cancelled"
			i.queue = append([]*command{cmd}, i.queue...)
			h.mu.Unlock()
			continue
		}
		cmd.status = "running"
		cmd.active = true
		i.state = "running"
		if i.group != nil {
			i.group.counters.ActiveTasks++
		}
		h.mu.Unlock()
		runningFields := events.Event{"commandId": cmd.id}
		addActivityFields(runningFields, cmd.activityID, cmd.causedBy, cmd.origin)
		i.event("instance.running", runningFields)
		outcome, run, fatal := i.execute(cmd)
		h.mu.Lock()
		if cmd.active {
			cmd.active = false
			if i.group != nil && i.group.counters.ActiveTasks > 0 {
				i.group.counters.ActiveTasks--
			}
			h.maybeCloseGroupLocked(i.group)
		}
		i.lastResult = &outcome
		i.currentRunID = ""
		cmd.status = outcome.State
		if fatal {
			if i.state == "stopping" || i.ctx.Err() != nil {
				// An accepted stop takes precedence over supervision cleanup.
				// The next iteration publishes that stop and cancels reservations.
				fatal = false
			} else {
				// Close admission before publishing the failed task. Observers
				// may block or immediately send a whole-instance interrupt.
				i.state = "stopping"
				i.reason = "supervision_failed"
			}
		}
		h.mu.Unlock()
		if run != nil {
			fields := events.Event{"status": outcome.State, "duration": time.Since(run.started)}
			if outcome.ErrorDetails != nil {
				fields["errorDetails"] = outcome.ErrorDetails
			}
			if outcome.Error != "" {
				fields["error"] = outcome.Error
			}
			h.traceEvent(traceEvent{RunID: run.id, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model, Status: outcome.State, Duration: time.Since(run.started)})
			h.observeRun("agent."+outcome.State, run.id, run, fields)
		}
		i.commandEvent(cmd, outcome.State, outcome.RunID)
		if fatal {
			h.mu.Lock()
			i.state = "stopping"
			queued := i.queue
			i.queue = nil
			for _, cmd := range queued {
				cmd.status = "cancelled"
			}
			h.mu.Unlock()
			for _, cmd := range queued {
				<-cmd.accepted
				i.commandEvent(cmd, "cancelled", "")
			}
			i.finish("failed", "supervision_failed")
			return
		}
	}
}

func (i *agentInstance) finish(state, reason string) {
	h := i.host
	h.mu.Lock()
	fields := i.finishLocked(state, reason)
	h.mu.Unlock()
	i.event("instance."+state, fields)
}

// finishLocked makes terminal state and active-group accounting visible as
// one transition. The caller must hold Host.mu and publish the event afterward.
func (i *agentInstance) finishLocked(state, reason string) events.Event {
	h := i.host
	i.state = state
	i.reason = reason
	i.currentRunID = ""
	i.conversation = nil
	i.images = nil
	i.conversationContext = nil
	if i.activeCounted {
		i.activeCounted = false
		if i.group != nil && i.group.counters.ActiveAgents > 0 {
			i.group.counters.ActiveAgents--
		}
		h.maybeCloseGroupLocked(i.group)
	}
	return events.Event{"status": state, "reason": reason}
}

func (h *Host) maybeCloseGroupLocked(group *executionGroup) {
	if group != nil && group.counters.ActiveAgents == 0 && group.counters.ActiveTasks == 0 {
		if group.terminalAt.IsZero() {
			group.terminalAt = time.Now()
		}
		delete(h.groups, group.id)
	}
}
