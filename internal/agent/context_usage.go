package agent

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"math/bits"
)

// contextUsageAnchor ties a completed request's measured input to its exact
// transcript prefix and tools. It retains hashes and counts, never message or
// image bytes. Once published, it is immutable and can follow a queued task.
type contextUsageAnchor struct {
	identity       contextUsageIdentity
	digest         string
	messageCount   int
	estimated      int64
	promptTokens   int64
	imageEstimates map[string]int64
}

type contextUsageIdentity struct {
	provider, model, effort string
	maxTokens               int
}

func usageIdentity(config runConfig) contextUsageIdentity {
	return contextUsageIdentity{config.Provider, config.Model, config.Effort, config.MaxTokens}
}

func newContextUsageAnchor(config runConfig, messages, tools []jsonv1.RawMessage, estimates map[string]int64, estimated int64) *contextUsageAnchor {
	return &contextUsageAnchor{
		identity: usageIdentity(config), digest: contextUsageDigest(messages, tools),
		messageCount: len(messages), estimated: safeTokenCount(estimated),
		imageEstimates: cloneImageEstimates(estimates),
	}
}

func (a *contextUsageAnchor) offset(config runConfig, messages, tools []jsonv1.RawMessage, estimates map[string]int64) (int64, bool) {
	if a == nil || a.promptTokens <= 0 || a.digest == "" || a.identity != usageIdentity(config) || a.messageCount == 0 || len(messages) < a.messageCount {
		return 0, false
	}
	for id, tokens := range a.imageEstimates {
		if current, exists := estimates[id]; !exists || current != tokens {
			return 0, false
		}
	}
	if contextUsageDigest(messages[:a.messageCount], tools) != a.digest {
		return 0, false
	}
	return a.promptTokens - a.estimated, true
}

func contextUsageDigest(messages, tools []jsonv1.RawMessage) string {
	value := struct{ Messages, Tools []jsonv1.RawMessage }{messages, tools}
	digest := sha256.New()
	// Go creates the ephemeral status object; JavaScript reconstructs it when
	// saving the block. Member order must not make those equal objects differ.
	// Keep numbers as raw JSON, including integers larger than float64 precision.
	if largeJSONValues(messages, tools) {
		encoded, err := json.Marshal(value, jsontext.ReorderRawObjects(true))
		if err != nil {
			return ""
		}
		_, _ = digest.Write(encoded)
	} else if err := json.MarshalWrite(digest, value, jsontext.ReorderRawObjects(true)); err != nil {
		return ""
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func (s *contextRunState) adjustContextTokens(estimated int64) int64 {
	estimated = safeTokenCount(estimated)
	if !s.usageAnchored {
		return estimated
	}
	if s.usageOffset >= 0 {
		return safeTokenAdd(estimated, s.usageOffset)
	}
	return max(0, estimated+s.usageOffset)
}

func (s *contextRunState) estimateMethod(imageTokens int64) string {
	method := imageEstimateMethod(imageTokens)
	if s.usageAnchored {
		return "provider_usage+" + method
	}
	return method
}

// scaleContextTokens distributes a total using heuristic proportions. Provider
// usage measures a whole request, not individual protected messages or blocks.
func scaleContextTokens(value, from, to int64) int64 {
	if value <= 0 || from <= 0 || to <= 0 {
		return 0
	}
	hi, lo := bits.Mul64(uint64(min(value, from)), uint64(to))
	quotient, _ := bits.Div64(hi, lo, uint64(from))
	return int64(quotient)
}

func (s *contextRunState) calibratedHistoryParts(analysis managedContextAnalysis) (int64, int64, []int64) {
	if !s.usageAnchored {
		return analysis.fixedTokens, analysis.executionTokens, analysis.weights
	}
	raw := safeTokenAdd(estimateContextTokens(analysis.stats.messagesBytes, analysis.stats.toolsBytes), analysis.stats.imageTokens)
	total := s.adjustContextTokens(raw)
	fixed := scaleContextTokens(analysis.fixedTokens, raw, total)
	execution := total - fixed
	var sum int64
	for _, weight := range analysis.weights {
		sum = safeTokenAdd(sum, weight)
	}
	weights := make([]int64, len(analysis.weights))
	var cumulative, previous int64
	for i, weight := range analysis.weights {
		cumulative = safeTokenAdd(cumulative, weight)
		next := scaleContextTokens(cumulative, sum, execution)
		weights[i], previous = next-previous, next
	}
	return fixed, execution, weights
}

func (s *contextRunState) completeUsageAnchor(requestID, status string, usage *usageSummary) {
	if s.pendingUsageRequestID != requestID {
		return
	}
	if status == "completed" && usage != nil && usage.PromptTokens != nil && *usage.PromptTokens > 0 && *usage.PromptTokens <= maxSafeContextBudget && s.pendingUsage != nil {
		completed := *s.pendingUsage
		completed.promptTokens = *usage.PromptTokens
		s.usageAnchor = &completed
	}
	s.pendingUsage, s.pendingUsageRequestID = nil, ""
}
