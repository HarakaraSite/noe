package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// executionGroup is the bounded live summary for one root managed agent.
// Host.mu protects every field. Detailed causal history remains in events.
type executionGroup struct {
	id         string
	limits     ExecutionLimits
	counters   executionCounters
	usage      usageTotals
	stop       *executionStop
	startedAt  time.Time
	terminalAt time.Time
}

// ExecutionLimits configures optional root-group admission limits. Nil fields
// are unlimited; a pointer to zero admits no work in that dimension.
type ExecutionLimits struct {
	ProviderCalls *int64 `json:"providerCalls,omitempty"`
	AgentStarts   *int64 `json:"agentStarts,omitempty"`
	QueuedTasks   *int64 `json:"queuedTasks,omitempty"`
	HookCalls     *int64 `json:"hookCalls,omitempty"`
}

func (l *ExecutionLimits) UnmarshalJSON(data []byte) error {
	if l == nil || len(data) == 0 || strings.TrimSpace(string(data)) == "null" {
		return fmt.Errorf("executionLimits must be an object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return fmt.Errorf("executionLimits must be an object")
	}
	var parsed ExecutionLimits
	for key, raw := range fields {
		var target **int64
		switch key {
		case "providerCalls":
			target = &parsed.ProviderCalls
		case "agentStarts":
			target = &parsed.AgentStarts
		case "queuedTasks":
			target = &parsed.QueuedTasks
		case "hookCalls":
			target = &parsed.HookCalls
		default:
			return fmt.Errorf("unknown executionLimits option: %s", key)
		}
		if string(raw) == "null" {
			return fmt.Errorf("executionLimits.%s must be a nonnegative safe integer", key)
		}
		var value int64
		if err := json.Unmarshal(raw, &value); err != nil || value < 0 || value > maxSafeContextBudget {
			return fmt.Errorf("executionLimits.%s must be a nonnegative safe integer", key)
		}
		*target = &value
	}
	*l = parsed
	return nil
}

func cloneExecutionLimits(limits *ExecutionLimits) ExecutionLimits {
	if limits == nil {
		return ExecutionLimits{}
	}
	return ExecutionLimits{
		ProviderCalls: cloneInt64(limits.ProviderCalls),
		AgentStarts:   cloneInt64(limits.AgentStarts),
		QueuedTasks:   cloneInt64(limits.QueuedTasks),
		HookCalls:     cloneInt64(limits.HookCalls),
	}
}

func (l ExecutionLimits) pointerCopy() *ExecutionLimits {
	copy := cloneExecutionLimits(&l)
	if copy.ProviderCalls == nil && copy.AgentStarts == nil && copy.QueuedTasks == nil && copy.HookCalls == nil {
		return nil
	}
	return &copy
}

type executionLimitSource struct {
	ActivityID string          `json:"activityId,omitempty"`
	Origin     *activityOrigin `json:"origin,omitempty"`
}

type executionStop struct {
	Reason    string                `json:"reason"`
	LimitName string                `json:"limitName"`
	Limit     int64                 `json:"limit"`
	Used      int64                 `json:"used"`
	Source    *executionLimitSource `json:"source,omitempty"`
}

type executionLimitAction struct {
	root  *agentInstance
	event events.Event
	done  chan struct{}
}

type executionCounters struct {
	ProviderCalls         int64 `json:"providerCalls"`
	AgentProviderCalls    int64 `json:"agentProviderCalls"`
	DecisionProviderCalls int64 `json:"decisionProviderCalls"`
	ImageProviderCalls    int64 `json:"imageProviderCalls"`
	AgentStarts           int64 `json:"agentStarts"`
	QueuedTasks           int64 `json:"queuedTasks"`
	HookCalls             int64 `json:"hookCalls"`
	ActiveAgents          int64 `json:"activeAgents"`
	ActiveTasks           int64 `json:"activeTasks"`
}

type usageTotals struct {
	PromptTokens     *int64   `json:"promptTokens,omitempty"`
	CompletionTokens *int64   `json:"completionTokens,omitempty"`
	TotalTokens      *int64   `json:"totalTokens,omitempty"`
	CachedTokens     *int64   `json:"cachedTokens,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
}

type executionSnapshot struct {
	GroupID   string            `json:"groupId"`
	Counters  executionCounters `json:"counters"`
	Limits    ExecutionLimits   `json:"limits"`
	ElapsedMS int64             `json:"elapsedMs"`
	Stop      *executionStop    `json:"stop,omitempty"`
	Usage     *usageTotals      `json:"usage,omitempty"`
}

type activityOrigin struct {
	Kind        string `json:"kind"`
	RunID       string `json:"runId,omitempty"`
	AgentID     string `json:"agentId,omitempty"`
	ToolCallID  string `json:"toolCallId,omitempty"`
	ToolName    string `json:"toolName,omitempty"`
	CommandID   string `json:"commandId,omitempty"`
	HookID      string `json:"hookId,omitempty"`
	Module      string `json:"module,omitempty"`
	Phase       string `json:"phase,omitempty"`
	ModelStepID string `json:"modelStepId,omitempty"`
}

type hookInvocation struct {
	activityID     string
	event          events.Event // immutable after admission; Host.mu protects state
	state          string       // active, ending, completed, failed, or cancelled; Host.mu
	started        time.Time
	startEventDone chan struct{}
}

func (g *executionGroup) snapshotLocked() executionSnapshot {
	if g == nil {
		return executionSnapshot{}
	}
	now := time.Now()
	if !g.terminalAt.IsZero() {
		now = g.terminalAt
	}
	var elapsed int64
	if !g.startedAt.IsZero() && now.After(g.startedAt) {
		elapsed = now.Sub(g.startedAt).Milliseconds()
	}
	snapshot := executionSnapshot{
		GroupID: g.id, Counters: g.counters, Limits: cloneExecutionLimits(&g.limits),
		ElapsedMS: elapsed, Stop: cloneExecutionStop(g.stop),
	}
	if g.usage.hasValues() {
		usage := g.usage
		snapshot.Usage = &usage
	}
	return snapshot
}

func cloneExecutionStop(stop *executionStop) *executionStop {
	if stop == nil {
		return nil
	}
	copy := *stop
	if stop.Source != nil {
		source := *stop.Source
		if stop.Source.Origin != nil {
			origin := *stop.Source.Origin
			source.Origin = &origin
		}
		copy.Source = &source
	}
	return &copy
}

func (l ExecutionLimits) value(name string) *int64 {
	switch name {
	case "providerCalls":
		return l.ProviderCalls
	case "agentStarts":
		return l.AgentStarts
	case "queuedTasks":
		return l.QueuedTasks
	case "hookCalls":
		return l.HookCalls
	default:
		return nil
	}
}

func (c *executionCounters) value(name string) int64 {
	switch name {
	case "providerCalls":
		return c.ProviderCalls
	case "agentStarts":
		return c.AgentStarts
	case "queuedTasks":
		return c.QueuedTasks
	case "hookCalls":
		return c.HookCalls
	default:
		return 0
	}
}

func (c *executionCounters) admit(name string, providerKind string) {
	switch name {
	case "providerCalls":
		c.ProviderCalls++
		if providerKind == "image" {
			c.ImageProviderCalls++
		} else if providerKind == "decision" {
			c.DecisionProviderCalls++
		} else {
			c.AgentProviderCalls++
		}
	case "agentStarts":
		c.AgentStarts++
	case "queuedTasks":
		c.QueuedTasks++
	case "hookCalls":
		c.HookCalls++
	}
}

func (u usageTotals) hasValues() bool {
	return u.PromptTokens != nil || u.CompletionTokens != nil || u.TotalTokens != nil || u.CachedTokens != nil || u.Cost != nil
}

func (u *usageTotals) add(summary *usageSummary) {
	if summary == nil {
		return
	}
	u.PromptTokens = addInt64(u.PromptTokens, summary.PromptTokens)
	u.CompletionTokens = addInt64(u.CompletionTokens, summary.CompletionTokens)
	u.TotalTokens = addInt64(u.TotalTokens, summary.TotalTokens)
	u.CachedTokens = addInt64(u.CachedTokens, summary.CachedTokens)
	if summary.Cost != nil {
		value := *summary.Cost
		if u.Cost != nil {
			value += *u.Cost
		}
		u.Cost = &value
	}
}

func addInt64(total, value *int64) *int64 {
	if value == nil {
		return total
	}
	result := *value
	if total != nil {
		result += *total
	}
	return &result
}

func addActivityFields(fields events.Event, activityID string, causedBy []string, origin activityOrigin) {
	if activityID != "" {
		fields["activityId"] = activityID
	}
	if len(causedBy) != 0 {
		fields["causedBy"] = append([]string(nil), causedBy...)
	}
	if origin.Kind != "" {
		fields["origin"] = origin
	}
}

func appendUniqueActivityIDs(existing []string, additions ...string) []string {
	seen := make(map[string]struct{}, len(existing)+len(additions))
	for _, id := range existing {
		if id != "" {
			seen[id] = struct{}{}
		}
	}
	for _, id := range additions {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		existing = append(existing, id)
	}
	return existing
}

// currentOwnerRunLocked resolves the run executing in this scope. A managed
// task's bootstrap parentRunID deliberately remains its initiating ancestor;
// task.run is the current owner for work created by that task.
func (h *Host) currentOwnerRunLocked(op *Operation) *agentRun {
	if op == nil {
		return nil
	}
	if op.task != nil && op.task.run != nil {
		return op.task.run
	}
	if op.parentRunID != "" {
		return h.runs[op.parentRunID]
	}
	return nil
}

func (h *Host) executionForOperationLocked(op *Operation) *executionGroup {
	if op == nil {
		return nil
	}
	if op.task != nil && op.task.instance != nil && op.task.instance.group != nil {
		return op.task.instance.group
	}
	if run := h.currentOwnerRunLocked(op); run != nil {
		return run.group
	}
	return op.group
}

// reserveExecutionLocked checks a group limit, records the admitted attempt,
// and initiates the one-way group stop atomically with admission. Callers must
// publish the returned action after releasing Host.mu.
func (h *Host) reserveExecutionLocked(group *executionGroup, dimension, providerKind, sourceActivityID string, origin activityOrigin) (*executionLimitAction, bool) {
	if group == nil {
		return nil, true
	}
	if group.stop != nil || !h.executionGroupOpenLocked(group) {
		return nil, false
	}
	used := group.counters.value(dimension)
	if limit := group.limits.value(dimension); limit != nil && used >= *limit {
		return h.stopExecutionGroupLocked(group, dimension, *limit, used, sourceActivityID, origin), false
	}
	group.counters.admit(dimension, providerKind)
	return nil, true
}

func (h *Host) executionGroupOpenLocked(group *executionGroup) bool {
	if group == nil {
		return true
	}
	if group.stop != nil || !group.terminalAt.IsZero() {
		return false
	}
	root := h.instances[group.id]
	return root == nil || (root.state != "stopping" && root.state != "stopped" && root.state != "failed")
}

func (h *Host) stopExecutionGroupLocked(group *executionGroup, dimension string, limit, used int64, sourceActivityID string, origin activityOrigin) *executionLimitAction {
	if group == nil || group.stop != nil {
		return nil
	}
	var source *executionLimitSource
	if sourceActivityID != "" || origin.Kind != "" {
		source = &executionLimitSource{ActivityID: sourceActivityID}
		if origin.Kind != "" {
			originCopy := origin
			source.Origin = &originCopy
		}
	}
	group.stop = &executionStop{Reason: "execution_limit", LimitName: dimension, Limit: limit, Used: used, Source: source}
	root := h.instances[group.id]
	var done chan struct{}
	if root != nil && root.state != "stopped" && root.state != "failed" {
		root.state = "stopping"
		root.reason = "execution_limit"
		if root.executionLimitEventDone == nil {
			root.executionLimitEventDone = make(chan struct{})
		}
		done = root.executionLimitEventDone
		for _, run := range h.runs {
			if run.group == group {
				run.interrupting = true
			}
		}
	}
	eventActivityID, _ := randomID()
	causedBy := []string(nil)
	if sourceActivityID != "" {
		causedBy = []string{sourceActivityID}
	}
	fields := events.Event{
		"type": "execution.limit_reached", "groupId": group.id,
		"reason": "execution_limit", "limitName": dimension, "limit": limit, "used": used,
		"source": source, "execution": group.snapshotLocked(),
	}
	addActivityFields(fields, eventActivityID, causedBy, origin)
	if root != nil {
		fields["agentId"] = root.id
		fields["name"] = root.config.Name
		fields["model"] = root.config.Model
		if root.currentRunID != "" {
			fields["runId"] = root.currentRunID
		}
	}
	if origin.RunID != "" {
		fields["runId"] = origin.RunID
	}
	if origin.AgentID != "" {
		fields["sourceAgentId"] = origin.AgentID
	}
	return &executionLimitAction{root: root, event: fields, done: done}
}

func (h *Host) finishExecutionLimit(action *executionLimitAction) {
	if action == nil {
		return
	}
	if action.root != nil {
		action.root.cancel()
	}
	h.observe(action.event)
	if action.done != nil {
		close(action.done)
	}
}

func executionLimitError(group *executionGroup) (code, message string) {
	if group != nil && group.stop != nil {
		stop := group.stop
		return "execution_limit", fmt.Sprintf("execution limit %s reached: limit %d, used %d", stop.LimitName, stop.Limit, stop.Used)
	}
	return "scope", "execution group is stopping or finished"
}

func (h *Host) executionSnapshot(group *executionGroup) executionSnapshot {
	if group == nil {
		return executionSnapshot{}
	}
	h.mu.Lock()
	snapshot := group.snapshotLocked()
	h.mu.Unlock()
	return snapshot
}

func (h *Host) decorateExecution(fields events.Event, group *executionGroup) {
	if group == nil {
		return
	}
	fields["groupId"] = group.id
	fields["execution"] = h.executionSnapshot(group)
}
