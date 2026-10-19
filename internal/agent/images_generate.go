package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

func (b *broker) handleGenerateImage(w http.ResponseWriter, r *http.Request, op *Operation) {
	var request imageapi.Request
	if err := decodeRequest(r, &request); err != nil {
		writeImageError(w, imageInput("invalid image generation request"))
		return
	}
	if err := request.Validate(); err != nil {
		writeImageError(w, err)
		return
	}
	if b.host.imager == nil {
		writeImageError(w, &imageapi.Error{Code: "unsupported", Message: "image generation provider is unavailable"})
		return
	}
	if !filepath.IsAbs(request.Output) {
		request.Output = filepath.Join(b.host.opts.Cwd, request.Output)
	}
	absoluteOutput, err := filepath.Abs(request.Output)
	if err != nil {
		writeImageError(w, &imageapi.Error{Code: "filesystem", Message: "cannot resolve image output path"})
		return
	}
	request.Output = filepath.Clean(absoluteOutput)
	b.host.mu.Lock()
	if run := b.host.currentOwnerRunLocked(op); run != nil {
		request.RunID = run.id
	}
	b.host.mu.Unlock()

	ctx, cancel := context.WithCancel(op.ctx)
	stopRequest := context.AfterFunc(r.Context(), cancel)
	defer func() { stopRequest(); cancel() }()
	if ctx.Err() != nil {
		writeImageError(w, &imageapi.Error{Code: "cancelled", Message: "image generation request cancelled"})
		return
	}
	requestID, err := randomID()
	if err != nil {
		writeImageError(w, &imageapi.Error{Code: "response", Message: "cannot identify image generation request"})
		return
	}
	activityID, err := randomID()
	if err != nil {
		writeImageError(w, &imageapi.Error{Code: "response", Message: "cannot identify image generation activity"})
		return
	}
	fields, group, _, admitted := b.host.prepareDecisionFields(op, requestID, activityID, decision.Request{Provider: request.Provider, Model: request.Model}, false)
	delete(fields, "decisionModel")
	fields["imageModel"] = request.Model
	origin := activityOrigin{Kind: "script"}
	origin.RunID, _ = fields["runId"].(string)
	origin.AgentID, _ = fields["agentId"].(string)
	origin.ToolCallID, _ = fields["toolCallId"].(string)
	limitAction := (*executionLimitAction)(nil)
	if admitted {
		b.host.mu.Lock()
		limitAction, admitted = b.host.reserveExecutionLocked(group, "providerCalls", "image", activityID, origin)
		limitCode, limitMessage := executionLimitError(group)
		b.host.mu.Unlock()
		if !admitted {
			b.host.finishExecutionLimit(limitAction)
			writeImageError(w, &imageapi.Error{Code: "cancelled", Message: limitCode + ": " + limitMessage})
			return
		}
	}
	if !admitted {
		b.host.finishExecutionLimit(limitAction)
		writeImageError(w, &imageapi.Error{Code: "cancelled", Message: "image generation execution scope has ended"})
		return
	}
	b.host.decorateExecution(fields, group)
	started := time.Now()
	base := events.Event{"path": request.Output}
	b.host.decisionEvent("image.started", fields, base)

	generated, err := b.host.imager.GenerateImage(ctx, request)
	if ctx.Err() != nil {
		err = &imageapi.Error{Code: "cancelled", Message: "image generation request cancelled"}
	}
	if err != nil {
		b.finishImageActivity(w, fields, group, started, request.Output, err, nil, "")
		return
	}
	// A successful remote generation is billable even if the local save fails.
	b.host.mu.Lock()
	if group != nil {
		group.usage.add(summarizeUsage(generated.Usage))
	}
	b.host.mu.Unlock()
	metadata, err := imageapi.SaveFile(ctx, request.Output, generated.Content)
	if err != nil {
		if ctx.Err() != nil {
			err = &imageapi.Error{Code: "cancelled", Message: "image generation request cancelled"}
		}
		b.finishImageActivity(w, fields, group, started, request.Output, err, generated.Usage, generated.RequestID)
		return
	}
	metadata.Path = request.Output
	result := imageapi.Response{
		Metadata: metadata, Provider: request.Provider, Model: request.Model,
		ResponseModel: generated.ResponseModel, RequestID: generated.RequestID, Usage: generated.Usage,
	}
	extra := events.Event{
		"duration": time.Since(started), "path": request.Output,
		"image": metadata, "responseModel": generated.ResponseModel,
		"providerRequestId": generated.RequestID, "usage": generated.Usage,
	}
	b.host.decorateExecution(extra, group)
	b.host.decisionEvent("image.completed", fields, extra)
	writeJSON(w, result)
}

func (b *broker) finishImageActivity(w http.ResponseWriter, fields events.Event, group *executionGroup, started time.Time, output string, err error, usage json.RawMessage, requestID string) {
	imageErr := imageapi.AsError(err)
	kind := "image.failed"
	if imageErr.Code == "cancelled" {
		kind = "image.cancelled"
	}
	extra := events.Event{"duration": time.Since(started), "path": output, "reason": imageErr.Code, "error": imageErr, "providerRequestId": requestID}
	if len(usage) > 0 {
		extra["usage"] = usage
	}
	b.host.decorateExecution(extra, group)
	b.host.decisionEvent(kind, fields, extra)
	writeImageError(w, imageErr)
}
