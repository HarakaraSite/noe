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

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
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
		return agentAnswer(interruptUserPrompt(req)), nil
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
	seenSessions := make(map[string]bool, len(requests))
	for _, req := range requests {
		if req.Model != "fixture/model" || req.MaxTokens != 123 || req.Effort != "high" || len(req.Messages) != 3 {
			t.Fatalf("settings/history not isolated: %+v", req)
		}
		messages := decodeRuntimeProviderMessages(t, req.Messages)
		if !runtimeMessageContains(messages, "system", agent.Instructions()+"\nadditional") {
			t.Fatalf("embedded Markdown and custom instructions were not composed: %+v", messages)
		}
		instruction := interruptUserPrompt(req)
		if instruction == "" || !runtimeMessageContains(messages, "user", instruction) {
			t.Fatalf("business instruction missing from provider history: %+v", messages)
		}
		if req.SessionID == "" || seenSessions[req.SessionID] {
			t.Fatalf("agent instances did not get independent session IDs: %+v", req)
		}
		seenSessions[req.SessionID] = true
		byInstruction[instruction] = req
	}
	if byInstruction["first"].CacheKey != "cache-1" || byInstruction["empty"].CacheKey != "" {
		t.Fatalf("explicit cacheKey changed: %+v", byInstruction)
	}
	for _, instruction := range []string{"second", "left", "right"} {
		req := byInstruction[instruction]
		if req.CacheKey == "" || req.CacheKey != req.SessionID {
			t.Fatalf("default cacheKey must use the instance session ID: %+v", req)
		}
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
if (Object.keys(api).sort().join(",")!=="interrupt_agent,list_models,observe_agent,queue_agent,read_task_result,run_agent,spawn_agent,steer_agent,wait_task") throw Error("unexpected public exports: "+Object.keys(api));
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
		`import {wait_task} from "noe:agent"; let failures=0;
for(const options of [null,[],{}, {agentId:"a"}, {commandId:"c"}, {agentId:"a",commandId:0}, {agentId:"a",commandId:"c",wait:true}]) {
 const pending=wait_task(options);
 if(!(pending instanceof Promise)) throw Error("wait_task did not return a promise");
 try {await pending;} catch(e) {if(!(e instanceof TypeError))throw e; failures++;}
}
if(failures!==7)throw Error("wait validation missed "+failures);`,
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
