package agent

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func usageTestStats(t *testing.T, messages, tools []json.RawMessage, images map[string]int64) contextHistoryStats {
	t.Helper()
	stats, err := measureContextHistory(messages, tools, images)
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestContextUsageTracksActualRequestAndAppendOnlyPreparation(t *testing.T) {
	var sent provider.Request
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{gen: func(_ context.Context, request provider.Request) (provider.Response, error) {
		sent = request
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"measured answer"}`), FinishReason: "stop",
			Usage: json.RawMessage(`{"input_tokens":3000,"input_tokens_details":{"cached_tokens":2000}}`)}, nil
	}})})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()
	run.context.policyDisabled = true
	run.modelStep = &modelStepState{id: "usage-first"}
	run.context.resetStep(run.modelStep.id)
	tools := []json.RawMessage{json.RawMessage(`{"name":"run_js"}`)}
	prepare := func() contextPrepareResponse {
		t.Helper()
		result, err := h.contextPrepare(op, contextPrepareRequest{RunID: run.id, ModelStepID: run.modelStep.id,
			HistoryRevision: instance.conversation.Revision, Tools: tools, Final: true})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	prepare()
	notice := run.context.preparedNotification.text
	response, err := h.model(t.Context(), op, modelRequest{RunID: run.id, ModelStepID: run.modelStep.id,
		Model: run.config.Model, Effort: run.config.Effort, MaxTokens: run.config.MaxTokens,
		HistoryRevision: instance.conversation.Revision, Notification: &notice, Tools: tools})
	if err != nil {
		t.Fatal(err)
	}
	anchor := run.context.usageAnchor
	if anchor == nil || anchor.promptTokens != 3000 || run.context.pendingUsage != nil {
		t.Fatalf("completed actual request did not establish input anchor: %+v", anchor)
	}
	// Both the sent status message and returned assistant belong to the next
	// transcript. Cached input is included in the 3000-token measurement.
	status, _ := json.Marshal(map[string]string{"role": "user", "content": notice})
	if err := instance.conversation.appendRaw("block", []json.RawMessage{status, response.Message}); err != nil {
		t.Fatal(err)
	}
	run.modelStep = &modelStepState{id: "usage-next"}
	run.context.resetStep(run.modelStep.id)
	prepare()
	if !run.context.usageAnchored || !strings.HasPrefix(run.context.estimate.Method, "provider_usage+") {
		t.Fatalf("append-only preparation ignored actual usage: %+v", run.context.estimate)
	}
	base := instance.conversation.messageView()
	newNotice, _ := json.Marshal(map[string]string{"role": "user", "content": run.context.preparedNotification.text})
	current := usageTestStats(t, append(base, newNotice), tools, nil)
	previous := usageTestStats(t, sent.Messages, tools, nil)
	want := int64(3000) + estimateContextTokens(current.messagesBytes, current.toolsBytes) - estimateContextTokens(previous.messagesBytes, previous.toolsBytes)
	if run.context.estimate.Tokens != want {
		t.Fatalf("estimate=%d, want measured input plus new messages=%d", run.context.estimate.Tokens, want)
	}
	execution := run.context.executionSnapshot()
	if execution.FixedTokens+execution.EstimatedTokens+execution.TransientTokens != want {
		t.Fatalf("trim budget and displayed total disagree: %+v want %d", execution, want)
	}
	cache, offset := run.context.analysisCache, run.context.usageOffset
	prepare()
	if run.context.analysisCache != cache || run.context.usageOffset != offset || run.context.estimate.Tokens != want {
		t.Fatal("repeated preparation changed or double-counted the measured prefix")
	}
	queued := &contextRunState{}
	queued.restoreConversation(instance.conversationContext)
	if queued.usageAnchor != anchor {
		t.Fatal("queued continuation lost the immutable measurement")
	}
	// A changed model must bypass an otherwise matching prepared-analysis cache.
	run.config.Model = "another/model"
	prepare()
	if run.context.usageAnchor != nil || run.context.usageAnchored || strings.HasPrefix(run.context.estimate.Method, "provider_usage+") {
		t.Fatal("model change reused old usage")
	}
}

func TestContextUsagePrefixInvalidationAndImageDeltas(t *testing.T) {
	config := runConfig{Provider: "openai-api", Model: "model", Effort: "high", MaxTokens: 100}
	messages := []json.RawMessage{json.RawMessage(`{"role":"user","content":[{"type":"noe_image","imageId":"old"}]}`)}
	tools := []json.RawMessage{json.RawMessage(`{"name":"one"}`)}
	images := map[string]int64{"old": 120}
	stats := usageTestStats(t, messages, tools, images)
	raw := estimateContextTokens(stats.messagesBytes, stats.toolsBytes) + stats.imageTokens
	anchor := newContextUsageAnchor(config, messages, tools, images, raw)
	anchor.promptTokens = 500
	images["old"] = 999
	validImages := map[string]int64{"old": 120, "new": 240}
	appended := append(cloneContextRawMessages(messages), json.RawMessage(`{"role":"user","content":[{"type":"noe_image","imageId":"new"}]}`))
	offset, ok := anchor.offset(config, appended, tools, validImages)
	if !ok || offset != 500-raw {
		t.Fatal("new image invalidated unchanged measured input or anchor borrowed its map")
	}
	state := contextRunState{usageAnchored: true, usageOffset: offset}
	newStats := usageTestStats(t, appended, tools, validImages)
	want := int64(500) + estimateContextTokens(newStats.messagesBytes, newStats.toolsBytes) - estimateContextTokens(stats.messagesBytes, stats.toolsBytes) + 240
	if got := state.adjustContextTokens(estimateContextTokens(newStats.messagesBytes, newStats.toolsBytes) + newStats.imageTokens); got != want {
		t.Fatalf("new image was lost or counted twice: got %d want %d", got, want)
	}
	for _, field := range []string{"provider", "model", "effort", "maxTokens", "tools", "prefix", "trim", "image"} {
		t.Run(field, func(t *testing.T) {
			cfg, msg, defs, costs := config, appended, tools, validImages
			switch field {
			case "provider":
				cfg.Provider = "different"
			case "model":
				cfg.Model = "different"
			case "effort":
				cfg.Effort = "low"
			case "maxTokens":
				cfg.MaxTokens++
			case "tools":
				defs = []json.RawMessage{json.RawMessage(`{"name":"two"}`)}
			case "prefix":
				msg = append([]json.RawMessage{json.RawMessage(`{"role":"user","content":"replacement"}`)}, appended[1:]...)
			case "trim":
				msg = appended[1:]
			case "image":
				costs = map[string]int64{"old": 121, "new": 240}
			}
			if _, matched := anchor.offset(cfg, msg, defs, costs); matched {
				t.Fatal("changed input reused a measurement")
			}
		})
	}
}

func TestContextUsageDigestHandlesObjectRoundTripsWithoutRoundingNumbers(t *testing.T) {
	left := []json.RawMessage{json.RawMessage(`{"role":"user","content":"a < b","extra":{"b":9007199254740993,"a":1}}`)}
	right := []json.RawMessage{json.RawMessage(`{"extra":{"a":1,"b":9007199254740993},"content":"a \u003c b","role":"user"}`)}
	if contextUsageDigest(left, nil) != contextUsageDigest(right, nil) {
		t.Fatal("object ordering or HTML escaping invalidated equal saved history")
	}
	rounded := []json.RawMessage{json.RawMessage(`{"role":"user","content":"a < b","extra":{"b":9007199254740992,"a":1}}`)}
	if contextUsageDigest(left, nil) == contextUsageDigest(rounded, nil) {
		t.Fatal("distinct large integer values matched after float64 rounding")
	}
}

func TestContextUsageOnlyAcceptsSuccessfulMeasuredInput(t *testing.T) {
	for _, test := range []struct {
		name, status string
		tokens       *int64
		valid        bool
	}{
		{"valid", "completed", new(int64(200)), true},
		{"missing", "completed", nil, false},
		{"zero", "completed", new(int64(0)), false},
		{"negative", "completed", new(int64(-1)), false},
		{"unsafe", "completed", new(int64(math.MaxInt64)), false},
		{"failure", "failed", new(int64(200)), false},
		{"cancelled", "cancelled", new(int64(200)), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			pending := &contextUsageAnchor{estimated: 100}
			state := contextRunState{pendingUsage: pending, pendingUsageRequestID: "actual"}
			state.completeUsageAnchor("other", test.status, &usageSummary{PromptTokens: test.tokens})
			if state.pendingUsage != pending || state.usageAnchor != nil {
				t.Fatal("unrelated completion changed the request's measurement")
			}
			state.completeUsageAnchor("actual", test.status, &usageSummary{PromptTokens: test.tokens})
			if (state.usageAnchor != nil) != test.valid || state.pendingUsage != nil || state.pendingUsageRequestID != "" {
				t.Fatalf("measurement acceptance=%+v", state)
			}
		})
	}
	state := contextRunState{usageAnchored: true, usageOffset: maxSafeContextBudget}
	if got := state.adjustContextTokens(maxSafeContextBudget); got != maxSafeContextBudget {
		t.Fatalf("usage estimate overflowed: %d", got)
	}
	state.usageOffset = -1000
	if got := state.adjustContextTokens(100); got != 0 {
		t.Fatalf("usage estimate became negative: %d", got)
	}
}

func TestContextUsageChangesTrimDecisionAndInvalidatesAfterAcknowledgement(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancel := newConversationTestRun(t, h, op)
	defer cancel()
	for range 3 {
		message, _ := json.Marshal(map[string]string{"role": "assistant", "content": strings.Repeat("history ", 100)})
		if err := instance.conversation.appendRaw("block", []json.RawMessage{message}); err != nil {
			t.Fatal(err)
		}
	}
	messages := instance.conversation.messageView()
	stats := usageTestStats(t, messages, nil, nil)
	run.context.usageAnchor = newContextUsageAnchor(run.config, messages, nil, nil, estimateContextTokens(stats.messagesBytes, stats.toolsBytes))
	run.context.usageAnchor.promptTokens = 9000
	run.context.budget = &ContextBudget{InputTokens: 5000}
	run.modelStep = &modelStepState{id: "usage-trim"}
	run.context.resetStep(run.modelStep.id)
	request := contextPrepareRequest{RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision, Final: true}
	response, err := h.contextPrepare(op, request)
	if err != nil {
		t.Fatal(err)
	}
	if response.Plan == nil || len(response.Plan.RemoveIDs) != 2 || run.context.executionTokens < 5000 {
		t.Fatalf("measured usage did not trigger trimming: plan=%+v execution=%d", response.Plan, run.context.executionTokens)
	}
	candidate := cloneConversationSnapshot(instance.conversation)
	candidate.Entries = append(candidate.Entries[:2], candidate.Entries[len(candidate.Entries)-1])
	candidate.Revision++
	_, err = h.contextPrepare(op, contextPrepareRequest{RunID: run.id, ModelStepID: run.modelStep.id, Final: true,
		AppliedPlanID: response.Plan.ID, Snapshot: candidate, Messages: candidate.messageView(), History: candidate.layout()})
	if err != nil {
		t.Fatal(err)
	}
	if run.context.usageAnchor != nil || run.context.usageAnchored || run.context.estimate.Method != "json_utf8_bytes/4" || run.context.trimCount != 1 {
		t.Fatal("trim retained old usage or failed to acknowledge the existing protocol")
	}
	if run.context.executionSnapshot().EstimatedTokens > 5000 {
		t.Fatal("trimmed history still uses old measured input")
	}
}
