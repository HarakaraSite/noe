package agent

import (
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type endedRun struct {
	id         string
	agentID    string
	parentID   string
	name       string
	model      string
	started    time.Time
	group      *executionGroup
	activityID string
	causedBy   []string
	origin     activityOrigin
}

func (h *Host) observe(event events.Event) {
	if h.opts.Observe == nil {
		return
	}
	h.observeMu.Lock()
	defer h.observeMu.Unlock()
	h.opts.Observe(event)
}

func (h *Host) observeRun(eventType, runID string, run *agentRun, fields events.Event) {
	event := events.Event{
		"type": eventType, "runId": runID,
		"parentRunId": run.parentRunID, "name": run.config.Name, "model": run.config.Model,
	}
	addActivityFields(event, run.activityID, run.causedBy, run.origin)
	h.decorateExecution(event, run.group)
	if id := run.agentID(); id != "" {
		event["agentId"] = id
	}
	for key, value := range fields {
		event[key] = value
	}
	h.observe(event)
}

func (h *Host) observeEndedRun(eventType string, run endedRun, fields events.Event) {
	event := events.Event{
		"type": eventType, "runId": run.id,
		"parentRunId": run.parentID, "name": run.name, "model": run.model,
	}
	addActivityFields(event, run.activityID, run.causedBy, run.origin)
	h.decorateExecution(event, run.group)
	if run.agentID != "" {
		event["agentId"] = run.agentID
	}
	for key, value := range fields {
		event[key] = value
	}
	h.observe(event)
}
