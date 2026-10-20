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
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type inheritedContextObservation struct {
	Type        string `json:"type"`
	RunID       string `json:"runId"`
	Name        string `json:"name"`
	Model       string `json:"model"`
	RequestID   string `json:"requestId"`
	ModelStepID string `json:"modelStepId"`
	Context     struct {
		ModelWindow struct {
			Tokens *int64 `json:"tokens"`
		} `json:"modelWindow"`
		ExecutionContext struct {
			Policy                string `json:"policy"`
			BudgetSource          string `json:"budgetSource"`
			EstimatedTokens       int64  `json:"estimatedTokens"`
			BudgetTokens          int64  `json:"budgetTokens"`
			TotalInputBudget      int64  `json:"totalInputBudgetTokens"`
			ResponseReserveTokens *int64 `json:"responseReserveTokens"`
			TrimCount             int    `json:"trimCount"`
		} `json:"executionContext"`
	} `json:"context"`
	RemovedBlocks int   `json:"removedBlocks"`
	BeforeTokens  int64 `json:"beforeTokens"`
	AfterTokens   int64 `json:"afterTokens"`
}

func TestContextPolicyAndModelBudgetInheritAcrossRunJSProcesses(t *testing.T) {
	// Keep the fixture's history allowances stable as built-in API guidance
	// grows. The parent threshold distinguishes its one recent block from the
	// 0.1 default; the child budget waits for two recent blocks so 0.2 retains
	// both while the default 0.1 retains only one.
	encodedInstructions, err := json.Marshal(agent.Instructions())
	if err != nil {
		t.Fatal(err)
	}
	instructionTokens := int64((len(encodedInstructions) + 3) / 4)
	parentWindow, childWindow, grandchildWindow := instructionTokens+7900, instructionTokens+12600, instructionTokens+15300
	for _, test := range []struct {
		name             string
		parentPolicy     string
		parentMode       string
		childPolicy      string
		childMode        string
		grandchildPolicy string
		grandchildMode   string
	}{
		{
			name:         "inherited ratio trims before children start",
			parentPolicy: `{retainRatio:0.2}`, parentMode: "auto",
			childMode: "auto", grandchildPolicy: "null", grandchildMode: "disabled",
		},
		{
			name:         "null inheritance and grandchild override",
			parentPolicy: `null`, parentMode: "disabled",
			childMode: "disabled", grandchildPolicy: `{retainRatio:0.2}`, grandchildMode: "auto",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			cwd := t.TempDir()
			catalogPath := filepath.Join(cwd, "provider-catalog.json")
			catalog := fmt.Sprintf(`{"providers":{"openrouter":{"models":{
  "fixture/parent":{"catalog":{"kind":"agent","contextWindow":%d,"maxOutputTokens":1000}},
  "fixture/child":{"catalog":{"kind":"agent","contextWindow":%d,"maxOutputTokens":1500}},
  "fixture/grandchild":{"catalog":{"kind":"agent","contextWindow":%d,"maxOutputTokens":2000}}
}}}}`, parentWindow, childWindow, grandchildWindow)
			if err := os.WriteFile(catalogPath, []byte(catalog), 0600); err != nil {
				t.Fatal(err)
			}
			writePhaseHook(t, cwd, "pad-root.mjs", `export async function afterTool(event) {
  if (event.toolCallId === "parent-old") {
    return {result:{...event.result,stdout:"PARENT_OLD_BLOCK:"+"p".repeat(20000)}};
  }
  return {result:{...event.result,stdout:"PARENT_RECENT_"+event.toolCallId+":"+"p".repeat(1900)}};
}`)

			var mu sync.Mutex
			callsByModel := map[string]int{}
			var requests []provider.Request
			p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
				mu.Lock()
				requests = append(requests, req)
				callsByModel[req.Model]++
				n := callsByModel[req.Model]
				mu.Unlock()

				switch req.Model {
				case "fixture/parent":
					switch n {
					case 1:
						return hookToolReply(map[string]string{"parent-old": `// Emit the oldest large parent block.
console.log("PARENT_OLD_TOOL_BLOCK");`}, "parent-old"), nil
					case 2, 3:
						id := fmt.Sprintf("parent-%d", n)
						code := fmt.Sprintf(`// Emit a small recent completed block %s.
console.log("PARENT_RECENT_%s:"+"p".repeat(1900));`, id, id)
						return hookToolReply(map[string]string{id: code}, id), nil
					case 4:
						code := `// Start a child only after the parent has prepared its trimmed context.
const child=await run_agent({name:"child-run",model:"fixture/child",hooks:[],maxSteps:5` + contextPolicyField(test.childPolicy) + `},"child task",{allowDelegation:true});
if(child.lastResult?.answer!=="child completed")throw Error(JSON.stringify(child));
console.log("PARENT_CHILD:child completed");`
						return hookToolReply(map[string]string{"parent-child": code}, "parent-child"), nil
					default:
						return agentAnswer("parent completed"), nil
					}
				case "fixture/child":
					switch n {
					case 1:
						return hookToolReply(map[string]string{"child-old": `// Emit the oldest large child block.
console.log("CHILD_OLD_BLOCK:"+"c".repeat(32000));`}, "child-old"), nil
					case 2, 3:
						id := fmt.Sprintf("child-%d", n)
						code := fmt.Sprintf(`// Emit a recent completed block that distinguishes the inherited retain ratio.
console.log("CHILD_RECENT_%s:"+"c".repeat(1900));`, id)
						return hookToolReply(map[string]string{id: code}, id), nil
					case 4:
						code := `// Start a grandchild after the child has prepared its context.
const grandchild=await run_agent({name:"grandchild-run",model:"fixture/grandchild",hooks:[],maxSteps:1` + contextPolicyField(test.grandchildPolicy) + `},"grandchild task");
if(grandchild.lastResult?.answer!=="grandchild completed")throw Error(JSON.stringify(grandchild));
console.log("CHILD_GRANDCHILD:grandchild completed");`
						return hookToolReply(map[string]string{"child-grandchild": code}, "child-grandchild"), nil
					default:
						return agentAnswer("child completed"), nil
					}
				case "fixture/grandchild":
					return agentAnswer("grandchild completed"), nil
				default:
					return provider.Response{}, fmt.Errorf("unexpected model %q", req.Model)
				}
			})

			var eventMu sync.Mutex
			var observations []inheritedContextObservation
			opts := agentTestOptions(p)
			opts.ProviderOptions = provider.Options{ConfigFile: catalogPath}
			opts.Observe = func(event events.Event) {
				encoded, err := json.Marshal(event)
				if err != nil {
					t.Errorf("encode event: %v", err)
					return
				}
				var observation inheritedContextObservation
				if err := json.Unmarshal(encoded, &observation); err != nil {
					t.Errorf("decode event: %v", err)
					return
				}
				if observation.Type == "context.updated" || observation.Type == "context.trimmed" || observation.Type == "model.started" {
					eventMu.Lock()
					observations = append(observations, observation)
					eventMu.Unlock()
				}
			}
			source := `import {run_agent} from "noe:agent";
const parent=await run_agent({name:"parent-run",provider:"openrouter",model:"fixture/parent",effort:"auto",maxSteps:5,contextPolicy:` + test.parentPolicy + `,hooks:["./pad-root.mjs"]},"parent task");
if(parent.lastResult?.answer!=="parent completed")throw Error(JSON.stringify(parent));
console.log("cross-process context policy verified");`
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			var stdout, stderr bytes.Buffer
			code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
				Cwd: cwd, Agent: opts, Stdout: &stdout, Stderr: &stderr,
			})
			if code != 0 || err != nil || !strings.Contains(stdout.String(), "cross-process context policy verified") {
				t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, stdout.String(), stderr.String())
			}

			mu.Lock()
			requestCopy := append([]provider.Request(nil), requests...)
			modelCalls := map[string]int{}
			for model, count := range callsByModel {
				modelCalls[model] = count
			}
			mu.Unlock()
			wantCalls := map[string]int{"fixture/parent": 5, "fixture/child": 5, "fixture/grandchild": 1}
			for model, want := range wantCalls {
				if modelCalls[model] != want {
					t.Fatalf("model %s requests=%d, want %d", model, modelCalls[model], want)
				}
			}
			if len(requestCopy) != 11 {
				t.Fatalf("provider requests=%d, want 11", len(requestCopy))
			}

			eventMu.Lock()
			observationCopy := append([]inheritedContextObservation(nil), observations...)
			eventMu.Unlock()
			byModel := map[string][]inheritedContextObservation{}
			trimEvents := map[string][]inheritedContextObservation{}
			modelStarts := map[string][]inheritedContextObservation{}
			for _, observation := range observationCopy {
				switch observation.Type {
				case "context.updated":
					byModel[observation.Model] = append(byModel[observation.Model], observation)
				case "context.trimmed":
					byModel[observation.Model] = append(byModel[observation.Model], observation)
					trimEvents[observation.Model] = append(trimEvents[observation.Model], observation)
				case "model.started":
					modelStarts[observation.Model] = append(modelStarts[observation.Model], observation)
				}
			}
			for model, want := range map[string]struct {
				window  int64
				reserve int64
			}{
				"fixture/parent":     {window: parentWindow, reserve: 1000},
				"fixture/child":      {window: childWindow, reserve: 1500},
				"fixture/grandchild": {window: grandchildWindow, reserve: 2000},
			} {
				if len(byModel[model]) == 0 {
					t.Fatalf("no context snapshots for %s", model)
				}
				wantPolicy := test.parentMode
				if model == "fixture/child" {
					wantPolicy = test.childMode
				} else if model == "fixture/grandchild" {
					wantPolicy = test.grandchildMode
				}
				for _, observation := range byModel[model] {
					if observation.Context.ExecutionContext.Policy != wantPolicy {
						t.Fatalf("%s policy=%q, want %q: %+v", model, observation.Context.ExecutionContext.Policy, wantPolicy, observation)
					}
					maxTrims := 0
					if test.parentMode == "auto" && model != "fixture/grandchild" {
						maxTrims = 1
					}
					if observation.Context.ExecutionContext.TrimCount > maxTrims || observation.Context.ExecutionContext.BudgetSource != "model_window" || observation.Context.ModelWindow.Tokens == nil || *observation.Context.ModelWindow.Tokens != want.window || observation.Context.ExecutionContext.ResponseReserveTokens == nil || *observation.Context.ExecutionContext.ResponseReserveTokens != want.reserve || observation.Context.ExecutionContext.TotalInputBudget <= 0 || observation.Context.ExecutionContext.TotalInputBudget >= want.window {
						t.Fatalf("%s did not recompute its own model budget: %+v", model, observation)
					}
				}
			}

			if test.parentMode == "auto" {
				trimOrdinals := map[string]int{}
				for _, model := range []string{"fixture/parent", "fixture/child"} {
					if len(trimEvents[model]) != 1 || trimEvents[model][0].RemovedBlocks != 1 || trimEvents[model][0].Context.ExecutionContext.TrimCount != 1 {
						t.Fatalf("%s should use inherited retainRatio 0.2 and trim only the oldest block: %+v", model, trimEvents[model])
					}
					trim := trimEvents[model][0]
					trimOrdinal := 0
					for index, started := range modelStarts[model] {
						if started.ModelStepID == trim.ModelStepID {
							trimOrdinal = index + 1
							break
						}
					}
					if trimOrdinal == 0 {
						t.Fatalf("%s trim has no matching model.started request: %+v starts=%+v", model, trim, modelStarts[model])
					}
					trimOrdinals[model] = trimOrdinal
					t.Logf("%s trim applied at provider request %d/%d, modelStepId=%s, budgetTokens=%d, estimatedTokens=%d, before=%d, after=%d", model, trimOrdinal, len(modelStarts[model]), trim.ModelStepID, trim.Context.ExecutionContext.BudgetTokens, trim.Context.ExecutionContext.EstimatedTokens, trim.BeforeTokens, trim.AfterTokens)
					if trim.BeforeTokens <= 0 || trim.AfterTokens*5 > trim.BeforeTokens || trim.AfterTokens*10 <= trim.BeforeTokens {
						t.Fatalf("%s retained history does not distinguish the inherited 0.2 ratio from the 0.1 default: before=%d after=%d", model, trim.BeforeTokens, trim.AfterTokens)
					}
				}
				if len(trimEvents["fixture/grandchild"]) != 0 {
					t.Fatalf("grandchild unexpectedly trimmed: %+v", trimEvents["fixture/grandchild"])
				}
				assertInheritedRetainedBlocks(t, requestCopy, "fixture/parent", true, trimOrdinals["fixture/parent"], "PARENT_RECENT_", "parent-2")
				assertInheritedRetainedBlocks(t, requestCopy, "fixture/child", true, trimOrdinals["fixture/child"], "CHILD_RECENT_", "child-2", "child-3")
			} else {
				if len(trimEvents) != 0 {
					t.Fatalf("explicit null inheritance should disable parent/child/grandchild trimming: %+v", trimEvents)
				}
				for _, observation := range observationCopy {
					if observation.Type == "context.updated" && observation.Context.ExecutionContext.TrimCount != 0 {
						t.Fatalf("disabled run has a nonzero trim count: %+v", observation)
					}
				}
				assertInheritedRetainedBlocks(t, requestCopy, "fixture/parent", false, 0, "PARENT_", "parent-old", "parent-2", "parent-3", "parent-child")
				assertInheritedRetainedBlocks(t, requestCopy, "fixture/child", false, 0, "CHILD_", "child-old", "child-2", "child-3", "child-grandchild")
			}
			assertFreshNestedRunCounters(t, observationCopy, test.parentMode == "auto")
		})
	}
}

func contextPolicyField(policy string) string {
	if policy == "" {
		return ""
	}
	return ",contextPolicy:" + policy
}

func assertFreshNestedRunCounters(t *testing.T, observations []inheritedContextObservation, parentTrims bool) {
	t.Helper()
	firstByModel := map[string]inheritedContextObservation{}
	trimIndexByModel := map[string]int{}
	firstIndexByModel := map[string]int{}
	for i, observation := range observations {
		if observation.Type == "context.trimmed" && trimIndexByModel[observation.Model] == 0 {
			trimIndexByModel[observation.Model] = i + 1
		}
		if observation.Type == "context.updated" {
			if _, exists := firstByModel[observation.Model]; !exists {
				firstByModel[observation.Model] = observation
				firstIndexByModel[observation.Model] = i + 1
			}
		}
	}
	for model, observation := range firstByModel {
		if observation.RunID == "" {
			t.Fatalf("new %s run has no ID: %+v", model, observation)
		}
		if observation.Context.ExecutionContext.TrimCount != 0 {
			t.Fatalf("new %s run inherited prior trim count: %+v", model, observation)
		}
	}
	if parentTrims {
		if trimIndexByModel["fixture/parent"] == 0 || trimIndexByModel["fixture/child"] == 0 {
			t.Fatalf("expected both parent and child to trim: parent=%d child=%d", trimIndexByModel["fixture/parent"], trimIndexByModel["fixture/child"])
		}
		if trimIndexByModel["fixture/parent"] >= firstIndexByModel["fixture/child"] {
			t.Fatal("child did not start after its parent's context trim")
		}
		if trimIndexByModel["fixture/child"] >= firstIndexByModel["fixture/grandchild"] {
			t.Fatal("grandchild did not start after its parent's context trim")
		}
		if firstByModel["fixture/parent"].RunID == firstByModel["fixture/child"].RunID || firstByModel["fixture/child"].RunID == firstByModel["fixture/grandchild"].RunID {
			t.Fatal("nested runs shared run IDs")
		}
	}
}

func assertInheritedRetainedBlocks(t *testing.T, requests []provider.Request, model string, trimmed bool, requestOrdinal int, marker string, wantIDs ...string) {
	t.Helper()
	var request *provider.Request
	requestIndex := -1
	modelOrdinal := 0
	for i := range requests {
		if requests[i].Model != model {
			continue
		}
		modelOrdinal++
		body, _ := json.Marshal(requests[i].Messages)
		if requestOrdinal > 0 && modelOrdinal == requestOrdinal {
			request = &requests[i]
			requestIndex = i
			if trimmed && !bytes.Contains(body, []byte("Context trimmed:")) {
				t.Fatalf("%s request %d does not contain its trim notice", model, requestIndex)
			}
			break
		}
		if requestOrdinal == 0 && (!trimmed || bytes.Contains(body, []byte("Context trimmed:"))) {
			request = &requests[i]
			requestIndex = i
		}
	}
	if request == nil {
		qualifier := ""
		if trimmed {
			qualifier = " after a trim"
		}
		t.Fatalf("%s has no request%s", model, qualifier)
	}
	callIDs := []string{}
	toolResults := map[string]string{}
	for _, raw := range request.Messages {
		var message struct {
			Role       string          `json:"role"`
			ToolCallID string          `json:"tool_call_id"`
			Content    json.RawMessage `json:"content"`
			ToolCalls  []struct {
				ID string `json:"id"`
			} `json:"tool_calls"`
		}
		if err := json.Unmarshal(raw, &message); err != nil {
			t.Fatalf("decode %s history: %v", model, err)
		}
		for _, call := range message.ToolCalls {
			callIDs = append(callIDs, call.ID)
		}
		if message.Role == "tool" {
			var content string
			if err := json.Unmarshal(message.Content, &content); err != nil {
				t.Fatalf("decode %s result %s: %v", model, message.ToolCallID, err)
			}
			toolResults[message.ToolCallID] = content
		}
	}
	if len(callIDs) != len(wantIDs) {
		t.Fatalf("%s request %d retained calls=%v, want %v", model, requestIndex, callIDs, wantIDs)
	}
	for i, id := range wantIDs {
		if callIDs[i] != id {
			t.Fatalf("%s retained call order=%v, want %v", model, callIDs, wantIDs)
		}
		if _, ok := toolResults[id]; !ok {
			t.Fatalf("%s retained %s without its complete tool result", model, id)
		}
		if !strings.Contains(toolResults[id], marker) {
			t.Fatalf("%s retained %s with an unexpected result: %s", model, id, toolResults[id])
		}
	}
}
