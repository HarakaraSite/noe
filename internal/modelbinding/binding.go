// Package modelbinding installs model operations independently of agent control.
package modelbinding

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type Service interface {
	ListModels(context.Context, provider.ModelQuery) ([]provider.Model, error)
	Generate(context.Context, provider.Request) (provider.Response, error)
	Decide(context.Context, decision.Request) (decision.Response, error)
	GenerateImage(context.Context, imageapi.GenerationRequest) (imageapi.Generated, error)
}

type ImageEditor interface {
	EditImage(context.Context, imageapi.EditRequest) (imageapi.Generated, error)
}

type Options struct {
	ReadImage func(context.Context, string, string, int, func(context.Context, string) ([]byte, error)) (imageapi.Metadata, error)
	Run       func(context.Context, func(context.Context) (any, error)) (any, error)
	AfterSave func(string, imageapi.Generated, imageapi.Metadata, error)
}

type Binding struct {
	view     *stdlib.Bindings
	service  Service
	options  Options
	ctx      context.Context
	cancel   context.CancelFunc
	stopView func() bool
	mu       sync.Mutex
	closed   bool
	done     chan struct{}
	workers  sync.WaitGroup
}

const moduleSource = `import {request} from "noe:model/internal";
export async function generate(options){return JSON.parse(await request("generate",JSON.stringify(options)))}
export async function list_models(options={}){return JSON.parse(await request("list_models",JSON.stringify(options)))}
import {decide} from "noe:decision";
import {generate_image,edit_image} from "noe:image";
export {decide,generate_image,edit_image};
`

func Bind(ctx context.Context, view *stdlib.Bindings, service Service, options Options) (*Binding, error) {
	if ctx == nil || view == nil || view.Context() == nil || view.Runtime() == nil || view.Loop() == nil || service == nil {
		return nil, errors.New("model: live bindings, context and service are required")
	}
	if view.Worker() {
		return nil, errors.New("model: Worker bindings are unsupported")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := view.Context().Err(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &Binding{view: view, service: service, options: options, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	b.stopView = context.AfterFunc(view.Context(), cancel)
	sources := map[string]string{"noe:model": moduleSource, "noe:decision": decision.ModuleSource, "noe:image": imageapi.ModuleSource}
	for id, source := range sources {
		if err := view.DefineModule(id, source); err != nil {
			b.Close()
			return nil, err
		}
	}
	for _, id := range []string{"noe:model/internal", "noe:decision/internal", "noe:image/internal"} {
		if err := view.Runtime().SetModule(id, map[string]any{"request": b.request}); err != nil {
			b.Close()
			return nil, err
		}
	}
	view.Runtime().OnClose(b.Close)
	return b, nil
}

func (b *Binding) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		<-b.done
		return
	}
	b.closed = true
	b.cancel()
	b.mu.Unlock()
	b.stopView()
	b.workers.Wait()
	close(b.done)
}

func (b *Binding) request(method, payload string) *quickjs.Promise {
	promise := b.view.Runtime().NewPromise()
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil {
		b.mu.Unlock()
		promise.RejectError(context.Canceled)
		return promise
	}
	b.workers.Add(1)
	b.view.Loop().Begin()
	b.mu.Unlock()
	go func() {
		defer b.workers.Done()
		defer b.view.Loop().Done()
		var imageValue any
		work := func(ctx context.Context) (any, error) {
			value, callErr := b.call(ctx, method, payload)
			if isImageMethod(method) && callErr == nil {
				imageValue = value
			}
			return value, callErr
		}
		var value any
		var err error
		if b.options.Run != nil {
			value, err = b.options.Run(b.ctx, work)
		} else {
			value, err = work(b.ctx)
		}
		if isImageMethod(method) {
			if hasStructuredImageError(imageValue) {
				// Execution wrappers may replace a structured image failure with
				// their raw cancellation error after the RPC has completed.
				value = imageValue
				if contextErr := imageCancellationError(b.ctx, err); contextErr != nil {
					value = cancelledImageValue(imageValue, method, contextErr)
				}
				err = nil
			} else if contextErr := imageCancellationError(b.ctx, err); contextErr != nil {
				if _, ok := imageValue.(imageapi.Response); ok {
					value = cancelledImageValue(imageValue, method, contextErr)
					err = nil
				}
			}
		}
		data, encodeErr := json.Marshal(value)
		if err == nil {
			err = encodeErr
		}
		b.view.Loop().Post(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.closed || b.view.Context().Err() != nil {
				return
			}
			if contextErr := b.ctx.Err(); contextErr != nil && isImageMethod(method) {
				if hasStructuredImageError(imageValue) {
					value = cancelledImageValue(imageValue, method, contextErr)
					err = nil
					data, err = json.Marshal(value)
				} else if _, ok := imageValue.(imageapi.Response); ok {
					value = cancelledImageValue(imageValue, method, contextErr)
					err = nil
					data, err = json.Marshal(value)
				} else {
					err = contextErr
				}
			} else if contextErr := b.ctx.Err(); contextErr != nil {
				err = contextErr
			}
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(string(data))
			}
		})
	}()
	return promise
}

func (b *Binding) call(ctx context.Context, method, payload string) (any, error) {
	if err := ctx.Err(); err != nil {
		if isImageMethod(method) {
			operation := imageOperationName(method)
			return imageErrorValue(imageapi.DescribeError(err, operation, "input", imageapi.Generated{})), nil
		}
		return nil, err
	}
	switch method {
	case "list_models":
		var q provider.ModelQuery
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			return nil, err
		}
		models, err := b.service.ListModels(ctx, q)
		if models == nil {
			models = []provider.Model{}
		}
		return models, err
	case "generate":
		var q provider.Request
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			return nil, err
		}
		if strings.TrimSpace(q.Provider) == "" || strings.TrimSpace(q.Model) == "" || len(q.Messages) == 0 || q.MaxTokens < 0 {
			return nil, errors.New("model: provider, model and messages are required")
		}
		return b.service.Generate(ctx, q)
	case "decide":
		q, err := decision.ParseRequest([]byte(payload))
		if err != nil {
			return nil, err
		}
		response, err := b.service.Decide(ctx, q)
		if err != nil {
			return nil, err
		}
		if err := decision.ValidateResponse(response.JSON); err != nil {
			return nil, err
		}
		return response.JSON, nil
	case "generate_image":
		value, err := b.generateImage(ctx, payload)
		if err != nil {
			return map[string]any{"imageError": imageapi.AsError(err)}, nil
		}
		return value, nil
	case "edit_image":
		value, err := b.editImage(ctx, payload)
		if err != nil {
			return map[string]any{"imageError": imageapi.AsError(err)}, nil
		}
		return value, nil
	case "read_image":
		var q struct {
			Path    string `json:"path"`
			Detail  string `json:"detail"`
			Ordinal int    `json:"ordinal"`
		}
		if err := json.Unmarshal([]byte(payload), &q); err != nil {
			return map[string]any{"imageError": &imageapi.Error{Code: "input", Message: "invalid image request"}}, nil
		}
		if b.options.ReadImage == nil {
			return imageErrorValue(&imageapi.Error{Code: "input", Message: "read_image requires an active run_js model tool call", Operation: "read", Stage: "input"}), nil
		}
		var read func(context.Context, string) ([]byte, error)
		if fs := b.view.FileSystem(); fs != nil {
			read = func(ctx context.Context, path string) ([]byte, error) {
				info, err := fs.Stat(ctx, path)
				if err != nil {
					return nil, filesystemImageError(err, "read")
				}
				if !info.Mode().IsRegular() {
					return nil, &imageapi.Error{Code: "input", Message: "image input must be a regular file", Operation: "read", Stage: "input"}
				}
				content, err := fs.ReadFile(ctx, path)
				if err != nil {
					return nil, filesystemImageError(err, "read")
				}
				return content, nil
			}
		}
		value, err := b.options.ReadImage(ctx, q.Path, q.Detail, q.Ordinal, read)
		if err != nil {
			return imageErrorValue(imageapi.DescribeError(err, "read", "input", imageapi.Generated{})), nil
		}
		return value, nil
	}
	return nil, fmt.Errorf("model: unknown operation %q", method)
}

func (b *Binding) generateImage(ctx context.Context, payload string) (imageapi.Response, error) {
	var request imageapi.Request
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "invalid image generation request", Operation: "generate", Stage: "input"}
	}
	if err := request.Validate(); err != nil {
		return imageapi.Response{}, imageapi.DescribeError(err, "generate", "input", imageapi.Generated{})
	}
	fs := b.view.FileSystem()
	if fs == nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: "image generation requires an enabled filesystem", Operation: "generate", Stage: "save"}
	}
	if err := fs.CheckWritable(); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: err.Error(), Operation: "generate", Stage: "save"}
	}
	generated, err := b.service.GenerateImage(ctx, imageapi.GenerationRequest{Provider: request.Provider, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters})
	if err != nil {
		return imageapi.Response{}, imageRequestFailure(ctx, err, "generate", generated)
	}
	generated.Operation = "generate"
	return b.saveImage(ctx, fs, request.Output, request.Provider, request.Model, generated)
}

type editFileRequest struct {
	Provider   string                       `json:"provider"`
	Model      string                       `json:"model"`
	Prompt     string                       `json:"prompt"`
	Output     string                       `json:"output"`
	Images     []string                     `json:"images"`
	Parameters map[string]jsonv1.RawMessage `json:"parameters,omitempty"`
}

func (b *Binding) editImage(ctx context.Context, payload string) (imageapi.Response, error) {
	var request editFileRequest
	decoder := jsontext.NewDecoder(strings.NewReader(payload))
	if err := json.UnmarshalDecode(decoder, &request, json.RejectUnknownMembers(true)); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "invalid image edit request", Operation: "edit", Stage: "input"}
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "invalid image edit request", Operation: "edit", Stage: "input"}
	}
	fileRequest := imageapi.Request{
		Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
		Output: request.Output, Parameters: request.Parameters,
	}
	if err := fileRequest.Validate(); err != nil {
		return imageapi.Response{}, imageapi.DescribeError(err, "edit", "input", imageapi.Generated{})
	}
	if len(request.Images) == 0 {
		return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "images must contain at least one image path", Operation: "edit", Stage: "input"}
	}
	for _, path := range request.Images {
		if strings.TrimSpace(path) == "" {
			return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "image paths must be nonempty strings", Operation: "edit", Stage: "input"}
		}
	}
	fs := b.view.FileSystem()
	if fs == nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: "image editing requires an enabled filesystem", Operation: "edit", Stage: "save"}
	}
	if err := fs.CheckWritable(); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: err.Error(), Operation: "edit", Stage: "save"}
	}
	if ctx.Err() != nil {
		return imageapi.Response{}, imageapi.DescribeError(ctx.Err(), "edit", "input", imageapi.Generated{})
	}
	inputs := make([]imageapi.Input, len(request.Images))
	for i, path := range request.Images {
		if err := ctx.Err(); err != nil {
			return imageapi.Response{}, imageapi.DescribeError(err, "edit", "input", imageapi.Generated{})
		}
		info, err := fs.Stat(ctx, path)
		if err != nil {
			return imageapi.Response{}, filesystemImageError(err, "edit")
		}
		if !info.Mode().IsRegular() {
			return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "image input must be a regular file", Operation: "edit", Stage: "input"}
		}
		content, err := fs.ReadFile(ctx, path)
		if err != nil {
			return imageapi.Response{}, filesystemImageError(err, "edit")
		}
		inputs[i] = imageapi.Input{Content: content}
	}
	prepared, err := imageapi.PrepareEdit(ctx, imageapi.EditRequest{
		Provider: request.Provider, Model: request.Model, Prompt: request.Prompt,
		Images: inputs, Parameters: request.Parameters,
	})
	if err != nil {
		return imageapi.Response{}, err
	}
	editor, ok := b.service.(ImageEditor)
	if !ok {
		return imageapi.Response{}, &imageapi.Error{Code: "unsupported", Message: "image editing is unsupported", Operation: "edit", Stage: "input"}
	}
	generated, err := editor.EditImage(ctx, prepared)
	if err != nil {
		return imageapi.Response{}, imageRequestFailure(ctx, err, "edit", generated)
	}
	generated.Operation = "edit"
	return b.saveImage(ctx, fs, request.Output, request.Provider, request.Model, generated)
}

func (b *Binding) saveImage(ctx context.Context, fs stdlib.FileSystem, output, provider, model string, generated imageapi.Generated) (imageapi.Response, error) {
	operation := generated.Operation
	if operation == "" {
		operation = "generate"
		generated.Operation = operation
	}
	if err := ctx.Err(); err != nil {
		return imageapi.Response{}, imageapi.DescribeError(err, operation, "response", generated)
	}
	data, err := imageapi.ReadData(ctx, output, generated.Content, "")
	var metadata imageapi.Metadata
	if err == nil {
		metadata = data.Metadata
		metadata.Detail = ""
		if writeErr := ctx.Err(); writeErr != nil {
			err = writeErr
		} else if writeErr := fs.WriteFileAtomic(ctx, output, data.Content); writeErr != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = &imageapi.Error{Code: "filesystem", Message: writeErr.Error(), RequestID: generated.RequestID, Operation: operation, Stage: "save"}
			}
		}
	}
	if b.options.AfterSave != nil {
		b.options.AfterSave(output, generated, metadata, err)
	}
	if err != nil {
		stage := "response"
		if imageapi.AsError(err).Code == "filesystem" {
			stage = "save"
		}
		return imageapi.Response{}, imageapi.DescribeError(err, operation, stage, generated)
	}
	return imageapi.Response{Metadata: metadata, Provider: provider, Model: model, ResponseModel: generated.ResponseModel, RequestID: generated.RequestID, Usage: generated.Usage}, nil
}

func ensureJSONEOF(decoder *jsontext.Decoder) error {
	var extra any
	if err := json.UnmarshalDecode(decoder, &extra); err == nil {
		return errors.New("unexpected trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func filesystemImageError(err error, operation string) *imageapi.Error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return imageapi.DescribeError(err, operation, "input", imageapi.Generated{})
	}
	return &imageapi.Error{Code: "filesystem", Message: err.Error(), Operation: operation, Stage: "input"}
}

func imageRequestFailure(ctx context.Context, err error, operation string, generated imageapi.Generated) *imageapi.Error {
	details := imageapi.DescribeError(err, operation, "request", generated)
	if ctxErr := ctx.Err(); ctxErr != nil {
		details.Code = "cancelled"
		details.Message = ctxErr.Error()
	}
	return details
}

func imageOperationName(method string) string {
	if method == "edit_image" {
		return "edit"
	}
	return "generate"
}

func isImageMethod(method string) bool {
	return method == "generate_image" || method == "edit_image"
}

func imageErrorValue(err *imageapi.Error) map[string]any {
	return map[string]any{"imageError": err}
}

func hasStructuredImageError(value any) bool {
	result, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, ok = result["imageError"].(*imageapi.Error)
	return ok
}

func imageCancellationError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func cancelledImageValue(value any, method string, cause error) map[string]any {
	operation := imageOperationName(method)
	if result, ok := value.(map[string]any); ok {
		if imageErr, ok := result["imageError"].(*imageapi.Error); ok {
			cancelled := *imageErr
			cancelled.Code = "cancelled"
			cancelled.Message = cause.Error()
			if cancelled.Operation == "" {
				cancelled.Operation = operation
			}
			if cancelled.Stage == "" {
				cancelled.Stage = "request"
			}
			return map[string]any{"imageError": &cancelled}
		}
	}
	if response, ok := value.(imageapi.Response); ok {
		return map[string]any{"imageError": &imageapi.Error{
			Code: "cancelled", Message: cause.Error(), Operation: operation,
			Stage: "save", RequestID: response.RequestID,
			Usage: append(jsonv1.RawMessage(nil), response.Usage...),
		}}
	}
	return map[string]any{"imageError": &imageapi.Error{
		Code: "cancelled", Message: cause.Error(), Operation: operation, Stage: "request",
	}}
}
