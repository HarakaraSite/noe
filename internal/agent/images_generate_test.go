package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
)

type imageProviderFunc func(context.Context, imageapi.Request) (imageapi.Generated, error)

func (f imageProviderFunc) GenerateImage(ctx context.Context, request imageapi.Request) (imageapi.Generated, error) {
	return f(ctx, request)
}

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

func attachExecutionGroup(h *Host, op *Operation, group *executionGroup) {
	h.mu.Lock()
	op.group = group
	h.groups[group.id] = group
	h.mu.Unlock()
}

func TestGenerateImageRPCSavesResultAndReportsImageEvents(t *testing.T) {
	dir := t.TempDir()
	content := testPNG(t)
	observed := make(chan events.Event, 8)
	var got imageapi.Request
	h := NewHost(Options{
		ImageProvider: imageProviderFunc(func(_ context.Context, request imageapi.Request) (imageapi.Generated, error) {
			got = request
			return imageapi.Generated{
				Content: content, ResponseModel: "response-image-model", RequestID: "fixture-request",
				Usage: json.RawMessage(`{"input_tokens":4,"output_tokens":2,"total_tokens":6,"cost":0.001}`),
			}, nil
		}),
		Cwd: dir, Observe: func(event events.Event) { observed <- event },
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	group := &executionGroup{id: "image-root"}
	attachExecutionGroup(h, op, group)

	response, err := op.Call("generate_image", `{"provider":"openai-api","model":"gpt-image-2.5-flare","prompt":"secret prompt","output":"result.png","parameters":{"quality":"low"}}`)
	if err != nil {
		t.Fatal(err)
	}
	var result imageapi.Response
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		t.Fatalf("decode response %s: %v", response, err)
	}
	if !filepath.IsAbs(got.Output) || got.Output != filepath.Join(dir, "result.png") || result.Path != got.Output {
		t.Fatalf("provider/output path mismatch: request=%+v response=%+v", got, result)
	}
	if result.Provider != "openai-api" || result.Model != "gpt-image-2.5-flare" || result.ResponseModel != "response-image-model" || result.RequestID != "fixture-request" || result.Bytes != len(content) || result.Width != 2 || result.Height != 3 || string(result.Usage) == "" {
		t.Fatalf("image result metadata: %+v", result)
	}
	saved, err := os.ReadFile(result.Path)
	if err != nil || !bytes.Equal(saved, content) {
		t.Fatalf("saved image differs: read error=%v bytes match=%v", err, bytes.Equal(saved, content))
	}

	var lifecycle []string
	for len(observed) > 0 {
		event := <-observed
		kind, _ := event["type"].(string)
		if kind == "image.started" || kind == "image.completed" {
			lifecycle = append(lifecycle, kind)
			if _, exists := event["prompt"]; exists {
				t.Fatalf("prompt leaked into event: %+v", event)
			}
			if event["imageModel"] != got.Model || event["provider"] != got.Provider {
				t.Fatalf("image event selection metadata: %+v", event)
			}
			encoded, _ := json.Marshal(event)
			if strings.Contains(string(encoded), "secret prompt") || strings.Contains(string(encoded), "data:image/") || strings.Contains(string(encoded), "fixture-secret") {
				t.Fatalf("sensitive data escaped into event: %s", encoded)
			}
		}
	}
	if strings.Join(lifecycle, ",") != "image.started,image.completed" {
		t.Fatalf("image lifecycle = %v", lifecycle)
	}
	h.mu.Lock()
	counters, usage := group.counters, group.usage
	h.mu.Unlock()
	if counters.ProviderCalls != 1 || counters.ImageProviderCalls != 1 || counters.AgentProviderCalls != 0 || counters.DecisionProviderCalls != 0 {
		t.Fatalf("image call counters: %+v", counters)
	}
	if usage.TotalTokens == nil || *usage.TotalTokens != 6 || usage.Cost == nil || *usage.Cost != 0.001 {
		t.Fatalf("image usage was not aggregated: %+v", usage)
	}
}

func TestGenerateImageRPCReturnsStructuredFailureAndCancellationEvent(t *testing.T) {
	observed := make(chan events.Event, 8)
	providerError := &imageapi.Error{Code: "refusal", Message: "provider refused", ProviderCode: "moderation_blocked", StatusCode: 400}
	h := NewHost(Options{
		ImageProvider: imageProviderFunc(func(context.Context, imageapi.Request) (imageapi.Generated, error) {
			return imageapi.Generated{}, providerError
		}),
		Observe: func(event events.Event) { observed <- event },
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	group := &executionGroup{id: "image-root"}
	attachExecutionGroup(h, op, group)

	response, err := op.Call("generate_image", `{"provider":"openrouter","model":"openai/gpt-image-2.5-flare","prompt":"prompt","output":"result.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ImageError imageapi.Error `json:"imageError"`
	}
	if err := json.Unmarshal([]byte(response), &body); err != nil || body.ImageError.Code != "refusal" || body.ImageError.ProviderCode != "moderation_blocked" || body.ImageError.StatusCode != 400 {
		t.Fatalf("structured image error: %s (%v)", response, err)
	}
	var lifecycle []string
	for len(observed) > 0 {
		event := <-observed
		if event["type"] == "image.started" || event["type"] == "image.failed" || event["type"] == "image.cancelled" {
			lifecycle = append(lifecycle, event["type"].(string))
		}
	}
	if strings.Join(lifecycle, ",") != "image.started,image.failed" {
		t.Fatalf("failure lifecycle = %v", lifecycle)
	}

	cancelStarted := make(chan struct{})
	cancelledEvents := make(chan string, 4)
	cancelHost := NewHost(Options{
		ImageProvider: imageProviderFunc(func(ctx context.Context, _ imageapi.Request) (imageapi.Generated, error) {
			close(cancelStarted)
			<-ctx.Done()
			return imageapi.Generated{}, ctx.Err()
		}),
		Observe: func(event events.Event) {
			if kind, _ := event["type"].(string); strings.HasPrefix(kind, "image.") {
				cancelledEvents <- kind
			}
		},
	})
	defer cancelHost.Close()
	cancelOp := cancelHost.NewOperation(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := cancelOp.Call("generate_image", `{"provider":"openrouter","model":"model","prompt":"prompt","output":"result.png"}`)
		done <- err
	}()
	<-cancelStarted
	cancelOp.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled image RPC did not return")
	}
	cancelHost.Close()
	var cancelled bool
	for len(cancelledEvents) > 0 {
		cancelled = cancelled || <-cancelledEvents == "image.cancelled"
	}
	if !cancelled {
		t.Fatal("operation cancellation emitted no image.cancelled event")
	}
}

func TestGenerateImageRPCAdmissionUsesImageProviderLimit(t *testing.T) {
	var calls int
	observed := make(chan events.Event, 8)
	h := NewHost(Options{
		ImageProvider: imageProviderFunc(func(context.Context, imageapi.Request) (imageapi.Generated, error) {
			calls++
			return imageapi.Generated{}, errors.New("provider must not be called")
		}),
		Observe: func(event events.Event) { observed <- event },
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	group := &executionGroup{id: "image-root", limits: ExecutionLimits{ProviderCalls: limitPtr(0)}}
	attachExecutionGroup(h, op, group)

	response, err := op.Call("generate_image", `{"provider":"openrouter","model":"model","prompt":"prompt","output":"result.png"}`)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		ImageError imageapi.Error `json:"imageError"`
	}
	if json.Unmarshal([]byte(response), &body) != nil || body.ImageError.Code != "cancelled" || calls != 0 {
		t.Fatalf("limit response=%s calls=%d", response, calls)
	}
	var started, limitReached bool
	for len(observed) > 0 {
		event := <-observed
		started = started || event["type"] == "image.started"
		limitReached = limitReached || event["type"] == "execution.limit_reached"
	}
	if started || !limitReached {
		t.Fatalf("limit admission events: started=%v limitReached=%v", started, limitReached)
	}
}

func TestGenerateImageRPCConcurrentIndependentRequests(t *testing.T) {
	content := testPNG(t)
	var mu sync.Mutex
	seen := make(map[string]bool)
	h := NewHost(Options{ImageProvider: imageProviderFunc(func(_ context.Context, request imageapi.Request) (imageapi.Generated, error) {
		mu.Lock()
		seen[request.Prompt] = true
		mu.Unlock()
		return imageapi.Generated{Content: content}, nil
	})})
	defer h.Close()
	const count = 5
	var wait sync.WaitGroup
	errorsByCall := make(chan error, count)
	for index := 0; index < count; index++ {
		index := index
		wait.Add(1)
		go func() {
			defer wait.Done()
			op := h.NewOperation(context.Background())
			defer op.Close()
			prompt := "request-" + string(rune('0'+index))
			payload, _ := json.Marshal(map[string]any{"provider": "openrouter", "model": "model", "prompt": prompt, "output": filepath.Join(t.TempDir(), prompt+".png")})
			response, err := op.Call("generate_image", string(payload))
			if err == nil {
				var result imageapi.Response
				err = json.Unmarshal([]byte(response), &result)
				if err == nil && result.Bytes != len(content) {
					err = errors.New("concurrent image response metadata changed")
				}
			}
			errorsByCall <- err
		}()
	}
	wait.Wait()
	close(errorsByCall)
	for err := range errorsByCall {
		if err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != count {
		t.Fatalf("provider calls = %d, want %d", len(seen), count)
	}
}
