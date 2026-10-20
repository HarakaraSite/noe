package runtime

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func decisionTool(code string) provider.Response {
	args, _ := json.Marshal(map[string]string{"code": code})
	message, _ := json.Marshal(map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "call-decision", "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}}}})
	return provider.Response{Message: message, FinishReason: "tool_calls"}
}

const decisionToolCode = `// Classify the payload and choose a route
const {decide}=await import("noe:decision");
const result=await decide({provider:"openrouter",model:"decision/model",state:"payload",questions:{q:{type:"choice",criteria:{yes:"Y",no:"N"}}}});
console.log(result.answers.q.choice==="yes"?"route-a":"route-b");`

func TestDecisionAgentResultAndModelObservation(t *testing.T) {
	var mu sync.Mutex
	var requests []provider.Request
	var observed []events.Event
	p := agentProviderFunc(func(_ context.Context, r provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, r)
		if len(requests) == 1 {
			return decisionTool(decisionToolCode), nil
		}
		return agentAnswer("route-a"), nil
	})
	opts := agentTestOptions(p)
	opts.DecisionProvider = decisionProviderFunc(func(context.Context, decision.Request) (decision.Response, error) {
		return decision.Response{JSON: json.RawMessage(`{"model":"decision/revision","answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":3,"output_tokens":0,"cost":0.0001}}`), StatusCode: 200}, nil
	})
	opts.Observe = func(e events.Event) { mu.Lock(); defer mu.Unlock(); observed = append(observed, e) }
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent";const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"classify");if(r.lastResult.answer!=="route-a")throw Error(JSON.stringify(r));`}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 || !runtimeMessageContains(decodeRuntimeProviderMessages(t, requests[1].Messages), "tool", "route-a") {
		t.Fatalf("result did not reach agent: %+v", requests)
	}
	var runID, requestID string
	count := 0
	for _, e := range observed {
		if e["type"] == "run_js.started" {
			runID, _ = e["runId"].(string)
		}
		if !strings.HasPrefix(e["type"].(string), "decision.") {
			continue
		}
		count++
		if e["model"] != "fixture/model" || e["decisionModel"] != "decision/model" || e["runId"] != runID || e["toolCallId"] != "call-decision" {
			t.Errorf("lost owner metadata: %+v", e)
		}
		if e["type"] == "decision.started" {
			requestID, _ = e["requestId"].(string)
		}
		if e["requestId"] != requestID || requestID == "" {
			t.Errorf("request mismatch: %+v", e)
		}
		if e["type"] == "decision.completed" && (e["responseModel"] != "decision/revision" || e["statusCode"] != 200) {
			t.Errorf("missing response metadata: %+v", e)
		}
	}
	if count != 2 {
		t.Fatalf("decision event count=%d", count)
	}
}

func TestDecisionAgentInterruptRecoversOwnedRequest(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	gate := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-started:
			w.Write([]byte("ready"))
		case <-r.Context().Done():
		}
	}))
	defer gate.Close()
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		return decisionTool(decisionToolCode), nil
	})
	opts := agentTestOptions(p)
	opts.DecisionProvider = decisionProviderFunc(func(ctx context.Context, _ decision.Request) (decision.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return decision.Response{}, ctx.Err()
	})
	url, _ := json.Marshal(gate.URL)
	source := `import {spawn_agent,observe_agent,interrupt_agent} from "noe:agent";
const child=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"classify");
await fetch(` + string(url) + `).then(r=>r.text());
await interrupt_agent({agentId:child.agentId});
let snapshot;
do {snapshot=await observe_agent({agentId:child.agentId});if(snapshot.state!=="stopped")await new Promise(r=>setTimeout(r,1));}while(snapshot.state!=="stopped");
if(snapshot.reason!=="interrupted")throw Error(JSON.stringify(snapshot));`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "interrupt.js"), source}, Options{Agent: opts})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("owned decision request not recovered")
	}
}
