package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent"
	"noe/internal/agent/provider"
)

type agentProviderFunc func(context.Context, provider.Request) (provider.Response, error)

func (f agentProviderFunc) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	return f(ctx, request)
}

func agentAnswer(text string) provider.Response {
	message, _ := json.Marshal(map[string]string{"role": "assistant", "content": text})
	return provider.Response{Message: message, FinishReason: "stop"}
}

func TestAgentMinimalRunHasIndependentHistories(t *testing.T) {
	var mu sync.Mutex
	var requests []provider.Request
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		var user struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(req.Messages[len(req.Messages)-1], &user); err != nil {
			return provider.Response{}, err
		}
		return agentAnswer(user.Content), nil
	})
	var out bytes.Buffer
	source := `import {createAgent} from "noe:agent";
const a = createAgent({model:"fixture/model",system:"additional",maxTokens:123});
console.log(await a.run("first",{cacheKey:"cache-1"}));
console.log(await a.run("second"));
console.log(JSON.stringify(await Promise.all([a.run("left"),a.run("right")])));`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{
		Stdout: &out, Agent: &agent.Options{Provider: p},
	})
	if code != 0 || err != nil || out.String() != "first\nsecond\n[\"left\",\"right\"]\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, &out)
	}
	if len(requests) != 4 {
		t.Fatalf("requests=%d", len(requests))
	}
	for _, req := range requests {
		if req.Model != "fixture/model" || req.MaxTokens != 123 || len(req.Messages) != 2 || !strings.Contains(string(req.Messages[0]), "additional") {
			t.Fatalf("settings/history not isolated: %+v", req)
		}
	}
	if requests[0].CacheKey != "cache-1" || requests[1].CacheKey != "" {
		t.Fatalf("cacheKey leaked: %+v", requests)
	}
}

func TestAgentLazyImportAndConfigurationErrors(t *testing.T) {
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("unexpected"), nil
	})
	for _, source := range []string{
		`import {createAgent} from "noe:agent"; createAgent({model:"fixture/model"});`,
		`import {createAgent} from "noe:agent"; let failures=0;
for (const config of [{},{model:42},{model:"x",provider:"unknown"},{model:"x",maxTokens:-1}]) {
  try { createAgent(config); } catch { failures++; }
}
const a=createAgent({model:"x"});
for (const [instruction,options] of [["",{}],[42,{}],["x",{maxSteps:-1}],["x",{cacheKey:42}]]) {
  try { await a.run(instruction,options); } catch { failures++; }
}
if(failures!==8) throw Error("validation missed "+failures);`,
	} {
		code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: &agent.Options{Provider: p}})
		if code != 0 || err != nil {
			t.Fatalf("code=%d error=%v", code, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unused/invalid agent invoked provider %d times", calls.Load())
	}
	// Prove import/create do not read a missing credential either.
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), `import {createAgent} from "noe:agent";createAgent({model:"x"});`},
		Options{Agent: &agent.Options{ProviderOptions: provider.Options{KeyFile: filepath.Join(t.TempDir(), "missing")}}})
	if code != 0 || err != nil {
		t.Fatalf("lazy auth code=%d error=%v", code, err)
	}
}

func TestAgentCancellationStopsProvider(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return provider.Response{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() { <-started; cancel() }()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {createAgent} from "noe:agent";await createAgent({model:"x"}).run("wait");`}, Options{Agent: &agent.Options{Provider: p}})
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("code=%d error=%v", code, err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("provider survived runtime cancellation")
	}
}

func TestAgentInternalRPCLongerThanFetchTimeout(t *testing.T) {
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		timer := time.NewTimer(30*time.Second + 250*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return agentAnswer("long request completed"), nil
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {createAgent} from "noe:agent";console.log(await createAgent({model:"x"}).run("wait"));`}, Options{Stdout: &out, Agent: &agent.Options{Provider: p}})
	if code != 0 || err != nil || out.String() != "long request completed\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, &out)
	}
}
