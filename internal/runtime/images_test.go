package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func runtimeImage(t *testing.T) (string, []byte) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 128, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 128; x++ {
			if x < 64 {
				img.Set(x, y, color.RGBA{255, 0, 0, 255})
			} else {
				img.Set(x, y, color.RGBA{0, 0, 255, 255})
			}
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "input.png")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	return path, data.Bytes()
}
func runtimeImageParts(messages []json.RawMessage) []string {
	var urls []string
	for _, raw := range messages {
		var msg struct {
			Content []struct {
				Type  string `json:"type"`
				Image struct {
					URL string `json:"url"`
				} `json:"image_url"`
			} `json:"content"`
		}
		if json.Unmarshal(raw, &msg) == nil {
			for _, p := range msg.Content {
				if p.Type == "image_url" {
					urls = append(urls, p.Image.URL)
				}
			}
		}
	}
	return urls
}
func TestImageRunJSAutoAttachmentAndQueueContinuation(t *testing.T) {
	path, content := runtimeImage(t)
	encodedPath, _ := json.Marshal(path)
	var mu sync.Mutex
	calls := 0
	var observed []events.Event
	p := agentProviderFunc(func(_ context.Context, r provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return decisionTool(`// Read the image without logging its metadata
const {read_image}=await import("noe:image");await read_image(` + string(encodedPath) + `);const fs=await import("node:fs");fs.writeFileSync(` + string(encodedPath) + `,"changed");`), nil
		}
		urls := runtimeImageParts(r.Messages)
		if len(urls) != 1 {
			t.Errorf("images lost on call %d: %s", calls, r.Messages)
		} else if urls[0] != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(content) {
			t.Error("snapshot was not preserved")
		}
		return agentAnswer("red left blue right"), nil
	})
	opts := agentTestOptions(p)
	opts.Observe = func(e events.Event) { mu.Lock(); defer mu.Unlock(); observed = append(observed, e) }
	source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"read image",{keepAlive:true});
await queue_agent({agentId,instruction:"describe it again",commandId:"second"});
while(true){const s=await observe_agent({agentId});if(s.state==="failed"||s.state==="stopped")throw Error(JSON.stringify(s));if(s.state==="idle"&&s.lastResult?.commandId==="second"){if(s.lastResult.state!=="completed")throw Error(JSON.stringify(s));break;}await new Promise(r=>setTimeout(r,5));}
await interrupt_agent({agentId});`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 {
		t.Fatalf("calls=%d", calls)
	}
	hasRead, hasTool, hasEstimate := false, false, false
	for _, e := range observed {
		raw, _ := json.Marshal(e)
		if bytes.Contains(raw, []byte("data:image/")) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(content))) {
			t.Fatalf("image payload leaked into event %s", raw)
		}
		switch e["type"] {
		case "image.read":
			hasRead = true
		case "tool.finished":
			hasTool = true
			if !bytes.Contains(raw, []byte(`"rawImages":[`)) || !bytes.Contains(raw, []byte(`"images":[`)) {
				t.Fatalf("missing metadata %s", raw)
			}
		case "context.updated":
			if bytes.Contains(raw, []byte(`"imageTokens":10`)) {
				hasEstimate = true
			}
		}
	}
	if !hasRead || !hasTool || !hasEstimate {
		t.Fatalf("read=%v tool=%v estimate=%v", hasRead, hasTool, hasEstimate)
	}
}
func TestImageHookSelectionAndPhysicalFailure(t *testing.T) {
	for _, mode := range []string{"keep", "drop", "invalid", "throw", "physical_failure", "sibling_failure"} {
		t.Run(mode, func(t *testing.T) {
			path, _ := runtimeImage(t)
			cwd := t.TempDir()
			encodedPath, _ := json.Marshal(path)
			hook := `export function afterTool(e){if(e.images.length){return {result:null};}}`
			switch mode {
			case "drop":
				hook = `export function afterTool(e){return {result:null,imageIds:[]};}`
			case "invalid":
				hook = `export function afterTool(e){return {result:null,imageIds:["foreign"]};}`
			case "throw":
				hook = `export function afterTool(e){throw Error("hook failed");}`
			}
			if err := os.WriteFile(filepath.Join(cwd, "hook.js"), []byte(hook), 0600); err != nil {
				t.Fatal(err)
			}
			var mu sync.Mutex
			calls := 0
			p := agentProviderFunc(func(_ context.Context, r provider.Request) (provider.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				calls++
				if calls == 1 {
					code := `// Read the fixture image
const {read_image}=await import("noe:image");await read_image(` + string(encodedPath) + `);`
					if mode == "physical_failure" {
						code += `throw Error("after read");`
					}
					if mode == "sibling_failure" {
						return hookToolReply(map[string]string{"a": code, "b": `// Fail independently
throw Error("ordinary tool failure");`}, "a", "b"), nil
					}
					return decisionTool(code), nil
				}
				urls := runtimeImageParts(r.Messages)
				want := 1
				if mode == "drop" || mode == "physical_failure" {
					want = 0
				}
				if len(urls) != want {
					t.Errorf("image count=%d want%d history=%s", len(urls), want, r.Messages)
				}
				return agentAnswer("done"), nil
			})
			opts := agentTestOptions(p)
			source := `import {run_agent} from "noe:agent";const s=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,hooks:["./hook.js"]},"image");`
			if mode == "invalid" || mode == "throw" {
				source += `if(s.lastResult.state!=="failed")throw Error(JSON.stringify(s));`
			} else {
				source += `if(s.lastResult.state!=="completed")throw Error(JSON.stringify(s));`
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			code, err := Execute(ctx, Source{filepath.Join(cwd, "main.js"), source}, Options{Cwd: cwd, Agent: opts})
			if code != 0 || err != nil {
				t.Fatalf("code=%d err=%v", code, err)
			}
			mu.Lock()
			defer mu.Unlock()
			want := 2
			if mode == "invalid" || mode == "throw" {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls=%d want%d", calls, want)
			}
		})
	}
}
func TestImageModuleLazyRootReadErrorAndUnsupportedGeneration(t *testing.T) {
	runStdlib(t, `import * as im from "noe:image";
const read=await im.read_image("missing.png").catch(e=>[e.name,e.code].join(":"));
if(read!=="ImageError:input")throw Error(read);
const generation=await im.generate_image({provider:"openai-chatgpt",model:"gpt-image-2.5-flare",prompt:"circle",output:"image.png"}).catch(e=>[e.name,e.code].join(":"));
if(generation!=="ImageError:unsupported")throw Error(generation);`, Options{})
}

type runtimeImageProvider func(context.Context, imageapi.Request) (imageapi.Generated, error)

func (f runtimeImageProvider) GenerateImage(ctx context.Context, r imageapi.Request) (imageapi.Generated, error) {
	return f(ctx, r)
}
func TestImageGenerationPublicModuleSavesWithoutAutomaticAttachment(t *testing.T) {
	_, data := runtimeImage(t)
	path := filepath.Join(t.TempDir(), "output.png")
	encoded, _ := json.Marshal(path)
	opts := agentTestOptions(agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		t.Fatal("unexpected chat call")
		return provider.Response{}, nil
	}))
	opts.ImageProvider = runtimeImageProvider(func(context.Context, imageapi.Request) (imageapi.Generated, error) {
		return imageapi.Generated{Content: data, ResponseModel: "image/revision"}, nil
	})
	source := `import {generate_image} from "noe:image";const x=await generate_image({provider:"openai-api",model:"image/model",prompt:"circle",output:` + string(encoded) + `});if(x.width!==128||x.mimeType!=="image/png"||"imageId" in x||JSON.stringify(x).includes("base64"))throw Error(JSON.stringify(x));`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	saved, _ := os.ReadFile(path)
	if !bytes.Equal(saved, data) {
		t.Fatal("generation bytes differ")
	}
}
func TestImageVisionRefusalPreservesExistingErrorAndDetails(t *testing.T) {
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":null,"refusal":"provider refusal reason"}`), FinishReason: "stop"}, nil
	})
	source := `import {run_agent} from "noe:agent";const x=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"image");if(x.lastResult.state!=="failed"||!x.lastResult.error.includes("model refused the request")||x.lastResult.errorDetails?.code!=="refusal"||x.lastResult.errorDetails.providerMessage!=="provider refusal reason")throw Error(JSON.stringify(x));`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestImageAgentGeneratesWithIndependentProviderAndReadsBack(t *testing.T) {
	_, data := runtimeImage(t)
	path := filepath.Join(t.TempDir(), "generated.png")
	encoded, _ := json.Marshal(path)
	var mu sync.Mutex
	calls := 0
	var eventsSeen []events.Event
	p := agentProviderFunc(func(_ context.Context, r provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return decisionTool(`// Generate an image with an independent provider and inspect it
const {generate_image,read_image}=await import("noe:image");const result=await generate_image({provider:"openai-api",model:"image/model",prompt:"a circle",output:` + string(encoded) + `});await read_image(result.path);`), nil
		}
		if len(runtimeImageParts(r.Messages)) != 1 {
			t.Error("generated file did not reach vision parent")
		}
		return agentAnswer("generated and inspected"), nil
	})
	opts := agentTestOptions(p)
	opts.ImageProvider = runtimeImageProvider(func(ctx context.Context, r imageapi.Request) (imageapi.Generated, error) {
		if r.Provider != "openai-api" || r.Model != "image/model" || r.RunID == "" {
			t.Errorf("generation inheritance changed explicit config: %+v", r)
		}
		return imageapi.Generated{Content: data, Usage: json.RawMessage(`{"input_tokens":3,"output_tokens":4}`)}, nil
	})
	opts.Observe = func(e events.Event) { mu.Lock(); defer mu.Unlock(); eventsSeen = append(eventsSeen, e) }
	source := `import {run_agent} from "noe:agent";const s=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",executionLimits:{providerCalls:3}},"make and inspect");if(s.lastResult?.answer!=="generated and inspected")throw Error(JSON.stringify(s));`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	completed := 0
	for _, e := range eventsSeen {
		if e["type"] == "image.completed" {
			completed++
			body, _ := json.Marshal(e)
			if !bytes.Contains(body, []byte(`"imageProviderCalls":1`)) || !bytes.Contains(body, []byte(`"decisionProviderCalls":0`)) || !bytes.Contains(body, []byte(`"providerCalls":2`)) {
				t.Fatalf("generation counters=%s", body)
			}
		}
	}
	if completed != 1 {
		t.Fatalf("generation terminals=%d", completed)
	}
}

func TestImageHistoryTrimmingKeepsLatestCompleteExchange(t *testing.T) {
	path, _ := runtimeImage(t)
	cwd := t.TempDir()
	encoded, _ := json.Marshal(path)
	hook := `export function afterTool(e){return {result:{stdout:"x".repeat(18000),stderr:"",exitCode:0,reason:"completed"}};}`
	if err := os.WriteFile(filepath.Join(cwd, "trim.js"), []byte(hook), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	calls, trims := 0, 0
	p := agentProviderFunc(func(_ context.Context, r provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls > 1 {
			if len(runtimeImageParts(r.Messages)) != 1 {
				t.Errorf("old image survived trim: %s", r.Messages)
			}
			var toolIDs []string
			for _, raw := range r.Messages {
				var m struct {
					Role string `json:"role"`
					ID   string `json:"tool_call_id"`
				}
				json.Unmarshal(raw, &m)
				if m.Role == "tool" {
					toolIDs = append(toolIDs, m.ID)
				}
			}
			if len(toolIDs) != 1 || toolIDs[0] != fmt.Sprint("tool-", calls-1) {
				t.Errorf("trim split complete image exchange: %v", toolIDs)
			}
		}
		if calls == 4 {
			return agentAnswer("trimmed images"), nil
		}
		id := fmt.Sprint("tool-", calls)
		return hookToolReply(map[string]string{id: `// Read an image for this complete history block
const {read_image}=await import("noe:image");await read_image(` + string(encoded) + `);`}, id), nil
	})
	opts := agentTestOptions(p)
	opts.Observe = func(e events.Event) {
		if e["type"] == "context.trimmed" {
			mu.Lock()
			trims++
			mu.Unlock()
		}
	}
	source := `import {run_agent} from "noe:agent";const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:4,contextBudget:{inputTokens:6000},hooks:["./trim.js"]},"retain image blocks");if(r.lastResult.answer!=="trimmed images")throw Error(JSON.stringify(r));`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(cwd, "main.js"), source}, Options{Cwd: cwd, Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 4 || trims < 2 {
		t.Fatalf("calls=%d trims=%d", calls, trims)
	}
}

func TestImagePublicValidationUsesStructuredInputError(t *testing.T) {
	source := `import {read_image,generate_image} from "noe:image";
for(const invoke of [()=>read_image(""),()=>read_image("x",{detail:"original"}),()=>generate_image({}),()=>generate_image({provider:"openai-api",model:"m",prompt:"p",output:"x",parameters:{n:2}})]){
try{await invoke();throw Error("accepted invalid input");}catch(e){if(e.name!=="ImageError"||e.code!=="input"||!e.message)throw e;}}
`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}
