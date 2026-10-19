package agent

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"net/http"
	"path/filepath"
	"sort"
)

// imageOwner is granted only to a physical run_js scope. Hook and task scopes
// cannot attach images on behalf of another model judgment.
type imageOwner struct{ runID, stepID, toolID string }

func writeImageError(w http.ResponseWriter, err error) {
	writeJSON(w, map[string]any{"imageError": imageapi.AsError(err)})
}
func imageInput(message string) error { return &imageapi.Error{Code: "input", Message: message} }

func (b *broker) handleReadImage(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request struct {
		Path    string `json:"path"`
		Detail  string `json:"detail"`
		Ordinal int    `json:"ordinal"`
	}
	if decodeRequest(r, &request) != nil || request.Path == "" || request.Ordinal <= 0 {
		writeImageError(w, imageInput("path is required"))
		return
	}
	h := b.host
	h.mu.Lock()
	owner := op.imageOwner
	run, tool := h.admitImageLocked(op)
	if tool != nil {
		if tool.imageOrders == nil {
			tool.imageOrders = map[int]bool{}
		}
		if tool.imageOrders[request.Ordinal] {
			tool = nil
		} else {
			tool.imageOrders[request.Ordinal] = true
		}
	}
	h.mu.Unlock()
	if run == nil || tool == nil {
		writeImageError(w, imageInput("read_image requires an active run_js model tool call"))
		return
	}
	path := request.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(h.opts.Cwd, path)
	}
	ctx, cancel := context.WithCancel(op.ctx)
	stop := context.AfterFunc(r.Context(), cancel)
	defer func() { stop(); cancel() }()
	data, err := imageapi.ReadFile(ctx, path, request.Detail)
	if err != nil {
		writeImageError(w, err)
		return
	}
	id, err := randomID()
	if err != nil {
		writeImageError(w, err)
		return
	}
	data.ImageID = id
	data.Ordinal = request.Ordinal
	h.mu.Lock()
	current, call := h.admitImageLocked(op)
	if current != run || call != tool || ctx.Err() != nil {
		h.mu.Unlock()
		writeImageError(w, &imageapi.Error{Code: "cancelled", Message: "image attachment scope has ended"})
		return
	}
	tool.rawImages = append(tool.rawImages, data)
	sort.Slice(tool.rawImages, func(i, j int) bool { return tool.rawImages[i].Ordinal < tool.rawImages[j].Ordinal })
	h.mu.Unlock()
	h.observeRun("image.read", run.id, run, map[string]any{"path": data.Path, "modelStepId": owner.stepID, "toolCallId": owner.toolID, "image": data.Metadata})
	writeJSON(w, data.Metadata)
}

func (h *Host) admitImageLocked(op *Operation) (*agentRun, *toolCallState) {
	if op == nil || op.imageOwner == nil || op.closed || op.ctx.Err() != nil || h.closed {
		return nil, nil
	}
	owner := op.imageOwner
	run := h.runs[owner.runID]
	if run == nil || run.ctx.Err() != nil || run.interrupting || run.finalClosed || !h.executionGroupOpenLocked(run.group) {
		return nil, nil
	}
	step := run.modelStep
	if step == nil || step.id != owner.stepID || step.imagesClosed || step.finished {
		return nil, nil
	}
	tool := step.toolCalls[owner.toolID]
	if tool == nil || !tool.started || tool.resultRecorded || tool.imagesReady {
		return nil, nil
	}
	return run, tool
}

func (h *Host) finishToolImages(run *agentRun, stepID, toolID string, success bool) []imageapi.Metadata {
	h.mu.Lock()
	defer h.mu.Unlock()
	if run.modelStep == nil || run.modelStep.id != stepID {
		return nil
	}
	tool := run.modelStep.toolCalls[toolID]
	if tool == nil {
		return nil
	}
	tool.imagesReady = true
	if !success || run.modelStep.imagesClosed {
		tool.rawImages = nil
		return nil
	}
	return imageMetadata(tool.rawImages)
}
func imageMetadata(images []*imageapi.Data) []imageapi.Metadata {
	if len(images) == 0 {
		return nil
	}
	result := make([]imageapi.Metadata, len(images))
	for i, d := range images {
		result[i] = d.Metadata
	}
	return result
}
func discardStepImages(step *modelStepState) {
	if step == nil {
		return
	}
	step.imagesClosed = true
	for _, call := range step.toolCalls {
		call.rawImages = nil
		call.images = nil
	}
}
func (b *broker) handleDiscardImages(w http.ResponseWriter, r *http.Request, op *Operation) {
	var req struct {
		RunID       string `json:"runId"`
		ModelStepID string `json:"modelStepId"`
	}
	if decodeRequest(r, &req) != nil {
		writeRPCError(w, 400, "request", "invalid image discard")
		return
	}
	h := b.host
	h.mu.Lock()
	run := h.runs[req.RunID]
	if run == nil || run.operation != op || run.modelStep == nil || run.modelStep.id != req.ModelStepID {
		h.mu.Unlock()
		writeRPCError(w, 409, "scope", "image discard scope has ended")
		return
	}
	discardStepImages(run.modelStep)
	h.mu.Unlock()
	writeJSON(w, map[string]bool{"discarded": true})
}

func selectedToolImages(tool *toolCallState, raw, selected []imageapi.Metadata) ([]*imageapi.Data, error) {
	if len(raw) != len(tool.rawImages) {
		return nil, errors.New("raw images differ from the physical tool result")
	}
	for i, d := range tool.rawImages {
		if raw[i] != d.Metadata {
			return nil, errors.New("raw image metadata differs from the physical tool result")
		}
	}
	wanted := make(map[string]bool, len(selected))
	for _, m := range selected {
		if m.ImageID == "" || wanted[m.ImageID] {
			return nil, errors.New("invalid or duplicate image selection")
		}
		wanted[m.ImageID] = true
	}
	var result []*imageapi.Data
	for _, d := range tool.rawImages {
		if wanted[d.ImageID] {
			if selected[len(result)] != d.Metadata {
				return nil, errors.New("image selection must retain original metadata and order")
			}
			result = append(result, d)
			delete(wanted, d.ImageID)
		}
	}
	if len(wanted) > 0 {
		return nil, errors.New("image selection contains an unknown image")
	}
	return result, nil
}

func (run *agentRun) imageStore() map[string]*imageapi.Data {
	if run.instance != nil {
		if run.instance.images == nil {
			run.instance.images = make(map[string]*imageapi.Data)
		}
		return run.instance.images
	}
	if run.images == nil {
		run.images = make(map[string]*imageapi.Data)
	}
	return run.images
}

// Internal history holds references, never data URLs. Expansion returns new
// request buffers, which pin bytes for the provider even during cancellation.
func (h *Host) expandImagesLocked(run *agentRun, messages []json.RawMessage) ([]json.RawMessage, error) {
	result := make([]json.RawMessage, len(messages))
	store := run.imageStore()
	for i, raw := range messages {
		var msg map[string]json.RawMessage
		if json.Unmarshal(raw, &msg) != nil {
			return nil, errors.New("invalid image message")
		}
		var parts []map[string]json.RawMessage
		if json.Unmarshal(msg["content"], &parts) != nil {
			result[i] = raw
			continue
		}
		changed := false
		for j, p := range parts {
			var kind, id string
			json.Unmarshal(p["type"], &kind)
			if kind != "noe_image" {
				continue
			}
			json.Unmarshal(p["imageId"], &id)
			data := store[id]
			if data == nil {
				return nil, errors.New("unknown or uncommitted image reference")
			}
			encoded, _ := json.Marshal(map[string]any{"type": "image_url", "image_url": map[string]string{"url": "data:" + data.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(data.Content), "detail": data.Detail}})
			parts[j] = nil
			json.Unmarshal(encoded, &parts[j])
			changed = true
		}
		if changed {
			msg["content"], _ = json.Marshal(parts)
			result[i], _ = json.Marshal(msg)
		} else {
			result[i] = raw
		}
	}
	return result, nil
}

func imageReferences(messages []json.RawMessage) []string {
	var ids []string
	for _, raw := range messages {
		var msg struct {
			Content []struct {
				Type    string `json:"type"`
				ImageID string `json:"imageId"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &msg) != nil {
			continue
		}
		for _, p := range msg.Content {
			if p.Type == "noe_image" {
				ids = append(ids, p.ImageID)
			}
		}
	}
	return ids
}
func imageAttachmentMessage(raw json.RawMessage) bool {
	var m struct {
		Role    string                       `json:"role"`
		Content []map[string]json.RawMessage `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil || m.Role != "user" || len(m.Content) == 0 {
		return false
	}
	seen := map[string]bool{}
	for _, p := range m.Content {
		var kind, id string
		if len(p) != 2 || json.Unmarshal(p["type"], &kind) != nil || kind != "noe_image" || json.Unmarshal(p["imageId"], &id) != nil || id == "" || seen[id] {
			return false
		}
		seen[id] = true
	}
	return true
}
func commitStepImagesLocked(run *agentRun, entry conversationEntry) error {
	step := run.modelStep
	ids := imageReferences(entry.Messages)
	if step.imagesClosed || step.imagesCommitted {
		if len(ids) > 0 {
			return errors.New("image batch is closed")
		}
		return nil
	}
	var images []*imageapi.Data
	for _, callID := range step.toolCallOrder {
		images = append(images, step.toolCalls[callID].images...)
	}
	if len(ids) != len(images) {
		return errors.New("image checkpoint differs from selected images")
	}
	for i, d := range images {
		if ids[i] != d.ImageID {
			return errors.New("image checkpoint order differs from selected images")
		}
	}
	store := run.imageStore()
	for _, d := range images {
		store[d.ImageID] = d
	}
	step.imagesCommitted = true
	discardStepImages(step)
	return nil
}
func reconcileImagesLocked(run *agentRun, messages []json.RawMessage) {
	keep := map[string]bool{}
	for _, id := range imageReferences(messages) {
		keep[id] = true
	}
	for id := range run.imageStore() {
		if !keep[id] {
			delete(run.imageStore(), id)
		}
	}
}
