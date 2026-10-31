package agent_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func fixturePNG(t *testing.T, c color.RGBA) []byte {
	t.Helper()
	var out bytes.Buffer
	m := image.NewRGBA(image.Rect(0, 0, 2, 3))
	m.Set(0, 0, c)
	if err := png.Encode(&out, m); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

type actualFSModels struct {
	fixtureModels
	mu                   sync.Mutex
	calls, images, edits int
	generated, edited    []byte
}

func (s *actualFSModels) Generate(ctx context.Context, q model.Request) (model.Response, error) {
	s.mu.Lock()
	s.calls++
	index := s.calls
	s.mu.Unlock()
	if index == 1 {
		return runtimeToolResponse("image-generate-tool", `import {read_image,generate_image} from "noe:image";
 const saved=await generate_image({provider:"fixture",model:"image",prompt:"tool image",output:"/tool.png"});
 if(saved.path!=="/tool.png"||"content" in saved)throw Error("save metadata");
 const read=await read_image(saved.path);
 if(!read.imageId||read.width!==2||"content" in read)throw Error("generated image read metadata");`), nil
	}
	if index == 2 {
		wire, _ := json.Marshal(q.Messages)
		if !strings.Contains(string(wire), base64.StdEncoding.EncodeToString(s.generated)) {
			return model.Response{}, fmt.Errorf("model did not receive generated image bytes")
		}
		return runtimeToolResponse("image-edit-tool", `import {read_image,edit_image} from "noe:image";
 const revised=await edit_image({provider:"fixture",model:"image",prompt:"revise generated image",images:["/tool.png"],output:"/edited.png"});
 if(revised.path!=="/edited.png"||"content" in revised)throw Error("edit metadata");
 const read=await read_image(revised.path);
 if(!read.imageId||read.width!==2||"content" in read)throw Error("edited image read metadata");`), nil
	}
	wire, _ := json.Marshal(q.Messages)
	if !strings.Contains(string(wire), base64.StdEncoding.EncodeToString(s.edited)) {
		return model.Response{}, fmt.Errorf("model did not receive edited image bytes")
	}
	return runtimeFinalResponse("image flow complete"), nil
}

func (s *actualFSModels) EditImage(ctx context.Context, q model.ImageEditRequest) (model.GeneratedImage, error) {
	if err := ctx.Err(); err != nil {
		return model.GeneratedImage{}, err
	}
	if len(q.Images) != 1 || q.Images[0].MIMEType != "image/png" || !bytes.Equal(q.Images[0].Content, s.generated) {
		return model.GeneratedImage{}, fmt.Errorf("edit did not receive the generated filesystem image")
	}
	s.mu.Lock()
	s.edits++
	s.mu.Unlock()
	return model.GeneratedImage{
		Content: append([]byte(nil), s.edited...), Usage: json.RawMessage(`{"total_tokens":7}`), RequestID: "edited",
	}, nil
}
func (s *actualFSModels) GenerateImage(ctx context.Context, q model.ImageRequest) (model.GeneratedImage, error) {
	s.mu.Lock()
	s.images++
	s.mu.Unlock()
	return model.GeneratedImage{Content: append([]byte(nil), s.generated...), Usage: json.RawMessage(`{"total_tokens":5}`), RequestID: "generated"}, nil
}

func TestAgentImageHelpersUseAttachedViewAndTaskTemplateSeparately(t *testing.T) {
	ctx := testContext(t)
	taskRoot, attachedRoot := t.TempDir(), t.TempDir()
	input := fixturePNG(t, color.RGBA{R: 255, A: 255})
	other := fixturePNG(t, color.RGBA{B: 255, A: 255})
	generated := fixturePNG(t, color.RGBA{G: 255, A: 255})
	if err := os.WriteFile(filepath.Join(taskRoot, "input.png"), input, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(taskRoot, "invalid.png"), []byte("not an image"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attachedRoot, "input.png"), other, 0600); err != nil {
		t.Fatal(err)
	}
	edited := fixturePNG(t, color.RGBA{R: 255, G: 128, A: 255})
	models := &actualFSModels{generated: generated, edited: edited}
	template := stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: taskRoot}}}
	var mu sync.Mutex
	var final agent.InstanceSnapshot
	var savedEvents int
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &template, Observe: func(event agent.Event) {
		if event["type"] == "image.saved" {
			mu.Lock()
			savedEvents++
			mu.Unlock()
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	profile := stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: attachedRoot}}, Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := agent.Attach(ctx, b, host)
		if err != nil {
			return nil, err
		}
		if err := b.Runtime().Set("recordFinal", func(text string) error { return json.Unmarshal([]byte(text), &final) }); err != nil {
			binding.Close()
			return nil, err
		}
		return binding.Close, nil
	}}
	_, err = stdlib.Execute(ctx, stdlib.Source{Name: "[attached image client]", Virtual: true, Module: true, Code: `
  import {generate_image} from "noe:image";
  import {run_agent} from "noe:agent";
  const image=await generate_image({provider:"fixture",model:"image",prompt:"root image",output:"/attached.png"});
  if(image.path!=="/attached.png")throw Error("attached path");
  recordFinal(JSON.stringify(await run_agent({provider:"fixture",model:"fixture-model",name:"image",effort:"none",maxSteps:10},"image flow")));
 `}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	if final.LastResult == nil || final.LastResult.State != "completed" || final.LastResult.Answer != "image flow complete" {
		t.Fatalf("final=%+v", final.LastResult)
	}
	for file, expected := range map[string][]byte{
		filepath.Join(attachedRoot, "attached.png"): generated,
		filepath.Join(taskRoot, "tool.png"):         generated,
		filepath.Join(taskRoot, "edited.png"):       edited,
	} {
		data, err := os.ReadFile(file)
		if err != nil || !bytes.Equal(data, expected) {
			t.Fatalf("saved %s matches=%v err=%v", file, bytes.Equal(data, expected), err)
		}
	}
	if _, err := os.Stat(filepath.Join(taskRoot, "attached.png")); !os.IsNotExist(err) {
		t.Fatalf("attached save used Host template: %v", err)
	}
	if _, err := os.Stat(filepath.Join(attachedRoot, "tool.png")); !os.IsNotExist(err) {
		t.Fatalf("tool save used attached root: %v", err)
	}
	if final.Execution == nil || final.Execution.Counters.ProviderCalls != 5 || final.Execution.Counters.ImageProviderCalls != 2 || final.Execution.Usage == nil || final.Execution.Usage.TotalTokens == nil || *final.Execution.Usage.TotalTokens != 17 {
		t.Fatalf("execution usage=%+v", final.Execution)
	}
	models.mu.Lock()
	imageCalls := models.images
	editCalls := models.edits
	models.mu.Unlock()
	if imageCalls != 2 || editCalls != 1 {
		t.Fatalf("provider calls: images=%d edits=%d", imageCalls, editCalls)
	}
	readOnly := profile
	readOnly.Modules.FS = &stdlib.FileSystemOptions{Root: attachedRoot, ReadOnly: true}
	_, err = stdlib.Execute(ctx, stdlib.Source{Name: "[readonly attached image client]", Virtual: true, Module: true, Code: `
        import {generate_image} from "noe:image";
        try{await generate_image({provider:"fixture",model:"image",prompt:"reject before model",output:"/rejected.png"});throw Error("readonly accepted")}
        catch(e){if(e.code!=="filesystem")throw e}
    `}, readOnly)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	models.mu.Lock()
	after := models.images
	models.mu.Unlock()
	if after != imageCalls {
		t.Fatal("read-only attached view used writable Host template")
	}
	mu.Lock()
	saved := savedEvents
	mu.Unlock()
	if saved != 3 {
		t.Fatalf("save events=%d", saved)
	}
}
