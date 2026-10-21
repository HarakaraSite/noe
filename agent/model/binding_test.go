package model_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type bindingService struct {
	mu       sync.Mutex
	images   int
	content  []byte
	generate func(context.Context, model.Request) (model.Response, error)
}

func (s *bindingService) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return nil, nil
}
func (s *bindingService) Generate(ctx context.Context, q model.Request) (model.Response, error) {
	if s.generate != nil {
		return s.generate(ctx, q)
	}
	return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"standalone"}`), FinishReason: "stop"}, nil
}
func (s *bindingService) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{JSON: json.RawMessage(`{"answers":{"q":{"choice":"yes"}}}`), StatusCode: 200}, nil
}
func (s *bindingService) GenerateImage(ctx context.Context, q model.ImageRequest) (model.GeneratedImage, error) {
	s.mu.Lock()
	s.images++
	s.mu.Unlock()
	return model.GeneratedImage{Content: append([]byte(nil), s.content...), RequestID: "image-request", Usage: json.RawMessage(`{"total_tokens":5}`)}, nil
}
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var out bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 2, 3))
	m.Set(0, 0, color.RGBA{R: 255, A: 255})
	if err := png.Encode(&out, m); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
func executeBinding(t *testing.T, service model.Service, modules stdlib.Modules, code string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[model consumer]", Virtual: true, Module: true, Code: code}, stdlib.Profile{Modules: modules, Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := model.Bind(ctx, b, service)
		if err != nil {
			return nil, err
		}
		return binding.Close, nil
	}})
	return err
}
func TestStandaloneModelBindingUsesActualRootFilesystem(t *testing.T) {
	root := t.TempDir()
	service := &bindingService{content: tinyPNG(t)}
	err := executeBinding(t, service, stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}, `
  import {generate,list_models,decide,generate_image} from "noe:model";
  if((await generate({provider:"fixture",model:"fixture",messages:[{role:"user",content:"hello"}]})).message.content!=="standalone")throw Error("generate");
  if(!Array.isArray(await list_models()))throw Error("catalog");
  if((await decide({provider:"openrouter",model:"fixture",state:"state",questions:{q:{type:"choice"}}})).answers.q.choice!=="yes")throw Error("decision");
  const result=await generate_image({provider:"fixture",model:"image",prompt:"draw",output:"/out.png"});
  if(result.path!=="/out.png"||result.width!==2||result.height!==3||result.requestId!=="image-request"||"content" in result)throw Error("image metadata");
 `)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	data, err := os.ReadFile(filepath.Join(root, "out.png"))
	if err != nil || !bytes.Equal(data, service.content) {
		t.Fatalf("saved data err=%v", err)
	}
	if service.images != 1 {
		t.Fatalf("image calls=%d", service.images)
	}
}
func TestStandaloneImageRejectsMissingOrReadOnlyFSBeforeProvider(t *testing.T) {
	for _, tc := range []struct {
		name    string
		modules stdlib.Modules
	}{{"missing", stdlib.Modules{}}, {"readonly", stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: t.TempDir(), ReadOnly: true}}}} {
		t.Run(tc.name, func(t *testing.T) {
			service := &bindingService{content: tinyPNG(t)}
			err := executeBinding(t, service, tc.modules, `import {generate_image,read_image} from "noe:image";
    try{await generate_image({provider:"fixture",model:"image",prompt:"draw",output:"/out.png"});throw Error("accepted")}catch(e){if(e.code!=="filesystem")throw e}
    try{await read_image("/any.png");throw Error("accepted")}catch(e){if(e.code!=="input"||!e.message.includes("active run_js"))throw e}
   `)
			if err != nil {
				t.Fatal(stdlib.Diagnostic(err))
			}
			if service.images != 0 {
				t.Fatal("filesystem rejection called provider")
			}
		})
	}
}
func TestStandaloneImageSaveFailurePreservesBillableUsageAndDestination(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "existing-directory"), 0700); err != nil {
		t.Fatal(err)
	}
	service := &bindingService{content: tinyPNG(t)}
	err := executeBinding(t, service, stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root}}, `
  import {generate_image} from "noe:image";
  try{await generate_image({provider:"fixture",model:"image",prompt:"draw",output:"/existing-directory"});throw Error("save succeeded")}
  catch(e){if(e.code!=="filesystem"||e.requestId!=="image-request"||e.usage?.total_tokens!==5)throw e}
 `)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	if service.images != 1 {
		t.Fatalf("image calls=%d", service.images)
	}
	info, err := os.Stat(filepath.Join(root, "existing-directory"))
	if err != nil || !info.IsDir() {
		t.Fatalf("destination changed: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Fatalf("temporary file leaked: %v %v", entries, err)
	}
}
func TestModelBindRejectsZeroBindings(t *testing.T) {
	if binding, err := model.Bind(context.Background(), new(stdlib.Bindings), new(bindingService)); err == nil {
		binding.Close()
		t.Fatal("zero bindings accepted")
	}
}

func TestModelBindingRequestCancellationRejectsWhileViewRemainsLive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	requestCtx, cancelRequest := context.WithCancel(ctx)
	defer cancelRequest()
	entered := make(chan struct{})
	service := &bindingService{generate: func(ctx context.Context, q model.Request) (model.Response, error) {
		close(entered)
		<-ctx.Done()
		// A cooperative service can finish cleanup with a valid result even
		// after cancellation; the binding still owns cancellation semantics.
		return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"late success"}`), FinishReason: "stop"}, nil
	}}
	go func() {
		select {
		case <-entered:
			cancelRequest()
		case <-ctx.Done():
			cancelRequest()
		}
	}()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[model cancellation]", Virtual: true, Module: true, Code: `
        import {generate} from "noe:model";
        let rejected=false;
        try{await generate({provider:"fixture",model:"fixture",messages:[{role:"user",content:"wait"}]})}
        catch(error){rejected=error.message.includes("canceled")}
        if(!rejected)throw Error("request cancellation did not reject");
    `}, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := model.Bind(requestCtx, b, service)
		if err != nil {
			return nil, err
		}
		return binding.Close, nil
	}})
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}
