// Package modelbinding installs model operations independently of agent control.
package modelbinding

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
import {generate_image} from "noe:image";
export {decide,generate_image};
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
		work := func(ctx context.Context) (any, error) { return b.call(ctx, method, payload) }
		var value any
		var err error
		if b.options.Run != nil {
			value, err = b.options.Run(b.ctx, work)
		} else {
			value, err = work(b.ctx)
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
			if contextErr := b.ctx.Err(); contextErr != nil {
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
			return map[string]any{"imageError": &imageapi.Error{Code: "input", Message: "read_image requires an active run_js model tool call"}}, nil
		}
		var read func(context.Context, string) ([]byte, error)
		if fs := b.view.FileSystem(); fs != nil {
			read = fs.ReadFile
		}
		value, err := b.options.ReadImage(ctx, q.Path, q.Detail, q.Ordinal, read)
		if err != nil {
			return map[string]any{"imageError": imageapi.AsError(err)}, nil
		}
		return value, nil
	}
	return nil, fmt.Errorf("model: unknown operation %q", method)
}

func (b *Binding) generateImage(ctx context.Context, payload string) (imageapi.Response, error) {
	var request imageapi.Request
	if err := json.Unmarshal([]byte(payload), &request); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "input", Message: "invalid image generation request"}
	}
	if err := request.Validate(); err != nil {
		return imageapi.Response{}, err
	}
	fs := b.view.FileSystem()
	if fs == nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: "image generation requires an enabled filesystem"}
	}
	if err := fs.CheckWritable(); err != nil {
		return imageapi.Response{}, &imageapi.Error{Code: "filesystem", Message: err.Error()}
	}
	generated, err := b.service.GenerateImage(ctx, imageapi.GenerationRequest{Provider: request.Provider, Model: request.Model, Prompt: request.Prompt, Parameters: request.Parameters})
	if err != nil {
		return imageapi.Response{}, err
	}
	data, err := imageapi.ReadData(ctx, request.Output, generated.Content, "")
	var metadata imageapi.Metadata
	if err == nil {
		metadata = data.Metadata
		metadata.Detail = ""
		if writeErr := fs.WriteFileAtomic(ctx, request.Output, data.Content); writeErr != nil {
			if ctx.Err() != nil {
				err = ctx.Err()
			} else {
				err = &imageapi.Error{Code: "filesystem", Message: writeErr.Error(), RequestID: generated.RequestID}
			}
		}
	}
	if b.options.AfterSave != nil {
		b.options.AfterSave(request.Output, generated, metadata, err)
	}
	if err != nil {
		details := *imageapi.AsError(err)
		details.Usage = append(json.RawMessage(nil), generated.Usage...)
		if details.RequestID == "" {
			details.RequestID = generated.RequestID
		}
		return imageapi.Response{}, &details
	}
	return imageapi.Response{Metadata: metadata, Provider: request.Provider, Model: request.Model, ResponseModel: generated.ResponseModel, RequestID: generated.RequestID, Usage: generated.Usage}, nil
}
