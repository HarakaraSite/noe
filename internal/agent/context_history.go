package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type contextHistoryEntry struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Count int    `json:"count"`
}

type contextHistoryLayout struct {
	Revision int64                 `json:"revision"`
	Entries  []contextHistoryEntry `json:"entries"`
}

type contextTrimPlan struct {
	ID        string   `json:"id"`
	Revision  int64    `json:"revision"`
	RemoveIDs []string `json:"removeIds"`
}

// Pending plans retain IDs and aggregates, never a second transcript.
type pendingContextTrim struct {
	plan          contextTrimPlan
	afterDigest   string
	afterLayout   contextHistoryLayout
	beforeTokens  int64
	removedBlocks int
}

type contextTrimSnapshot struct {
	ID            string `json:"id"`
	BeforeTokens  int64  `json:"beforeTokens"`
	AfterTokens   int64  `json:"afterTokens"`
	RemovedBlocks int    `json:"removedBlocks"`
}

type executionContextSnapshot struct {
	Policy                 string               `json:"policy"`
	BudgetSource           string               `json:"budgetSource"`
	FixedTokens            int64                `json:"fixedTokens"`
	TransientTokens        int64                `json:"transientTokens"`
	ResponseReserveTokens  *int64               `json:"responseReserveTokens"`
	ResponseReserveUnknown bool                 `json:"responseReserveUnknown"`
	EstimatedTokens        int64                `json:"estimatedTokens"`
	BudgetTokens           int64                `json:"budgetTokens"`
	TotalInputBudgetTokens int64                `json:"totalInputBudgetTokens"`
	UsedPercent            *float64             `json:"usedPercent"`
	NearBudget             bool                 `json:"nearBudget"`
	BudgetExceeded         bool                 `json:"budgetExceeded"`
	TargetUnreachable      bool                 `json:"targetUnreachable"`
	TrimCount              int                  `json:"trimCount"`
	LastTrim               *contextTrimSnapshot `json:"lastTrim"`
	PendingTrim            *contextTrimSnapshot `json:"pendingTrim,omitempty"`
}

func (s *contextRunState) executionSnapshot() *executionContextSnapshot {
	if !s.prepared {
		return nil
	}
	transient := int64(0)
	if s.estimate != nil {
		transient = max(0, s.estimate.Tokens-estimateContextTokens(s.baseMessagesBytes, s.toolsBytes)-s.baseImageTokens)
	}
	budget := resolveExecutionBudget(s.budget, s.modelWindow, s.maxTokens, s.catalogMaxOutput, s.policy, s.fixedTokens, transient)
	policy := "auto"
	if s.policyDisabled {
		policy = "disabled"
	}
	result := &executionContextSnapshot{
		Policy: policy, BudgetSource: budget.BudgetSource, FixedTokens: s.fixedTokens, TransientTokens: transient,
		ResponseReserveTokens: cloneInt64(budget.ResponseReserveTokens), ResponseReserveUnknown: budget.ResponseReserveUnknown,
		EstimatedTokens: s.executionTokens, BudgetTokens: budget.ExecutionTokens, TotalInputBudgetTokens: budget.TotalInputTokens,
		BudgetExceeded:    s.executionTokens > budget.ExecutionTokens || s.fixedTokens+transient > budget.TotalInputTokens,
		TargetUnreachable: s.targetUnreachable, TrimCount: s.trimCount,
	}
	if s.modelWindow != nil {
		reserve := int64(0)
		if budget.ResponseReserveTokens != nil {
			reserve = *budget.ResponseReserveTokens
		}
		result.BudgetExceeded = result.BudgetExceeded || s.fixedTokens+transient+reserve > *s.modelWindow
	}
	result.TargetUnreachable = result.TargetUnreachable || (result.BudgetExceeded && s.executionTokens == 0)
	if budget.ExecutionTokens > 0 {
		percent := float64(s.executionTokens) * 100 / float64(budget.ExecutionTokens)
		result.UsedPercent = &percent
		result.NearBudget = percent >= 80
	} else {
		result.NearBudget = result.BudgetExceeded
	}
	if s.lastTrim != nil {
		copy := *s.lastTrim
		result.LastTrim = &copy
	}
	if s.pendingNotice != nil {
		copy := *s.pendingNotice
		result.PendingTrim = &copy
	}
	return result
}

func contextHistoryDigest(messages, tools []json.RawMessage) string {
	encoded, _ := json.Marshal(struct {
		Messages []json.RawMessage
		Tools    []json.RawMessage
	}{messages, tools})
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func historyParts(messages []json.RawMessage, tools []json.RawMessage, layout *contextHistoryLayout, imageEstimates ...map[string]int64) (int64, int64, []int64, error) {
	var estimates map[string]int64
	if len(imageEstimates) > 0 {
		estimates = imageEstimates[0]
	}
	stats, err := measureContextHistory(messages, tools, estimates)
	if err != nil {
		return 0, 0, nil, err
	}
	return historyPartsFromStats(messages, layout, estimates, stats)
}

func historyPartsFromStats(messages []json.RawMessage, layout *contextHistoryLayout, estimates map[string]int64, stats contextHistoryStats) (int64, int64, []int64, error) {
	if layout == nil {
		// Old internal callers only supply a transcript, without deletion metadata.
		// Treat it as protected rather than guessing ownership from roles.
		return estimateContextTokens(stats.messagesBytes, stats.toolsBytes) + stats.imageTokens, 0, nil, nil
	}
	if layout.Revision <= 0 || len(layout.Entries) < 2 {
		return 0, 0, nil, fmt.Errorf("invalid context history layout")
	}
	protected := []json.RawMessage{}
	weights := []int64{}
	seen := make(map[string]bool)
	offset := 0
	for _, entry := range layout.Entries {
		if entry.ID == "" || seen[entry.ID] || entry.Count <= 0 || entry.Count > len(messages)-offset || (entry.Kind != "protected" && entry.Kind != "block") {
			return 0, 0, nil, fmt.Errorf("invalid context history entry")
		}
		seen[entry.ID] = true
		group := messages[offset : offset+entry.Count]
		if entry.Kind == "protected" {
			protected = append(protected, group...)
		} else {
			encoded, _ := json.Marshal(group)
			weights = append(weights, estimateContextTokens(len(encoded)-1, 0)+historyImageTokens(group, estimates))
		}
		offset += entry.Count
	}
	if offset != len(messages) {
		return 0, 0, nil, fmt.Errorf("context history count mismatch")
	}
	fixedData, _ := json.Marshal(protected)
	fixed := estimateContextTokens(len(fixedData), stats.toolsBytes) + historyImageTokens(protected, estimates)
	execution := max(0, estimateContextTokens(stats.messagesBytes, stats.toolsBytes)+stats.imageTokens-fixed)
	return fixed, execution, weights, nil
}

func (s *contextRunState) planTrim(messages, tools []json.RawMessage, layout *contextHistoryLayout) (*contextTrimPlan, error) {
	if layout == nil || s.policyDisabled {
		return nil, nil
	}
	_, _, weights, err := historyParts(messages, tools, layout, s.imageEstimates)
	if err != nil {
		return nil, err
	}
	return s.planTrimWithWeights(messages, tools, layout, weights)
}

func (s *contextRunState) planTrimWithWeights(messages, tools []json.RawMessage, layout *contextHistoryLayout, weights []int64) (*contextTrimPlan, error) {
	if layout == nil || s.policyDisabled {
		return nil, nil
	}
	snapshot := s.executionSnapshot()
	if snapshot == nil || s.executionTokens <= 0 || s.executionTokens < snapshot.BudgetTokens {
		s.targetUnreachable = false
		return nil, nil
	}
	target := int64(float64(s.executionTokens) * effectiveContextRetainRatio(s.policy))
	keep := int64(0)
	removeCount := len(weights)
	for i := len(weights) - 1; i >= 0; i-- {
		if i < len(weights)-1 && keep+weights[i] > target {
			break
		}
		keep += weights[i]
		removeCount = i
	}
	s.targetUnreachable = keep > target || snapshot.BudgetTokens == 0
	if removeCount <= 0 {
		return nil, nil
	}
	id, err := randomID()
	if err != nil {
		return nil, err
	}
	plan := contextTrimPlan{ID: id, Revision: layout.Revision}
	keptLayout := contextHistoryLayout{Revision: layout.Revision + 1}
	keptMessages := []json.RawMessage{}
	offset, blocks := 0, 0
	for _, entry := range layout.Entries {
		remove := entry.Kind == "block" && blocks < removeCount
		if entry.Kind == "block" {
			blocks++
		}
		if remove {
			plan.RemoveIDs = append(plan.RemoveIDs, entry.ID)
		} else {
			keptLayout.Entries = append(keptLayout.Entries, entry)
			keptMessages = append(keptMessages, messages[offset:offset+entry.Count]...)
		}
		offset += entry.Count
	}
	s.pendingPlan = &pendingContextTrim{plan: plan, afterDigest: contextHistoryDigest(keptMessages, tools), afterLayout: keptLayout, beforeTokens: s.executionTokens, removedBlocks: removeCount}
	return &plan, nil
}

func (s *contextRunState) acknowledgeTrim(planID string, layout *contextHistoryLayout, messages, tools []json.RawMessage) (*contextTrimSnapshot, error) {
	pending := s.pendingPlan
	if pending == nil || planID != pending.plan.ID || layout == nil || layout.Revision != pending.afterLayout.Revision || contextHistoryDigest(messages, tools) != pending.afterDigest {
		return nil, fmt.Errorf("stale or invalid context trim acknowledgement")
	}
	encoded, _ := json.Marshal(layout.Entries)
	expected, _ := json.Marshal(pending.afterLayout.Entries)
	if string(encoded) != string(expected) {
		return nil, fmt.Errorf("context trim acknowledgement layout mismatch")
	}
	result := &contextTrimSnapshot{ID: planID, BeforeTokens: pending.beforeTokens, AfterTokens: s.executionTokens, RemovedBlocks: pending.removedBlocks}
	s.trimCount++
	s.lastTrim = result
	if s.pendingNotice == nil {
		copy := *result
		s.pendingNotice = &copy
	} else {
		s.pendingNotice.AfterTokens = result.AfterTokens
		s.pendingNotice.RemovedBlocks += result.RemovedBlocks
	}
	s.pendingTrimActivityIDs = appendUniqueActivityIDs(s.pendingTrimActivityIDs, planID)
	s.pendingPlan = nil
	return result, nil
}

func (h *Host) emitContextTrimmed(run *agentRun, snapshot contextSnapshot, trim contextTrimSnapshot, stepID string, causes []string) {
	h.observeRun("context.trimmed", run.id, run, map[string]any{
		"context": snapshot, "modelStepId": stepID, "activityId": trim.ID, "causedBy": causes,
		"origin":       activityOrigin{Kind: "context", RunID: run.id, AgentID: run.agentID(), ModelStepID: stepID},
		"beforeTokens": trim.BeforeTokens, "afterTokens": trim.AfterTokens, "removedBlocks": trim.RemovedBlocks,
		"reason": "input_budget", "budgetSource": snapshot.ExecutionContext.BudgetSource, "appliedAt": time.Now(),
	})
}
