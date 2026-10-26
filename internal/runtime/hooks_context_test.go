package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestHooksContextNotificationHistoryMeasuresPreviousRequest(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "snapshots.jsonl")
	writePhaseHook(t, cwd, "context.mjs", fmt.Sprintf(`import {existsSync,readFileSync,writeFileSync} from "node:fs";
const path=%q;
let before=0;
export async function beforeModelRequest(event,api) {
 const context=await api.context.get();
 writeFileSync(path,(existsSync(path)?readFileSync(path,"utf8"):"")+JSON.stringify(context)+"\n");
 if(++before===1) {
  await api.context.setRuntimeInfo({key:"fixture",text:"superseded runtime text"});
  await api.context.setRuntimeInfo({key:"fixture",text:"current runtime text"});
  await api.agents.steer_agent({runId:event.runId,instruction:"steer must remain once",commandId:"context-steer"});
 }
}

export async function afterModelResponse(event,api) {
 const context=await api.context.get();
 if(context.previous?.requestId!==event.requestId) throw Error("after response context request mismatch");
 let rejected=false;
 try { await api.context.setRuntimeInfo({key:"late",text:"invalid phase"}); } catch (_) { rejected=true; }
 if(!rejected) throw Error("runtime info accepted after model response");
}
export async function beforeTool(event,api) {
 const context=await api.context.get();
 if(context.modelStepId!==event.modelStepId) throw Error("before tool context step mismatch");
}
export async function afterTool(event,api) {
 const context=await api.context.get();
 if(context.modelStepId!==event.modelStepId) throw Error("after tool context step mismatch");
}`, path))
	var mu sync.Mutex
	var requests []provider.Request
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		n := len(requests)
		mu.Unlock()
		if n == 1 {
			r := hookToolReply(map[string]string{"one": "// Return a small fixture value.\nconsole.log('value');"}, "one")
			r.Usage = json.RawMessage(`{"prompt_tokens":4096,"prompt_tokens_details":{"cached_tokens":3072},"completion_tokens":8,"total_tokens":4104}`)
			return r, nil
		}
		return agentAnswer("done"), nil
	})
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,contextBudget:{inputTokens:9000},hooks:["./context.mjs"]},"immutable original task")));`
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || !strings.Contains(out.String(), `"state":"completed"`) {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	// Each sent status notice remains in history beside provider and tool messages.
	if len(requests[1].Messages) != len(requests[0].Messages)+3 {
		t.Fatalf("notice accumulated: first=%s second=%s", requests[0].Messages, requests[1].Messages)
	}
	firstMessages := decodeRuntimeProviderMessages(t, requests[0].Messages)
	secondMessages := decodeRuntimeProviderMessages(t, requests[1].Messages)
	firstStatuses := runtimeContextStatusMessages(firstMessages)
	secondStatuses := runtimeContextStatusMessages(secondMessages)
	if len(firstStatuses) != 1 || len(secondStatuses) != 2 || firstStatuses[0].Role != "user" || secondStatuses[0].Role != "user" || secondStatuses[1].Role != "user" || !strings.HasPrefix(secondMessages[len(secondMessages)-1].Content, "NOE_CONTEXT_STATUS\n") {
		t.Fatalf("status notices were not retained as user history with a current tail: first=%+v second=%+v", firstMessages, secondMessages)
	}
	for n, req := range requests {
		body, _ := json.Marshal(req.Messages)
		if bytes.Contains(body, []byte("superseded runtime text")) || bytes.Contains(body, []byte("invalid phase")) {
			t.Fatalf("stale/late info on request %d: %s", n, body)
		}
		if n == 0 && bytes.Count(body, []byte("current runtime text")) != 1 {
			t.Fatalf("replacement lost/duplicated: %s", body)
		}
		if n == 1 && (bytes.Count(body, []byte("current runtime text")) != 1 || strings.Contains(secondStatuses[len(secondStatuses)-1].Content, "current runtime text")) {
			t.Fatalf("prior status should retain step info without carrying it into the current notice: %s", body)
		}
		messages := decodeRuntimeProviderMessages(t, req.Messages)
		if bytes.Count(body, []byte("steer must remain once")) != 1 || !runtimeMessageContains(messages, "system", agent.Facts()) || messages[0].Content != firstMessages[0].Content {
			t.Fatalf("history changed: %s", body)
		}
	}
	secondBody, _ := json.Marshal(requests[1].Messages)
	if !bytes.Contains(secondBody, []byte("retain tool reasoning")) {
		t.Fatalf("reasoning lost: %s", requests[1].Messages)
	}
	lines, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type hookContextSnapshot struct {
		RunID       string `json:"runId"`
		ModelStepID string `json:"modelStepId"`
		Budget      struct {
			InputTokens int `json:"inputTokens"`
		} `json:"budget"`
		Previous *struct {
			RequestID    string `json:"requestId"`
			PromptTokens *int   `json:"promptTokens"`
			CachedTokens *int   `json:"cachedTokens"`
		} `json:"previous"`
		Estimate *struct {
			Tokens int `json:"tokens"`
		} `json:"estimate"`
	}
	var snapshots []hookContextSnapshot
	for _, line := range bytes.Split(bytes.TrimSpace(lines), []byte("\n")) {
		var s hookContextSnapshot
		if err := json.Unmarshal(line, &s); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, s)
	}
	if len(snapshots) != 2 {
		t.Fatalf("before refired: %s", lines)
	}
	a, b := snapshots[0], snapshots[1]
	if a.Previous != nil || a.RunID == "" || a.ModelStepID == b.ModelStepID || a.Budget.InputTokens != 9000 || a.Estimate == nil || a.Estimate.Tokens <= 0 || b.Previous == nil || b.Previous.PromptTokens == nil || *b.Previous.PromptTokens != 4096 || b.Previous.CachedTokens == nil || *b.Previous.CachedTokens != 3072 || b.Previous.RequestID == "" {
		t.Fatalf("snapshots: %s", lines)
	}
	var final struct {
		Context hookContextSnapshot `json:"context"`
	}
	if err := json.Unmarshal(out.Bytes(), &final); err != nil {
		t.Fatal(err)
	}
	if final.Context.Previous == nil || final.Context.Previous.RequestID == b.Previous.RequestID || final.Context.Previous.PromptTokens != nil || final.Context.Previous.CachedTokens != nil {
		t.Fatalf("last request reused old measurements: %s", out.Bytes())
	}
}

func TestHooksContextBudgetSurvivesRunJSChildDefaults(t *testing.T) {
	var mu sync.Mutex
	seen := map[string]string{}
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		input := interruptUserPrompt(req)
		messages := make([]runtimeProviderMessage, 0, len(req.Messages))
		for _, raw := range req.Messages {
			var message runtimeProviderMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			messages = append(messages, message)
		}
		if input == "" || len(messages) == 0 || !strings.HasPrefix(messages[len(messages)-1].Content, "NOE_CONTEXT_STATUS\n") {
			return provider.Response{}, fmt.Errorf("provider request lost business task or status tail: %+v", messages)
		}
		mu.Lock()
		_, previous := seen[input]
		seen[input] = messages[len(messages)-1].Content
		mu.Unlock()
		if input == "parent-budget" && !previous {
			return hookToolReply(map[string]string{"children": `// Check inherited and cleared budgets through ordinary child helpers.
import {run_agent} from "noe:agent";
const inherited=await run_agent({name:"inherited"},"inherited-budget");
const cleared=await run_agent({name:"cleared",contextBudget:null},"cleared-budget");
if(inherited.context.budget.inputTokens!==9000 || cleared.context.budget.inputTokens!==null) throw Error("child budget mismatch "+JSON.stringify({inherited,cleared}));
console.log("child budgets verified");`}, "children"), nil
		}
		return agentAnswer("done"), nil
	})
	var out, stderr bytes.Buffer
	source := `import {run_agent} from "noe:agent"; console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,contextBudget:{inputTokens:9000}},"parent-budget")));`
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{Name: filepath.Join(t.TempDir(), "main.js"), Code: source}, Options{Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || !strings.Contains(out.String(), `"state":"completed"`) {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(seen["inherited-budget"], "Input budget: 9000") || (!strings.Contains(seen["cleared-budget"], "Input budget: 1000000 tokens") || !strings.Contains(seen["cleared-budget"], "Explicit input budget: not set")) {
		t.Fatalf("run_js defaults lost inheritance/clear: %+v", seen)
	}
}
