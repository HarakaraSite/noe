package agent

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type TaskOutcome struct {
	omitAnswer   bool            // Per-response projection; never set on the retained result.
	RunID        string          `json:"runId"`
	CommandID    string          `json:"commandId"`
	State        string          `json:"state"`
	Answer       string          `json:"answer"`
	AnswerBytes  *int            `json:"answerBytes,omitempty"`
	Error        string          `json:"error,omitempty"`
	ErrorDetails *imageapi.Error `json:"errorDetails,omitempty"`
}

type taskOutcome = TaskOutcome

// Keep the default wire shape, including an empty answer, for existing callers.
func (o taskOutcome) MarshalJSON() ([]byte, error) {
	type outcome taskOutcome
	if !o.omitAnswer {
		return json.Marshal(outcome(o))
	}
	return json.Marshal(struct {
		outcome
		Answer      *string `json:"answer,omitempty"`
		AnswerBytes int     `json:"answerBytes"`
	}{outcome: outcome(o), AnswerBytes: len(o.Answer)})
}

type InstanceSnapshot struct {
	AgentID          string             `json:"agentId"`
	GroupID          string             `json:"groupId,omitempty"`
	ActivityID       string             `json:"activityId,omitempty"`
	CausedBy         []string           `json:"causedBy,omitempty"`
	Origin           *activityOrigin    `json:"origin,omitempty"`
	Execution        *executionSnapshot `json:"execution,omitempty"`
	ParentRunID      string             `json:"parentRunId"`
	Name             string             `json:"name"`
	State            string             `json:"state"`
	Reason           string             `json:"reason"`
	KeepAlive        bool               `json:"keepAlive"`
	CurrentRunID     string             `json:"currentRunId"`
	CurrentCommandID string             `json:"currentCommandId,omitempty"`
	QueueLength      int                `json:"queueLength"`
	LastResult       *taskOutcome       `json:"lastResult"`
	Context          *contextSnapshot   `json:"context,omitempty"`
}

type instanceSnapshot = InstanceSnapshot

type agentInstance struct {
	host                 *Host
	id                   string
	config               taskConfig
	configJSON           json.RawMessage
	keepAlive            bool
	allowDelegation      bool // Frozen at launch by the owning caller; never inherited.
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
	publishWG            sync.WaitGroup
	// Mutable fields below are protected by Host.mu.
	instructionLoaded       bool
	instructionText         string
	instructionErr          error
	instructionLoading      chan struct{}
	state                   string
	reason                  string
	queue                   []*command
	selected                *command
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
	currentCommandID := ""
	for _, command := range i.commands {
		if command.active {
			currentCommandID = command.id
			break
		}
	}
	snapshot := instanceSnapshot{AgentID: i.id, ActivityID: i.activityID, CausedBy: append([]string(nil), i.causedBy...), ParentRunID: parentID, Name: i.config.Name, State: i.state, Reason: i.reason, KeepAlive: i.keepAlive, CurrentRunID: i.currentRunID, CurrentCommandID: currentCommandID, QueueLength: len(i.queue), LastResult: i.lastResult}
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
			queued := i.settleQueuedCommandsLocked(reason)
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
					i.publishCommandOutcome(cmd)
				}
				i.event("instance.stopping", events.Event{"reason": reason})
			} else {
				i.event("instance.stopping", events.Event{"reason": reason})
				for _, cmd := range queued {
					i.publishCommandOutcome(cmd)
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
		cmd := i.selectCommandLocked()
		h.mu.Unlock()
		<-cmd.accepted // Publish command acceptance before any execution event.
		h.mu.Lock()
		if i.group != nil && i.group.stop != nil {
			i.state = "stopping"
			if i.reason == "" {
				i.reason = i.group.stop.Reason
			}
		}
		switch i.startSelectedCommandLocked(cmd) {
		case commandCancelledBeforeStart:
			h.mu.Unlock()
			i.publishCommandOutcome(cmd)
			continue
		case commandReturnedToQueue:
			h.mu.Unlock()
			continue
		}
		h.mu.Unlock()
		runningFields := events.Event{"commandId": cmd.id}
		addActivityFields(runningFields, cmd.activityID, cmd.causedBy, cmd.origin)
		i.event("instance.running", runningFields)
		outcome, run, fatal := i.execute(cmd)
		h.mu.Lock()
		outcome, fatal = i.finishCommandLocked(cmd, outcome, fatal)
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
		// Cancellation can be admitted after execute snapshots its run gate.
		// The command gate is fixed by outcome commitment and covers both events.
		if cmd.cancelAccepted != nil {
			<-cmd.cancelAccepted
		}
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
		i.publishCommandOutcome(cmd)
		if fatal {
			h.mu.Lock()
			i.state = "stopping"
			queued := i.settleQueuedCommandsLocked("supervision_failed")
			h.mu.Unlock()
			for _, cmd := range queued {
				i.publishCommandOutcome(cmd)
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
