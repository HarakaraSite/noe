package agent

import (
	"bytes"
	"context"
	"encoding/json"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 3))
	img.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	return encoded.Bytes()
}

func imageFixture(t *testing.T) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			c := color.RGBA{255, 0, 0, 255}
			if x >= 64 {
				c = color.RGBA{0, 0, 255, 255}
			}
			img.Set(x, y, c)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "image.dat")
	if err := os.WriteFile(path, b.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, b.Bytes()
}
func imageRunFixture(t *testing.T) (*Host, *Operation, *Operation, *agentRun) {
	t.Helper()
	h := NewHost(Options{Models: modelsWithChat(&fixtureProvider{})})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	child := h.NewOperation(op.ctx)
	ctx, cancel := context.WithCancel(op.ctx)
	run := &agentRun{id: "image-run", operation: op, ctx: ctx, cancel: cancel, modelStep: &modelStepState{id: "step", responseComplete: true, toolCalls: map[string]*toolCallState{"tool": {name: "run_js", started: true}}, toolCallOrder: []string{"tool"}}}
	h.runs[run.id] = run
	child.imageOwner = &imageOwner{runID: run.id, stepID: "step", toolID: "tool"}
	return h, op, child, run
}
func TestImageAttachmentsSurviveChildCloseAndCommit(t *testing.T) {
	path, original := imageFixture(t)
	h, op, child, run := imageRunFixture(t)
	meta, err := child.ReadImage(context.Background(), path, "low", 1, func(_ context.Context, selected string) ([]byte, error) {
		if selected != path {
			t.Fatalf("reader path=%q", selected)
		}
		return os.ReadFile(selected)
	})
	if err != nil {
		t.Fatal(err)
	}
	if meta.ImageID == "" || meta.Detail != "low" {
		t.Fatalf("metadata=%+v", meta)
	}
	child.Close()
	os.WriteFile(path, []byte("overwritten"), 0600)
	raw := h.finishToolImages(run, "step", "tool", true)
	if len(raw) != 1 || raw[0] != meta {
		t.Fatalf("child close lost pending image: %+v", raw)
	}
	h.mu.Lock()
	tool := run.modelStep.toolCalls["tool"]
	tool.images, _ = selectedToolImages(tool, raw, raw)
	tool.resultRecorded = true
	run.modelStep.finished = true
	entry := conversationEntry{ID: "history-3", Kind: "block", Messages: []json.RawMessage{json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[{"id":"tool"}]}`), json.RawMessage(`{"role":"tool","tool_call_id":"tool","content":"{}"}`)}}
	suffix, _ := json.Marshal(map[string]any{"role": "user", "content": []any{map[string]string{"type": "noe_image", "imageId": meta.ImageID}}})
	entry.Messages = append(entry.Messages, suffix)
	if err := validateConversationBlock(entry.Messages); err != nil {
		t.Fatal(err)
	}
	if err := commitStepImagesLocked(run, entry); err != nil {
		t.Fatal(err)
	}
	expanded, err := h.expandImagesLocked(run, entry.Messages)
	h.mu.Unlock()
	if err != nil || !strings.Contains(string(expanded[2]), "data:image/png;base64,") || strings.Contains(string(expanded[2]), "imageId") || strings.Contains(string(expanded[2]), "noe_image") {
		t.Fatalf("expand=%s err=%v", expanded, err)
	}
	if !bytes.Equal(run.images[meta.ImageID].Content, original) {
		t.Fatal("image snapshot changed with file")
	}
	other := &agentRun{id: "other", operation: op}
	h.mu.Lock()
	_, err = h.expandImagesLocked(other, entry.Messages)
	h.mu.Unlock()
	if err == nil {
		t.Fatal("foreign run accessed image")
	}
	h.mu.Lock()
	reconcileImagesLocked(run, []json.RawMessage{entry.Messages[0]})
	remaining := len(run.images)
	h.mu.Unlock()
	if remaining != 0 {
		t.Fatal("confirmed trim kept removed image")
	}
}
func TestImageAdmissionRejectsRootAndAbortedLateReads(t *testing.T) {
	path, _ := imageFixture(t)
	h, op, child, run := imageRunFixture(t)
	read := func(_ context.Context, selected string) ([]byte, error) { return os.ReadFile(selected) }
	if _, err := op.ReadImage(context.Background(), path, "auto", 1, read); imageErrorCode(err) != "input" {
		t.Fatalf("root image read error=%v code=%q", err, imageErrorCode(err))
	}
	if _, err := child.ReadImage(context.Background(), path, "auto", 1, read); err != nil {
		t.Fatalf("owned image read: %v", err)
	}
	_, err := op.Call("discard_images", `{"runId":"image-run","modelStepId":"step"}`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = child.ReadImage(context.Background(), path, "auto", 2, read)
	if imageErrorCode(err) != "input" {
		t.Fatalf("late image read error=%v code=%q", err, imageErrorCode(err))
	}
	if len(run.modelStep.toolCalls["tool"].rawImages) != 0 {
		t.Fatal("aborted bytes retained")
	}
	if got := h.finishToolImages(run, "step", "tool", true); len(got) != 0 {
		t.Fatal("abort was undone")
	}
}
func TestImagePhysicalFailureAndHookSelection(t *testing.T) {
	_, _, _, run := imageRunFixture(t)
	tool := run.modelStep.toolCalls["tool"]
	a := &imageapi.Data{Metadata: imageapi.Metadata{ImageID: "a"}}
	b := &imageapi.Data{Metadata: imageapi.Metadata{ImageID: "b"}}
	tool.rawImages = []*imageapi.Data{a, b}
	for _, selected := range [][]imageapi.Metadata{{a.Metadata, a.Metadata}, {{ImageID: "foreign"}}, {b.Metadata, a.Metadata}} {
		if _, err := selectedToolImages(tool, imageMetadata(tool.rawImages), selected); err == nil {
			t.Fatalf("accepted %+v", selected)
		}
	}
	selected, err := selectedToolImages(tool, imageMetadata(tool.rawImages), []imageapi.Metadata{b.Metadata})
	if err != nil || len(selected) != 1 || selected[0] != b {
		t.Fatalf("selection=%+v err=%v", selected, err)
	}
	if err := commitStepImagesLocked(run, conversationEntry{}); err != nil {
		t.Fatal(err)
	}
	if len(tool.rawImages) != 0 {
		t.Fatal("unselected image bytes retained")
	}

}
func TestImageContextEstimateIgnoresEncodedSize(t *testing.T) {
	data := &imageapi.Data{Metadata: imageapi.Metadata{ImageID: "a", Width: 1024, Height: 1024, Detail: "high"}, Content: make([]byte, 100000)}
	if estimateImageTokens(data) != 1229 {
		t.Fatal(estimateImageTokens(data))
	}
	data.Detail = "low"
	if estimateImageTokens(data) != 308 {
		t.Fatal(estimateImageTokens(data))
	}
	messages := []json.RawMessage{json.RawMessage(`{"role":"system","content":"fixed"}`), json.RawMessage(`{"role":"user","content":"task"}`), json.RawMessage(`{"role":"assistant","content":"text"}`), json.RawMessage(`{"role":"user","content":[{"type":"noe_image","imageId":"a"}]}`)}
	layout := &contextHistoryLayout{Revision: 3, Entries: []contextHistoryEntry{{ID: "1", Kind: "protected", Count: 1}, {ID: "2", Kind: "protected", Count: 1}, {ID: "3", Kind: "block", Count: 2}}}
	fixed, execution, weights, err := historyParts(messages, nil, layout, map[string]int64{"a": 308})
	if err != nil {
		t.Fatal(err)
	}
	f0, e0, w0, _ := historyParts(messages, nil, layout)
	if fixed != f0 || execution-e0 != 308 || weights[0]-w0[0] != 308 {
		t.Fatalf("image estimate %d %d %v", fixed, execution, weights)
	}
	state := contextRunState{imageEstimates: map[string]int64{"a": 308}}
	snapshot, _, err := prepareContextEstimate(&state, "r", messages, nil)
	if err != nil || snapshot.Estimate.ImageTokens != 308 || !strings.Contains(snapshot.Estimate.Method, "fallback") {
		t.Fatalf("estimate=%+v err=%v", snapshot.Estimate, err)
	}
}

func TestImageConcurrentRegistrationUsesInvocationOrder(t *testing.T) {
	path, _ := imageFixture(t)
	h, _, child, run := imageRunFixture(t)
	// Broker arrival can be out of order: the native boundary launches one
	// goroutine per request. JS ordinals define the public call order.
	for _, ordinal := range []int{2, 1} {
		_, err := child.ReadImage(context.Background(), path, "auto", ordinal, func(_ context.Context, selected string) ([]byte, error) {
			return os.ReadFile(selected)
		})
		if err != nil {
			t.Fatalf("ordinal=%d image read: %v", ordinal, err)
		}
	}
	h.mu.Lock()
	tool := run.modelStep.toolCalls["tool"]
	if len(tool.rawImages) != 2 || tool.rawImages[0].Ordinal != 1 || tool.rawImages[1].Ordinal != 2 {
		t.Fatal("registration order changed")
	}
	h.mu.Unlock()
	child.Close()
	if len(h.finishToolImages(run, "step", "tool", false)) != 0 || len(tool.rawImages) != 0 {
		t.Fatal("physical failure retained candidates")
	}
}

func TestImageCheckpointCommitsHostStateAndRetainsImage(t *testing.T) {
	path, _ := imageFixture(t)
	h, op, child, run := imageRunFixture(t)
	initial, err := initialConversation("system", "task")
	if err != nil {
		t.Fatal(err)
	}
	instance := &agentInstance{id: "instance", conversation: initial}
	run.instance = instance
	op.task = &managedTask{instance: instance, run: run}
	meta, err := child.ReadImage(context.Background(), path, "auto", 1, func(_ context.Context, selected string) ([]byte, error) {
		return os.ReadFile(selected)
	})
	if err != nil {
		t.Fatal(err)
	}
	child.Close()
	h.finishToolImages(run, "step", "tool", true)
	h.mu.Lock()
	call := run.modelStep.toolCalls["tool"]
	call.images = append([]*imageapi.Data(nil), call.rawImages...)
	call.resultRecorded = true
	run.modelStep.finished = true
	h.mu.Unlock()
	messages := []json.RawMessage{json.RawMessage(`{"role":"assistant","content":null,"tool_calls":[{"id":"tool"}]}`), json.RawMessage(`{"role":"tool","tool_call_id":"tool","content":"{}"}`)}
	imageMessage, _ := json.Marshal(map[string]any{"role": "user", "content": []any{map[string]string{"type": "noe_image", "imageId": meta.ImageID}}})
	messages = append(messages, imageMessage)
	next := cloneConversationSnapshot(initial)
	if err := next.appendRaw("block", messages); err != nil {
		t.Fatal(err)
	}
	if _, err := h.conversationCheckpoint(op, conversationCheckpointRequest{RunID: run.id, Snapshot: next}); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !conversationSnapshotEqual(instance.conversation, next) || instance.images[meta.ImageID] == nil || !run.modelStep.imagesCommitted {
		t.Fatal("lost acknowledgement undid committed image exchange")
	}
	discardStepImages(run.modelStep)
	if instance.images[meta.ImageID] == nil {
		t.Fatal("batch cleanup freed committed conversation image")
	}
}
