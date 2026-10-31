package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

// ReadImage reads bytes through the execution's selected filesystem and
// attaches their validated snapshot to the active run_js tool call.
func (op *Operation) ReadImage(ctx context.Context, path, detail string, ordinal int, read func(context.Context, string) ([]byte, error)) (imageapi.Metadata, error) {
	if path == "" || ordinal <= 0 {
		return imageapi.Metadata{}, &imageapi.Error{Code: "input", Message: "path and a positive ordinal are required", Operation: "read", Stage: "input"}
	}
	merged, release, _, err := op.beginRequestContext(ctx)
	if err != nil {
		return imageapi.Metadata{}, imageapi.DescribeError(readImageRequestError(err), "read", "input", imageapi.Generated{})
	}
	defer release()

	h := op.host
	h.mu.Lock()
	owner := op.imageOwner
	run, tool := h.admitImageLocked(op)
	if tool != nil {
		if tool.imageOrders == nil {
			tool.imageOrders = map[int]bool{}
		}
		if tool.imageOrders[ordinal] {
			tool = nil
		} else {
			tool.imageOrders[ordinal] = true
		}
	}
	h.mu.Unlock()
	if run == nil || tool == nil {
		return imageapi.Metadata{}, &imageapi.Error{Code: "input", Message: "read_image requires an active run_js model tool call", Operation: "read", Stage: "input"}
	}
	if read == nil {
		return imageapi.Metadata{}, &imageapi.Error{Code: "filesystem", Message: "image input filesystem is unavailable", Operation: "read", Stage: "input"}
	}
	if err := merged.Err(); err != nil {
		return imageapi.Metadata{}, imageapi.DescribeError(err, "read", "input", imageapi.Generated{})
	}
	content, err := read(merged, path)
	if err != nil {
		if contextErr := merged.Err(); contextErr != nil {
			err = contextErr
		}
		return imageapi.Metadata{}, readImageFailure(err)
	}
	data, err := imageapi.ReadInputData(merged, path, content, detail)
	if err != nil {
		return imageapi.Metadata{}, imageapi.DescribeError(err, "read", "input", imageapi.Generated{})
	}
	imageID, err := randomID()
	if err != nil {
		return imageapi.Metadata{}, &imageapi.Error{Code: "response", Message: "cannot identify image attachment", Operation: "read", Stage: "input"}
	}
	data.ImageID = imageID
	data.Ordinal = ordinal

	h.mu.Lock()
	current, currentTool := h.admitImageLocked(op)
	if current != run || currentTool != tool || merged.Err() != nil {
		h.mu.Unlock()
		return imageapi.Metadata{}, &imageapi.Error{Code: "cancelled", Message: "image attachment scope has ended", Operation: "read", Stage: "input"}
	}
	tool.rawImages = append(tool.rawImages, data)
	sort.Slice(tool.rawImages, func(i, j int) bool { return tool.rawImages[i].Ordinal < tool.rawImages[j].Ordinal })
	h.mu.Unlock()
	h.observeRun("image.read", run.id, run, events.Event{
		"path": path, "modelStepId": owner.stepID, "toolCallId": owner.toolID, "image": data.Metadata,
	})
	return data.Metadata, nil
}

func readImageRequestError(err error) error {
	if errors.Is(err, ErrOperationClosed) {
		return &imageapi.Error{Code: "cancelled", Message: "image attachment scope has ended", Operation: "read", Stage: "input"}
	}
	var operationErr *operationError
	if errors.As(err, &operationErr) {
		return imageInput("read_image requires an active run_js model tool call")
	}
	return imageapi.AsError(err)
}

// ObserveImageSave records the local result of saving generated image bytes.
// Generation usage and provider-call counters were already accounted by the
// scoped model service, so this observer never updates them again.
func (op *Operation) ObserveImageSave(output string, generated model.GeneratedImage, metadata imageapi.Metadata, saveErr error) {
	if op == nil || op.host == nil {
		return
	}
	h := op.host
	operation := generated.Operation
	if operation == "" {
		operation = "generate"
	}
	requestID := generated.RequestID
	usage := append(json.RawMessage(nil), generated.Usage...)
	fields := events.Event{
		"operation": operation,
		"path":      output, "providerRequestId": generated.RequestID,
		"responseModel": generated.ResponseModel,
	}
	if saveErr == nil {
		if metadata.Path == "" {
			metadata.Path = output
		}
		fields["image"] = metadata
	} else {
		imageErr := imageapi.DescribeError(saveErr, operation, "save", generated)
		if requestID == "" {
			requestID = imageErr.RequestID
		}
		if len(usage) == 0 {
			usage = append(json.RawMessage(nil), imageErr.Usage...)
		}
		fields["reason"], fields["error"] = imageErr.Code, imageErr
	}
	fields["providerRequestId"] = requestID
	if len(usage) != 0 {
		fields["usage"] = usage
	}

	h.mu.Lock()
	run := h.currentOwnerRunLocked(op)
	group := h.executionForOperationLocked(op)
	causes := append([]string(nil), op.causeActivityIDs...)
	origin := op.activityOrigin
	if run != nil {
		for key, value := range decisionOwnerFields(run.id, run, "") {
			fields[key] = value
		}
		if len(causes) == 0 && run.activityID != "" {
			causes = []string{run.activityID}
		}
	}
	for key, value := range op.decisionOwner {
		fields[key] = value
	}
	h.mu.Unlock()
	if origin.Kind == "" {
		origin = activityOrigin{Kind: "image"}
		origin.RunID, _ = fields["runId"].(string)
		origin.AgentID, _ = fields["agentId"].(string)
		origin.ToolCallID, _ = fields["toolCallId"].(string)
	}
	addActivityFields(fields, "", causes, origin)
	h.decorateExecution(fields, group)
	if saveErr == nil {
		fields["type"] = "image.saved"
	} else {
		fields["type"] = "image.save_failed"
	}
	h.observe(fields)
}

func readImageFailure(err error) *imageapi.Error {
	var imageErr *imageapi.Error
	if errors.As(err, &imageErr) {
		return imageapi.DescribeError(err, "read", "input", imageapi.Generated{})
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return imageapi.DescribeError(err, "read", "input", imageapi.Generated{})
	}
	return &imageapi.Error{Code: "filesystem", Message: err.Error(), Operation: "read", Stage: "input"}
}
