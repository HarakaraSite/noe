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
	source := `import {run_agent} from "noe:agent";
const config={provider:"openrouter",model:"fixture/model",system:"additional",maxTokens:123,effort:"high"};
const first=await run_agent({...config,cacheKey:"cache-1"},"first");
const second=await run_agent(config,"second");
const [left,right]=await Promise.all([run_agent(config,"left"),run_agent(config,"right")]);
const empty=await run_agent({...config,cacheKey:""},"empty");
for (const result of [first,second,left,right,empty]) {
  if (result.state!=="stopped" || result.reason!=="task_completed" || result.lastResult.state!=="completed") throw Error(JSON.stringify(result));
}
console.log(JSON.stringify([first.lastResult.answer,second.lastResult.answer,[left.lastResult.answer,right.lastResult.answer],empty.lastResult.answer]));`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{
		Stdout: &out, Agent: agentTestOptions(p),
	})
	if code != 0 || err != nil || out.String() != "[\"first\",\"second\",[\"left\",\"right\"],\"empty\"]\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, &out)
	}
	if len(requests) != 5 {
		t.Fatalf("requests=%d", len(requests))
	}
	byInstruction := make(map[string]provider.Request, len(requests))
	for _, req := range requests {
		if req.Model != "fixture/model" || req.MaxTokens != 123 || req.Effort != "high" || len(req.Messages) != 2 || !strings.Contains(string(req.Messages[0]), "additional") {
			t.Fatalf("settings/history not isolated: %+v", req)
		}
		var system struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(req.Messages[0], &system); err != nil {
			t.Fatal(err)
		}
		if system.Role != "system" || system.Content != agent.Instructions()+"\nadditional" {
			t.Fatalf("embedded Markdown and custom instructions were not composed: %+v", system)
		}
		var user struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(req.Messages[1], &user); err != nil {
			t.Fatal(err)
		}
		byInstruction[user.Content] = req
	}
	if byInstruction["first"].CacheKey != "cache-1" || byInstruction["empty"].CacheKey != "" {
		t.Fatalf("explicit cacheKey changed: %+v", byInstruction)
	}
	seen := map[string]bool{"cache-1": true}
	for _, instruction := range []string{"second", "left", "right"} {
		req := byInstruction[instruction]
		if req.CacheKey == "" || seen[req.CacheKey] {
			t.Fatalf("automatic cacheKey missing or shared across runs: %+v", byInstruction)
		}
		seen[req.CacheKey] = true
	}
}

func TestAgentLazyImportAndConfigurationErrors(t *testing.T) {
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("unexpected"), nil
	})
	for _, source := range []string{
		`const api=await import("noe:agent");
if (Object.keys(api).sort().join(",")!=="interrupt_agent,list_models,observe_agent,queue_agent,run_agent,spawn_agent,steer_agent") throw Error("unexpected public exports: "+Object.keys(api));
if ("createAgent" in api) throw Error("legacy createAgent is still exported");`,
		`import {run_agent} from "noe:agent"; let failures=0;
for (const config of [{model:""},{model:42},{provider:"unknown"},{maxTokens:-1},{maxSteps:-1},{cacheKey:42}]) {
  const pending=run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",...config},"must not contact provider");
  if (!(pending instanceof Promise)) throw Error("run_agent did not return a promise");
  try { await pending; } catch { failures++; }
}
for (const [config,instruction] of [[{provider:"openrouter",model:"fixture/model",effort:"auto"},""],[{provider:"openrouter",model:"fixture/model"},"task"],[{provider:"openrouter",effort:"auto"},"task"],[{model:"fixture/model",effort:"auto"},"task"]]) {
  try { await run_agent(config,instruction); } catch { failures++; }
}
if(failures!==10) throw Error("async validation missed "+failures);`,
	} {
		code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: agentTestOptions(p)})
		if code != 0 || err != nil {
			t.Fatalf("code=%d error=%v", code, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("unused/invalid agent invoked provider %d times", calls.Load())
	}
	// Prove importing the public API does not read a missing credential.
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent";if(typeof run_agent!=="function")throw Error("missing run_agent");`},
		Options{Agent: &agent.Options{Executable: runtimeTestExecutable, ProviderOptions: provider.Options{KeyFile: filepath.Join(t.TempDir(), "missing")}}})
	if code != 0 || err != nil {
		t.Fatalf("lazy auth code=%d error=%v", code, err)
	}
}

func TestAgentEffortValuesAndValidation(t *testing.T) {
	var mu sync.Mutex
	var efforts []string
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		efforts = append(efforts, req.Effort)
		mu.Unlock()
		return agentAnswer("done"), nil
	})
	source := `import {run_agent} from "noe:agent";
for (const effort of ["auto","none","minimal","low","medium","high","xhigh","max"]) {
  const result=await run_agent({provider:"openrouter",model:"fixture/model",effort},"task");
  if(result.lastResult.state!=="completed")throw Error(JSON.stringify(result));
}
for (const effort of [null,"","HIGH","unknown",1,false,{},["low"]]) {
  let rejected = false;
  try { await run_agent({provider:"openrouter",model:"fixture/model",effort},"task"); } catch (e) {
    if (!(e instanceof TypeError) || !e.message.includes("effort")) throw e;
    rejected = true;
  }
  if (!rejected) throw Error("invalid effort accepted: "+JSON.stringify(effort));
}`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil {
		t.Fatalf("code=%d error=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got := strings.Join(efforts, ","); got != "auto,none,minimal,low,medium,high,xhigh,max" {
		t.Fatalf("effort values/validation: %q", got)
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
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent";await run_agent({provider:"openrouter",model:"x",effort:"auto"},"wait");`}, Options{Agent: agentTestOptions(p)})
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
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"x",effort:"auto"},"wait");console.log(result.lastResult.answer);`}, Options{Stdout: &out, Agent: agentTestOptions(p)})
	if code != 0 || err != nil || out.String() != "long request completed\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, &out)
	}
}
