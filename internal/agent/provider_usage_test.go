package agent

import (
	"encoding/json"
	"testing"
)

func TestResponsesUsageUsesCommonObservationFields(t *testing.T) {
	summary := summarizeUsage(json.RawMessage(`{"input_tokens":15,"output_tokens":3,"total_tokens":18,"input_tokens_details":{"cached_tokens":10}}`))
	if summary == nil || summary.PromptTokens == nil || *summary.PromptTokens != 15 || summary.CompletionTokens == nil || *summary.CompletionTokens != 3 || summary.CachedTokens == nil || *summary.CachedTokens != 10 {
		t.Fatalf("usage not normalized: %+v", summary)
	}
}
