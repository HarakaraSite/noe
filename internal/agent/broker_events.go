package agent

import (
	"encoding/json"
	"time"

	"noe/internal/events"
)

func fieldsWith(base, additional events.Event) events.Event {
	fields := make(events.Event, len(base)+len(additional))
	for key, value := range base {
		fields[key] = value
	}
	for key, value := range additional {
		fields[key] = value
	}
	return fields
}

func (b *broker) traceJSResult(runID string, run *agentRun, started time.Time, result Result) {
	exitCode := result.ExitCode
	b.host.traceEvent(traceEvent{
		RunID: runID, ParentID: run.parentRunID, Name: run.config.Name, Model: run.config.Model,
		Status: "js_result", Duration: time.Since(started),
		ExitCode: &exitCode, Reason: result.Reason,
	})
}

type traceEvent struct {
	RunID    string        `json:"runId,omitempty"`
	ParentID string        `json:"parentRunId,omitempty"`
	Name     string        `json:"name,omitempty"`
	Model    string        `json:"model,omitempty"`
	Status   string        `json:"status"`
	Duration time.Duration `json:"duration,omitempty"`
	Usage    *usageSummary `json:"usage,omitempty"`
	ExitCode *int          `json:"exitCode,omitempty"`
	Reason   string        `json:"reason,omitempty"`
}

type usageSummary struct {
	PromptTokens     *int64   `json:"promptTokens,omitempty"`
	CompletionTokens *int64   `json:"completionTokens,omitempty"`
	TotalTokens      *int64   `json:"totalTokens,omitempty"`
	CachedTokens     *int64   `json:"cachedTokens,omitempty"`
	Cost             *float64 `json:"cost,omitempty"`
}

func (h *Host) traceEvent(event traceEvent) {
	if h.opts.Trace == nil {
		return
	}
	h.traceMu.Lock()
	defer h.traceMu.Unlock()
	_ = json.NewEncoder(h.opts.Trace).Encode(event)
}

func summarizeUsage(raw json.RawMessage) *usageSummary {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	var usage struct {
		PromptTokens     *int64 `json:"prompt_tokens"`
		CompletionTokens *int64 `json:"completion_tokens"`
		InputTokens      *int64 `json:"input_tokens"`
		OutputTokens     *int64 `json:"output_tokens"`
		TotalTokens      *int64 `json:"total_tokens"`
		PromptDetails    *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
		InputDetails *struct {
			CachedTokens *int64 `json:"cached_tokens"`
		} `json:"input_tokens_details"`
		Cost *float64 `json:"cost"`
	}
	if json.Unmarshal(raw, &usage) != nil {
		return nil
	}
	summary := &usageSummary{
		PromptTokens: usage.PromptTokens, CompletionTokens: usage.CompletionTokens,
		TotalTokens: usage.TotalTokens, Cost: usage.Cost,
	}
	if usage.PromptDetails != nil {
		summary.CachedTokens = usage.PromptDetails.CachedTokens
	}
	if summary.PromptTokens == nil {
		summary.PromptTokens = usage.InputTokens
	}
	if summary.CompletionTokens == nil {
		summary.CompletionTokens = usage.OutputTokens
	}
	if summary.CachedTokens == nil && usage.InputDetails != nil {
		summary.CachedTokens = usage.InputDetails.CachedTokens
	}
	return summary
}
