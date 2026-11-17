package agent

import (
	"bytes"
	jsonv1 "encoding/json"
)

// managedContextAnalysis is derived from one immutable managed conversation,
// its exact tools input, and the image estimates used for that preparation.
// weights is owned by the cache and only read by the trimming planner.
type managedContextAnalysis struct {
	stats           contextHistoryStats
	fixedTokens     int64
	executionTokens int64
	weights         []int64
	digest          string
}

// managedContextAnalysisCache is run-owned and keeps one entry. It references
// the already-owned immutable conversation instead of retaining another copy
// of its transcript. Tools and image estimates are copied because they are
// caller or mutable run inputs.
type managedContextAnalysisCache struct {
	conversation   *conversationSnapshot
	revision       int64
	tools          []jsonv1.RawMessage
	imageEstimates map[string]int64
	analysis       managedContextAnalysis
}

func newManagedContextAnalysisCache(
	conversation *conversationSnapshot,
	tools []jsonv1.RawMessage,
	imageEstimates map[string]int64,
	analysis managedContextAnalysis,
) *managedContextAnalysisCache {
	return &managedContextAnalysisCache{
		conversation:   conversation,
		revision:       conversation.Revision,
		tools:          cloneContextRawMessages(tools),
		imageEstimates: cloneImageEstimates(imageEstimates),
		analysis: managedContextAnalysis{
			stats:           analysis.stats,
			fixedTokens:     analysis.fixedTokens,
			executionTokens: analysis.executionTokens,
			weights:         append([]int64(nil), analysis.weights...),
			digest:          analysis.digest,
		},
	}
}

func (c *managedContextAnalysisCache) matches(
	conversation *conversationSnapshot,
	tools []jsonv1.RawMessage,
	imageEstimates map[string]int64,
) bool {
	return c != nil && conversation != nil && c.conversation == conversation && c.revision == conversation.Revision &&
		sameContextRawMessages(c.tools, tools) && sameImageEstimates(c.imageEstimates, imageEstimates)
}

func (c *managedContextAnalysisCache) lookup(
	conversation *conversationSnapshot,
	tools []jsonv1.RawMessage,
	imageEstimates map[string]int64,
) (managedContextAnalysis, bool) {
	if !c.matches(conversation, tools, imageEstimates) {
		return managedContextAnalysis{}, false
	}
	return c.analysis, true
}

func (c *managedContextAnalysisCache) digestFor(
	conversation *conversationSnapshot,
	tools []jsonv1.RawMessage,
	imageEstimates map[string]int64,
) (string, bool) {
	if !c.matches(conversation, tools, imageEstimates) {
		return "", false
	}
	return c.analysis.digest, true
}

func cloneContextRawMessages(values []jsonv1.RawMessage) []jsonv1.RawMessage {
	if values == nil {
		return nil
	}
	copy := make([]jsonv1.RawMessage, len(values))
	for index, value := range values {
		copy[index] = append(jsonv1.RawMessage(nil), value...)
	}
	return copy
}

func sameContextRawMessages(left, right []jsonv1.RawMessage) bool {
	if (left == nil) != (right == nil) || len(left) != len(right) {
		return false
	}
	for index := range left {
		if !bytes.Equal(left[index], right[index]) {
			return false
		}
	}
	return true
}

func cloneImageEstimates(estimates map[string]int64) map[string]int64 {
	if estimates == nil {
		return nil
	}
	copy := make(map[string]int64, len(estimates))
	for id, tokens := range estimates {
		copy[id] = tokens
	}
	return copy
}

func sameImageEstimates(left, right map[string]int64) bool {
	if len(left) != len(right) {
		return false
	}
	for id, tokens := range left {
		other, exists := right[id]
		if !exists || other != tokens {
			return false
		}
	}
	return true
}
