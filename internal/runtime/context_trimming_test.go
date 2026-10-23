package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

// This exercises actual task JS, broker receipts and provider payloads, rather
// than just checking the deletion algorithm against itself.
func TestContextTrimmingContinuesWithProtectedSteersAndCompleteParallelTools(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprint("disabled=", disabled), func(t *testing.T) {
			cwd := t.TempDir()
			writePhaseHook(t, cwd, "trim.mjs", `let before=0;
export async function beforeModelRequest(event,api) {
 before++;
 await api.context.setRuntimeInfo({key:"step",text:"current runtime info"});
 if(before===3)await api.agents.steer_agent({runId:event.runId,instruction:"protected extra direction",commandId:"trim-steer"});
}

export async function afterTool(event) {
 return {result:{stdout:"PAYLOAD-"+event.toolCallId+"-"+"x".repeat(18000),stderr:"",exitCode:0,reason:"completed"}};
}`)
			var mu sync.Mutex
			var requests []provider.Request
			var observed []events.Event
			p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, req)
				n := len(requests)
				if n == 5 {
					return agentAnswer("continued successfully"), nil
				}
				reply := hookToolReply(map[string]string{fmt.Sprintf("%da", n): "// Invalid arguments are handled without a subprocess.", fmt.Sprintf("%db", n): "// Another tool."}, fmt.Sprintf("%da", n), fmt.Sprintf("%db", n))
				var m map[string]any
				_ = json.Unmarshal(reply.Message, &m)
				for _, call := range m["tool_calls"].([]any) {
					call.(map[string]any)["function"].(map[string]any)["arguments"] = "not-json"
				}
				m["reasoning_details"] = []any{map[string]any{"type": "reasoning.text", "text": fmt.Sprintf("reasoning-%d", n), "signature": "opaque"}}
				m["nativeResponseItems"] = []any{map[string]any{"opaque": fmt.Sprintf("native-%d", n)}}
				reply.Message, _ = json.Marshal(m)
				return reply, nil
			})
			opts := agentTestOptions(p)
			opts.Observe = func(e events.Event) { mu.Lock(); observed = append(observed, e); mu.Unlock() }
			policy := "{}"
			if disabled {
				policy = "null"
			}
			source := fmt.Sprintf(`import {run_agent} from "noe:agent";
const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:5,contextBudget:{inputTokens:6000},contextPolicy:%s,hooks:["./trim.mjs"]},"original immutable task");
if(r.lastResult.answer!=="continued successfully")throw Error(JSON.stringify(r));`, policy)
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			var out, stderr bytes.Buffer
			code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: opts, Stdout: &out, Stderr: &stderr})
			if code != 0 || err != nil {
				t.Fatalf("code=%d err=%v stdout=%s stderr=%s", code, err, &out, &stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 5 {
				t.Fatalf("requests=%d", len(requests))
			}
			for i, req := range requests {
				body, _ := json.Marshal(req.Messages)
				messages := decodeRuntimeProviderMessages(t, req.Messages)
				statuses := runtimeContextStatusMessages(messages)
				if len(statuses) == 0 || statuses[len(statuses)-1].Role != "user" || !strings.HasPrefix(messages[len(messages)-1].Content, "NOE_CONTEXT_STATUS\n") || !runtimeMessageContains(messages, "user", "original immutable task") {
					t.Fatalf("lost initial instruction/current user status on %d: %+v", i, messages)
				}
				var tail struct{ Content string }
				_ = json.Unmarshal(req.Messages[len(req.Messages)-1], &tail)
				var statedEstimate int
				for _, line := range strings.Split(tail.Content, "\n") {
					if strings.HasPrefix(line, "Next request estimate:") {
						_, _ = fmt.Sscanf(line, "Next request estimate: about %d tokens", &statedEstimate)
					}
				}
				toolsData, _ := json.Marshal(req.Tools)
				actualEstimate := (len(body) + len(toolsData) + 3) / 4
				if statedEstimate != actualEstimate {
					t.Fatalf("request %d notice estimate=%d actual prepared payload estimate=%d", i, statedEstimate, actualEstimate)
				}
				if i >= 2 && bytes.Count(body, []byte("protected extra direction")) != 1 {
					t.Fatalf("steer lost/duplicated on %d", i)
				}
				if !disabled && i >= 2 && bytes.Contains(body, []byte(fmt.Sprintf("PAYLOAD-%da-", i-1))) {
					t.Fatalf("old tool block survived on %d", i)
				}
				if i > 0 {
					latest := i
					var assistant map[string]any
					for _, raw := range req.Messages {
						var item map[string]any
						_ = json.Unmarshal(raw, &item)
						if item["role"] == "assistant" {
							assistant = item
						}
					}
					if assistant == nil || !strings.Contains(fmt.Sprint(assistant["reasoning_details"]), fmt.Sprintf("reasoning-%d", latest)) || !strings.Contains(fmt.Sprint(assistant["nativeResponseItems"]), fmt.Sprintf("native-%d", latest)) {
						t.Fatalf("recent provider fields lost on %d", i)
					}
					toolIDs := []string{}
					for _, raw := range req.Messages {
						var item struct {
							Role, ToolID string
							ID           string `json:"tool_call_id"`
						}
						_ = json.Unmarshal(raw, &item)
						if item.Role == "tool" {
							toolIDs = append(toolIDs, item.ID)
						}
					}
					want := []string{fmt.Sprintf("%da", latest), fmt.Sprintf("%db", latest)}
					if len(toolIDs) < 2 || toolIDs[len(toolIDs)-2] != want[0] || toolIDs[len(toolIDs)-1] != want[1] {
						t.Fatalf("parallel tool order changed %v", toolIDs)
					}
				}
			}
			trims, hooks := 0, 0
			var trimIDs []string
			for _, e := range observed {
				if e["type"] == "context.trimmed" {
					trims++
					trimIDs = append(trimIDs, e["activityId"].(string))
				}
				if e["type"] == "hook.started" && e["phase"] == "beforeModelRequest" {
					hooks++
				}
			}
			if hooks != 5 || (!disabled && trims != 3) || (disabled && trims != 0) {
				t.Fatalf("trims=%d hooks=%d disabled=%v", trims, hooks, disabled)
			}
			for _, id := range trimIDs {
				linked := false
				for _, e := range observed {
					if e["type"] == "model.started" {
						data, _ := json.Marshal(e["causedBy"])
						if bytes.Contains(data, []byte(id)) {
							linked = true
						}
					}
				}
				if !linked {
					t.Fatalf("trim %s missing from request causes", id)
				}
			}
		})
	}
}

func TestContextTrimmingDoesNotTouchOneShotDecisionState(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "padding.mjs", `export async function afterTool(event) {return {result:{...event.result,stdout:"x".repeat(18000)}};}`)
	var mu sync.Mutex
	calls, trims, decisions := 0, 0, 0
	payload := "decision-state-" + strings.Repeat("unchanged", 8000)
	decisionCode := fmt.Sprintf(`// Classify a complete one-shot state.
const {decide}=await import("noe:decision");
const result=await decide({provider:"openrouter",model:"decision/model",state:%q,questions:{q:{type:"choice",criteria:{yes:"Y",no:"N"}}}});
console.log(result.answers.q.choice);`, payload)
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls <= 2 {
			// Keep the synthetic history large enough to exercise trimming before the decision.
			id := fmt.Sprintf("c%d", calls)
			return hookToolReply(map[string]string{id: `// Prepare the synthetic history block.
// @noe: {"outputBytes": 65536}
export default null;`}, id), nil
		}
		if calls == 3 {
			if trims == 0 {
				t.Error("decision did not follow a trim")
			}
			return decisionTool(decisionCode), nil
		}
		return agentAnswer("decision completed"), nil
	})
	opts := agentTestOptions(p)
	opts.Observe = func(e events.Event) {
		if e["type"] == "context.trimmed" {
			mu.Lock()
			trims++
			mu.Unlock()
		}
	}
	opts.Models.DecisionProvider = decisionProviderFunc(func(_ context.Context, req decision.Request) (decision.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		decisions++
		var state string
		if json.Unmarshal(req.State, &state) != nil || state != payload {
			t.Error("decision state was altered")
		}
		var question struct {
			Type     string
			Criteria map[string]string
		}
		if len(req.Questions) != 1 || json.Unmarshal(req.Questions["q"], &question) != nil || question.Type != "choice" || question.Criteria["yes"] != "Y" || question.Criteria["no"] != "N" {
			t.Error("decision questions were altered")
		}
		return decision.Response{JSON: json.RawMessage(`{"answers":{"q":{"type":"choice","choice":"yes"}}}`), StatusCode: 200}, nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: `import {run_agent} from "noe:agent";const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:4,contextBudget:{inputTokens:6000},hooks:["./padding.mjs"]},"use a decision after processing");if(r.lastResult.answer!=="decision completed")throw Error(JSON.stringify(r));`}, Options{Cwd: cwd, Agent: opts, Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 4 || decisions != 1 || trims < 1 {
		t.Fatalf("calls=%d decisions=%d trims=%d", calls, decisions, trims)
	}
}
