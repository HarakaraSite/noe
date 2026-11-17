package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

func TestManagedContextAnalysisCacheOwnsToolsAndMatchesExactIdentity(t *testing.T) {
	conversation, err := initialConversation("system", "task")
	if err != nil {
		t.Fatal(err)
	}
	callerTools := []json.RawMessage{json.RawMessage(`{"name":"run_js"}`)}
	imageEstimates := map[string]int64{"image-1": 120}
	analysis := managedContextAnalysis{
		stats:       contextHistoryStats{messagesBytes: 42, messageCount: 2, toolsBytes: 18, imageTokens: 120},
		fixedTokens: 20, executionTokens: 30, weights: []int64{30}, digest: "prepared-digest",
	}
	cache := newManagedContextAnalysisCache(conversation, callerTools, imageEstimates, analysis)
	callerTools[0][2] = 'X'
	imageEstimates["image-1"] = 999
	analysis.weights[0] = -1

	wantTools := []json.RawMessage{json.RawMessage(`{"name":"run_js"}`)}
	wantEstimates := map[string]int64{"image-1": 120}
	got, ok := cache.lookup(conversation, wantTools, wantEstimates)
	if !ok || got.digest != "prepared-digest" || got.stats.imageTokens != 120 || got.weights[0] != 30 {
		t.Fatalf("cache did not retain its owned input/result: got=%+v hit=%t", got, ok)
	}
	if cache.matches(conversation, []json.RawMessage{}, wantEstimates) {
		t.Fatal("nil tools and empty tools unexpectedly matched")
	}
	if cache.matches(conversation, []json.RawMessage{json.RawMessage(`{"name":"other"}`)}, wantEstimates) {
		t.Fatal("changed tools unexpectedly matched")
	}
	if cache.matches(conversation, wantTools, map[string]int64{"image-1": 121}) {
		t.Fatal("changed image estimate unexpectedly matched")
	}
	if cache.matches(conversation, wantTools, map[string]int64{"other-image": 0}) {
		t.Fatal("different zero-valued image estimate key unexpectedly matched")
	}
	if cache.matches(cloneConversationSnapshot(conversation), wantTools, wantEstimates) {
		t.Fatal("distinct snapshot with the same numeric revision unexpectedly matched")
	}
	nilToolsCache := newManagedContextAnalysisCache(conversation, nil, wantEstimates, analysis)
	if !nilToolsCache.matches(conversation, nil, wantEstimates) || nilToolsCache.matches(conversation, []json.RawMessage{}, wantEstimates) {
		t.Fatal("nil tools did not remain distinct from empty tools")
	}
	zeroEstimateCache := newManagedContextAnalysisCache(conversation, nil, map[string]int64{"image-1": 0}, analysis)
	if zeroEstimateCache.matches(conversation, nil, map[string]int64{"other-image": 0}) {
		t.Fatal("equal-length zero-valued estimate maps with different keys unexpectedly matched")
	}
	conversation.Revision++
	if cache.matches(conversation, wantTools, wantEstimates) {
		t.Fatal("changed revision on the same snapshot unexpectedly matched")
	}
}

func TestManagedContextPrepareReusesAndInvalidatesAnalysis(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()

	instance.images = map[string]*imageapi.Data{
		"image-1": {Metadata: imageapi.Metadata{ImageID: "image-1", Width: 320, Height: 320, MIMEType: "image/png"}},
	}
	if err := instance.conversation.append("protected", map[string]any{
		"role": "user", "content": []map[string]string{{"type": "noe_image", "imageId": "image-1"}},
	}); err != nil {
		t.Fatal(err)
	}
	run.modelStep = &modelStepState{id: "cache-step"}
	run.context.resetStep("cache-step")
	run.context.policyDisabled = true
	tools := []json.RawMessage{json.RawMessage(`{"name":"run_js"}`)}
	request := contextPrepareRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision,
		Tools: tools, Final: true,
	}
	prepare := func() {
		t.Helper()
		if _, err := h.contextPrepare(op, request); err != nil {
			t.Fatalf("managed context preparation failed: %v", err)
		}
	}

	prepare()
	first := run.context.analysisCache
	if first == nil || first.analysis.stats.imageTokens != 120 || run.context.preparedDigest == "" {
		t.Fatalf("first preparation did not cache complete analysis: %+v", first)
	}
	originalDigest := first.analysis.digest
	first.analysis.digest = "reuse-sentinel"
	prepare()
	if run.context.analysisCache != first || run.context.preparedDigest != "reuse-sentinel" {
		t.Fatal("same managed snapshot/tools/image estimates did not reuse cached analysis")
	}
	first.analysis.digest = originalDigest

	request.Tools = []json.RawMessage{json.RawMessage(`{"name":"other"}`)}
	prepare()
	second := run.context.analysisCache
	if second == first || string(second.tools[0]) != `{"name":"other"}` {
		t.Fatal("changed tools did not replace the cache with an owned key")
	}
	request.Tools[0][2] = 'X'
	if string(second.tools[0]) != `{"name":"other"}` {
		t.Fatal("caller mutation changed the cached tools key")
	}
	request.Tools = []json.RawMessage{json.RawMessage(`{"name":"other"}`)}

	oldRevision := instance.conversation.Revision
	h.mu.Lock()
	instance.conversation = cloneConversationSnapshot(instance.conversation)
	h.mu.Unlock()
	request.HistoryRevision = instance.conversation.Revision
	prepare()
	third := run.context.analysisCache
	if third == second || third.revision != oldRevision {
		t.Fatal("new authoritative snapshot with the same revision reused old analysis")
	}

	if err := instance.conversation.appendProtected("another turn"); err != nil {
		t.Fatal(err)
	}
	request.HistoryRevision = instance.conversation.Revision
	prepare()
	fourth := run.context.analysisCache
	if fourth == third || fourth.revision != oldRevision+1 {
		t.Fatal("appended managed revision reused old analysis")
	}

	instance.images["image-1"].Width = 640
	prepare()
	fifth := run.context.analysisCache
	if fifth == fourth || fifth.analysis.stats.imageTokens != 240 {
		t.Fatalf("changed image estimate was reused: old=%+v new=%+v", fourth.analysis.stats, fifth.analysis.stats)
	}

	invalid := request
	invalid.HistoryRevision = 0
	invalid.Messages = []json.RawMessage{json.RawMessage(`{"role":`)}
	cacheBeforeInvalid := run.context.analysisCache
	if _, err := h.contextPrepare(op, invalid); err == nil {
		t.Fatal("invalid explicit message was accepted")
	} else {
		var operationErr *operationError
		if !errors.As(err, &operationErr) || operationErr.code != "request" {
			t.Fatalf("invalid explicit message error = %v", err)
		}
	}
	if run.context.analysisCache != cacheBeforeInvalid {
		t.Fatal("invalid explicit input changed the managed cache")
	}

	// Explicit message requests remain uncached and still verify their full
	// prepared transcript when model admission is attempted.
	explicitMessages := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"explicit system"}`),
		json.RawMessage(`{"role":"user","content":"explicit task"}`),
	}
	explicitPrepare := contextPrepareRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, Messages: explicitMessages, Tools: request.Tools, Final: true,
	}
	if _, err := h.contextPrepare(op, explicitPrepare); err != nil {
		t.Fatalf("explicit preparation failed: %v", err)
	}
	if run.context.analysisCache != nil {
		t.Fatal("explicit history retained the managed cache")
	}
	explicitNotification := run.context.preparedNotification.text
	changedMessages := []json.RawMessage{
		json.RawMessage(`{"role":"system","content":"changed system"}`),
		json.RawMessage(`{"role":"user","content":"explicit task"}`),
		json.RawMessage(mustJSON(t, map[string]string{"role": "user", "content": explicitNotification})),
	}
	if _, err := h.model(context.Background(), op, modelRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, Model: run.config.Model, Messages: changedMessages,
		MaxTokens: run.config.MaxTokens, Effort: run.config.Effort, Tools: request.Tools,
	}); err == nil || !containsError(err, "model transcript differs from prepared context") {
		t.Fatalf("changed unmanaged transcript passed prepared digest verification: %v", err)
	}
}

func TestManagedModelReusesDigestOnlyForMatchingManagedInputs(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()
	run.modelStep = &modelStepState{id: "digest-step"}
	run.context.resetStep("digest-step")
	run.context.policyDisabled = true
	tools := []json.RawMessage{json.RawMessage(`{"name":"run_js"}`)}
	request := contextPrepareRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision,
		Tools: tools, Final: true,
	}
	if _, err := h.contextPrepare(op, request); err != nil {
		t.Fatal(err)
	}
	notification := run.context.preparedNotification.text
	model := modelRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision,
		Notification: &notification, Model: run.config.Model, MaxTokens: run.config.MaxTokens,
		Effort: run.config.Effort, Tools: tools,
	}
	run.context.analysisCache.analysis.digest = "reused-cache-sentinel"
	if _, err := h.model(context.Background(), op, model); err == nil || !containsError(err, "model transcript differs from prepared context") {
		t.Fatalf("managed admission did not reuse the matching cached digest: %v", err)
	}

	// A different tools input must bypass the cached digest and fail the normal
	// prepared transcript comparison.
	model.Tools = []json.RawMessage{json.RawMessage(`{"name":"changed"}`)}
	if _, err := h.model(context.Background(), op, model); err == nil || !containsError(err, "model transcript differs from prepared context") {
		t.Fatalf("changed tools unexpectedly passed prepared transcript validation: %v", err)
	}
}

func TestFinishReleasesManagedContextAnalysisCache(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()
	run.modelStep = &modelStepState{id: "finish-cache-step"}
	run.context.resetStep("finish-cache-step")
	run.context.policyDisabled = true
	if _, err := h.contextPrepare(op, contextPrepareRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision, Final: true,
	}); err != nil {
		t.Fatal(err)
	}
	if run.context.analysisCache == nil {
		t.Fatal("managed preparation did not populate the cache")
	}
	if _, err := h.finish(context.Background(), op, finishRequest{RunID: run.id, Status: "failed"}); err != nil {
		t.Fatal(err)
	}
	if run.context.analysisCache != nil {
		t.Fatal("finished run retained its managed analysis cache")
	}
}

func TestTaskOutcomeReleasesManagedContextAnalysisCacheWithoutFinish(t *testing.T) {
	h := NewHost(Options{})
	t.Cleanup(h.Close)
	op := h.NewOperation(t.Context())
	t.Cleanup(op.Close)
	instance, run, cancelRun := newConversationTestRun(t, h, op)
	defer cancelRun()
	run.modelStep = &modelStepState{id: "task-outcome-cache-step"}
	run.context.resetStep("task-outcome-cache-step")
	run.context.policyDisabled = true
	if _, err := h.contextPrepare(op, contextPrepareRequest{
		RunID: run.id, ModelStepID: run.modelStep.id, HistoryRevision: instance.conversation.Revision, Final: true,
	}); err != nil {
		t.Fatal(err)
	}
	if run.context.analysisCache == nil {
		t.Fatal("managed preparation did not populate the cache")
	}
	h.mu.Lock()
	op.task.commitOutcomeLocked(Result{ExitCode: 1, Reason: "failed"}, errors.New("task exited without finish"))
	h.mu.Unlock()
	if run.context.analysisCache != nil {
		t.Fatal("failed task outcome retained its managed analysis cache")
	}
}

func containsError(err error, text string) bool {
	return err != nil && strings.Contains(err.Error(), text)
}
