package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

const maxSafeContextBudget = int64(1<<53 - 1)

// ContextBudget is an advisory input-token budget for one agent run.
type ContextBudget struct {
	InputTokens int64 `json:"inputTokens"`
}

func (b *ContextBudget) UnmarshalJSON(data []byte) error {
	var value struct {
		InputTokens json.Number `json:"inputTokens"`
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return errors.New("contextBudget must contain only inputTokens")
	}
	input, err := value.InputTokens.Int64()
	if err != nil || input <= 0 || input > maxSafeContextBudget {
		return errors.New("contextBudget.inputTokens must be a positive safe integer")
	}
	b.InputTokens = input
	return nil
}

type contextBudgetSnapshot struct {
	InputTokens *int64 `json:"inputTokens"`
}

type contextModelWindow struct {
	Tokens *int64  `json:"tokens"`
	Source *string `json:"source"`
}

type contextPrevious struct {
	RequestID    string    `json:"requestId"`
	Status       string    `json:"status"`
	PromptTokens *int64    `json:"promptTokens"`
	CachedTokens *int64    `json:"cachedTokens"`
	MeasuredAt   time.Time `json:"measuredAt"`
}

type contextEstimate struct {
	Tokens      int64     `json:"tokens"`
	Method      string    `json:"method"`
	Stage       string    `json:"stage"`
	ModelStepID string    `json:"modelStepId"`
	RequestID   *string   `json:"requestId"`
	EstimatedAt time.Time `json:"estimatedAt"`
}

type contextSnapshot struct {
	ExecutionContext *executionContextSnapshot `json:"executionContext,omitempty"`
	RunID            string                    `json:"runId"`
	ModelStepID      string                    `json:"modelStepId"`
	Budget           contextBudgetSnapshot     `json:"budget"`
	ModelWindow      contextModelWindow        `json:"modelWindow"`
	Previous         *contextPrevious          `json:"previous"`
	Estimate         *contextEstimate          `json:"estimate"`
	RemainingTokens  *int64                    `json:"remainingTokens"`
	UsedPercent      *float64                  `json:"usedPercent"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
}

// contextRunState deliberately stores only the current estimate inputs and
// runtime information. The transcript remains in the task runtime.
type contextRunState struct {
	policy                 *ContextPolicy
	policyDisabled         bool
	maxTokens              int
	catalogMaxOutput       *int64
	fixedTokens            int64
	executionTokens        int64
	targetUnreachable      bool
	trimCount              int
	lastTrim               *contextTrimSnapshot
	pendingNotice          *contextTrimSnapshot
	pendingPlan            *pendingContextTrim
	pendingTrimActivityIDs []string
	preparedDigest         string
	finalPrepared          bool
	budget                 *ContextBudget
	modelWindow            *int64
	previous               *contextPrevious
	modelStepID            string
	estimate               *contextEstimate
	runtimeInfo            map[string]string
	baseMessagesBytes      int
	baseMessageCount       int
	toolsBytes             int
	prepared               bool
}

// contextContinuation carries only facts that remain meaningful when a queue
// task starts a new run. Pending plans and prepared request state stay local
// to their run.
type contextContinuation struct {
	previous               *contextPrevious
	trimCount              int
	lastTrim               *contextTrimSnapshot
	pendingNotice          *contextTrimSnapshot
	pendingTrimActivityIDs []string
}

func (s *contextRunState) restoreConversation(previous *contextContinuation) {
	if previous == nil {
		return
	}
	s.previous = cloneContextPrevious(previous.previous)
	s.trimCount = previous.trimCount
	s.lastTrim = cloneContextTrimSnapshot(previous.lastTrim)
	s.pendingNotice = cloneContextTrimSnapshot(previous.pendingNotice)
	s.pendingTrimActivityIDs = append([]string(nil), previous.pendingTrimActivityIDs...)
}

func (s *contextRunState) conversationContinuation() *contextContinuation {
	return &contextContinuation{
		previous:               cloneContextPrevious(s.previous),
		trimCount:              s.trimCount,
		lastTrim:               cloneContextTrimSnapshot(s.lastTrim),
		pendingNotice:          cloneContextTrimSnapshot(s.pendingNotice),
		pendingTrimActivityIDs: append([]string(nil), s.pendingTrimActivityIDs...),
	}
}

func cloneContextTrimSnapshot(trim *contextTrimSnapshot) *contextTrimSnapshot {
	if trim == nil {
		return nil
	}
	copy := *trim
	return &copy
}

func parseContextBudget(raw json.RawMessage) (*ContextBudget, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true, nil
	}
	var budget ContextBudget
	if err := json.Unmarshal(raw, &budget); err != nil {
		return nil, true, err
	}
	return &budget, true, nil
}

func cloneContextBudget(budget *ContextBudget) *ContextBudget {
	if budget == nil {
		return nil
	}
	copy := *budget
	return &copy
}

func contextWindowForModel(catalog interface {
	ListModels(provider.ModelQuery) ([]provider.Model, error)
}, providerName, modelName string) *int64 {
	if catalog == nil {
		return nil
	}
	models, err := catalog.ListModels(provider.ModelQuery{Provider: providerName, Kind: "agent"})
	if err != nil {
		return nil
	}
	for _, model := range models {
		if model.Model == modelName && model.ContextWindow > 0 {
			value := model.ContextWindow
			return &value
		}
	}
	return nil
}

func (s *contextRunState) resetStep(modelStepID string) {
	s.modelStepID = modelStepID
	s.estimate = nil
	s.runtimeInfo = make(map[string]string)
	s.baseMessagesBytes = 0
	s.baseMessageCount = 0
	s.toolsBytes = 0
	s.prepared = false
	s.finalPrepared = false
	s.preparedDigest = ""
	s.pendingPlan = nil
	s.targetUnreachable = false
}

func (s *contextRunState) snapshot(runID string, now time.Time) contextSnapshot {
	snapshot := contextSnapshot{
		ExecutionContext: s.executionSnapshot(),
		RunID:            runID, ModelStepID: s.modelStepID,
		Budget:      contextBudgetSnapshot{},
		ModelWindow: contextModelWindow{},
		Previous:    cloneContextPrevious(s.previous),
		Estimate:    cloneContextEstimate(s.estimate),
		UpdatedAt:   now,
	}
	if s.budget != nil {
		value := s.budget.InputTokens
		snapshot.Budget.InputTokens = &value
	}
	if s.modelWindow != nil {
		value := *s.modelWindow
		snapshot.ModelWindow.Tokens = &value
		source := "catalog"
		snapshot.ModelWindow.Source = &source
	}
	var tokenCount *int64
	if s.estimate != nil {
		value := s.estimate.Tokens
		tokenCount = &value
	}
	if s.budget != nil && tokenCount != nil {
		remaining := s.budget.InputTokens - *tokenCount
		snapshot.RemainingTokens = &remaining
		percent := float64(*tokenCount) * 100 / float64(s.budget.InputTokens)
		snapshot.UsedPercent = &percent
	}
	return snapshot
}

func cloneContextPrevious(previous *contextPrevious) *contextPrevious {
	if previous == nil {
		return nil
	}
	copy := *previous
	copy.PromptTokens = cloneInt64(previous.PromptTokens)
	copy.CachedTokens = cloneInt64(previous.CachedTokens)
	return &copy
}

func cloneContextEstimate(estimate *contextEstimate) *contextEstimate {
	if estimate == nil {
		return nil
	}
	copy := *estimate
	if estimate.RequestID != nil {
		value := *estimate.RequestID
		copy.RequestID = &value
	}
	return &copy
}

func cloneInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func (h *Host) storeContextSnapshotLocked(run *agentRun, now time.Time) contextSnapshot {
	snapshot := run.context.snapshot(run.id, now)
	if run.instance != nil {
		copy := snapshot
		copy.Previous = cloneContextPrevious(snapshot.Previous)
		copy.Estimate = cloneContextEstimate(snapshot.Estimate)
		run.instance.context = &copy
		run.instance.conversationContext = run.context.conversationContinuation()
	}
	return snapshot
}

func (h *Host) emitContextUpdated(run *agentRun, snapshot contextSnapshot, status, activityID string, causes []string) {
	fields := events.Event{
		"context":     snapshot,
		"modelStepId": snapshot.ModelStepID,
		"status":      status,
		"activityId":  activityID,
		"origin":      activityOrigin{Kind: "model", RunID: run.id, AgentID: run.agentID(), ModelStepID: snapshot.ModelStepID},
	}
	if snapshot.Estimate != nil && snapshot.Estimate.RequestID != nil {
		fields["requestId"] = *snapshot.Estimate.RequestID
	}
	if len(causes) != 0 {
		fields["causedBy"] = append([]string(nil), causes...)
	}
	h.observeRun("context.updated", run.id, run, fields)
}

func contextMessage(snapshot contextSnapshot, runtimeInfo map[string]string) string {
	var out strings.Builder
	out.WriteString("NOE_CONTEXT_STATUS\n")
	out.WriteString("Estimated messages/tools UTF-8 JSON bytes divided by 4; not a hard execution limit.\n")
	if execution := snapshot.ExecutionContext; execution != nil {
		percent := "unknown"
		if execution.UsedPercent != nil {
			percent = fmt.Sprintf("%.1f%%", *execution.UsedPercent)
		}
		fmt.Fprintf(&out, "Execution context: ~%d / %d tokens (%s); source=%s; total input allowance=%d.\n", execution.EstimatedTokens, execution.BudgetTokens, percent, execution.BudgetSource, execution.TotalInputBudgetTokens)
		out.WriteString("The Execution context denominator is the history budget and trimming threshold when enabled. Input budget is the computed total allowance, including fixed instructions; it remains available when Model context window is unknown.\n")
		if execution.Policy == "auto" {
			out.WriteString("Older completed history may be discarded at the budget threshold. Save information only if useful for the task; saving is optional.\n")
			if execution.NearBudget {
				out.WriteString("Context budget is near or exceeded; avoid unnecessary archival work.\n")
			}
		} else {
			out.WriteString("Automatic context trimming is disabled; conversation history is unchanged.\n")
		}
		if execution.BudgetExceeded {
			out.WriteString("Execution context budget exceeded; protected input or the newest complete block may prevent further trimming.\n")
		}
		if trim := execution.PendingTrim; trim != nil {
			fmt.Fprintf(&out, "Context trimmed: ~%d -> ~%d tokens; older execution history was removed.\n", trim.BeforeTokens, trim.AfterTokens)
		}
	}
	budget, remaining, usedPercent := snapshot.Budget.InputTokens, snapshot.RemainingTokens, snapshot.UsedPercent
	if execution := snapshot.ExecutionContext; execution != nil {
		value := execution.TotalInputBudgetTokens
		budget = &value
		remaining, usedPercent = nil, nil
		if snapshot.Estimate != nil {
			left := value - snapshot.Estimate.Tokens
			remaining = &left
			if value > 0 {
				percent := float64(snapshot.Estimate.Tokens) * 100 / float64(value)
				usedPercent = &percent
			}
		}
	}
	if budget == nil {
		out.WriteString("Input budget: unknown.\n")
	} else {
		fmt.Fprintf(&out, "Input budget: %d tokens.\n", *budget)
	}
	if snapshot.Budget.InputTokens == nil {
		out.WriteString("Explicit input budget: not set.\n")
	} else {
		fmt.Fprintf(&out, "Explicit input budget: %d tokens.\n", *snapshot.Budget.InputTokens)
	}
	if snapshot.ModelWindow.Tokens == nil {
		out.WriteString("Model context window: unknown.\n")
	} else {
		fmt.Fprintf(&out, "Model context window: %d tokens (catalog).\n", *snapshot.ModelWindow.Tokens)
	}
	if snapshot.Previous == nil {
		out.WriteString("Previous request: none.\n")
	} else {
		previous := snapshot.Previous
		fmt.Fprintf(&out, "Previous request: %s (%s); prompt tokens %s; cached tokens %s.\n",
			previous.RequestID, previous.Status, formatMaybeInt(previous.PromptTokens), formatMaybeInt(previous.CachedTokens))
	}
	if snapshot.Estimate == nil {
		out.WriteString("Next request estimate: preparing.\n")
	} else {
		fmt.Fprintf(&out, "Next request estimate: about %d tokens (%s).\n", snapshot.Estimate.Tokens, snapshot.Estimate.Stage)
	}
	if remaining == nil {
		out.WriteString("Remaining input budget: unknown.\n")
	} else {
		fmt.Fprintf(&out, "Remaining input budget: %d tokens.\n", *remaining)
	}
	if usedPercent == nil {
		out.WriteString("Estimated budget use: unknown.\n")
	} else {
		fmt.Fprintf(&out, "Estimated budget use: %.1f%%.\n", *usedPercent)
	}
	keys := make([]string, 0, len(runtimeInfo))
	for key := range runtimeInfo {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) != 0 {
		out.WriteString("Runtime information for this request:\n")
		for _, key := range keys {
			fmt.Fprintf(&out, "- %s: %s\n", key, runtimeInfo[key])
		}
	}
	return out.String()
}

func formatMaybeInt(value *int64) string {
	if value == nil {
		return "unknown"
	}
	return fmt.Sprintf("%d", *value)
}

func estimateContextTokens(messagesBytes, toolsBytes int) int64 {
	if messagesBytes < 0 || toolsBytes < 0 || messagesBytes > math.MaxInt-toolsBytes {
		return math.MaxInt64
	}
	bytes := messagesBytes + toolsBytes
	return int64((bytes + 3) / 4)
}

func prepareContextEstimate(state *contextRunState, runID string, messages []json.RawMessage, tools []json.RawMessage) (contextSnapshot, string, error) {
	messageData, err := json.Marshal(messages)
	if err != nil {
		return contextSnapshot{}, "", fmt.Errorf("encode context messages: %w", err)
	}
	toolData, err := json.Marshal(tools)
	if err != nil {
		return contextSnapshot{}, "", fmt.Errorf("encode context tools: %w", err)
	}
	state.baseMessagesBytes = len(messageData)
	state.baseMessageCount = len(messages)
	state.toolsBytes = len(toolData)
	state.prepared = true
	return recalculatePreparedContext(state, runID, time.Now()), "", nil
}

func recalculatePreparedContext(state *contextRunState, runID string, now time.Time) contextSnapshot {
	// The diagnostic message includes its own estimate. Iterate until the
	// integer token estimate stabilizes, which normally takes two passes.
	candidate := int64(0)
	var note string
	for i := 0; i < 8; i++ {
		estimate := &contextEstimate{
			Tokens: candidate, Method: "json_utf8_bytes/4", Stage: "preparing",
			ModelStepID: state.modelStepID, EstimatedAt: now,
		}
		state.estimate = estimate
		snapshot := state.snapshot(runID, now)
		note = contextMessage(snapshot, state.runtimeInfo)
		noteMessage, _ := json.Marshal(map[string]string{"role": "system", "content": note})
		messagesBytes := state.baseMessagesBytes
		if state.baseMessageCount > 0 {
			messagesBytes += 1 // comma before the ephemeral tail message
		}
		messagesBytes += len(noteMessage)
		next := estimateContextTokens(messagesBytes, state.toolsBytes)
		if next == candidate {
			return snapshot
		}
		candidate = next
	}
	state.estimate = &contextEstimate{
		Tokens: candidate, Method: "json_utf8_bytes/4", Stage: "preparing",
		ModelStepID: state.modelStepID, EstimatedAt: now,
	}
	snapshot := state.snapshot(runID, now)
	return snapshot
}

func contextBudgetFromRaw(raw json.RawMessage) (*ContextBudget, error) {
	budget, _, err := parseContextBudget(raw)
	return budget, err
}
