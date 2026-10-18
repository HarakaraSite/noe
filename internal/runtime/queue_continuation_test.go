package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
)

func queueContainsUser(messages []json.RawMessage, text string) bool {
	for _, raw := range messages {
		var item struct{ Role, Content string }
		_ = json.Unmarshal(raw, &item)
		if item.Role == "user" && item.Content == text {
			return true
		}
	}
	return false
}

func TestQueueContinuesWholeConversationAndKeepsOriginalSystem(t *testing.T) {
	cwd := t.TempDir()
	instructionsDir := filepath.Join(cwd, ".noe", "instructions")
	if err := os.MkdirAll(instructionsDir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(instructionsDir, "rules.md")
	if err := os.WriteFile(path, []byte("ORIGINAL_QUEUE_RULE"), 0600); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var requests []provider.Request
	calls := map[string]int{}
	firstFinal := json.RawMessage(`{"role":"assistant","content":"first final proof","reasoning_details":[{"type":"reasoning.text","text":"keep final reasoning","signature":"opaque-final-signature"}],"_noe_response_items":[{"type":"reasoning","encrypted_content":"opaque-final-native"}]}`)
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		defer mu.Unlock()
		requests = append(requests, req)
		calls[req.RunID]++
		switch {
		case queueContainsUser(req.Messages, "independent task"):
			return agentAnswer("independent done"), nil
		case queueContainsUser(req.Messages, "third queued instruction"):
			return agentAnswer("third done"), nil
		case queueContainsUser(req.Messages, "second queued instruction"):
			return agentAnswer("second done"), nil
		case calls[req.RunID] == 1:
			return hookToolReply(map[string]string{"first-tool": `// Produce evidence for the continued conversation.
console.log("tool-result-proof");`}, "first-tool"), nil
		default:
			if err := os.WriteFile(path, []byte("NEW_INSTANCE_QUEUE_RULE"), 0600); err != nil {
				t.Error(err)
			}
			return provider.Response{Message: firstFinal, FinishReason: "stop"}, nil
		}
	})
	source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent,run_agent} from "noe:agent";
const config={provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2,system:"EXPLICIT_QUEUE_RULE"};
const {agentId}=await spawn_agent(config,"initial queue instruction",{keepAlive:true});
await queue_agent({agentId,instruction:"second queued instruction",commandId:"second"});
async function until(commandId) {
 while(true){const s=await observe_agent({agentId});if(s.state==="failed"||s.state==="stopped")throw Error(JSON.stringify(s));if(s.state==="idle"&&s.lastResult?.commandId===commandId){if(s.lastResult.state!=="completed")throw Error(JSON.stringify(s));return s;}await new Promise(r=>setTimeout(r,5));}
}
await until("second");
await queue_agent({agentId,instruction:"third queued instruction",commandId:"third"});
await until("third");
await interrupt_agent({agentId});
const independent=await run_agent(config,"independent task");
if(independent.lastResult?.answer!=="independent done")throw Error(JSON.stringify(independent));
console.log("continuation verified");`
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil || out.String() != "continuation verified\n" {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 5 {
		t.Fatalf("requests=%d", len(requests))
	}
	for i, req := range requests {
		var system struct{ Content string }
		_ = json.Unmarshal(req.Messages[0], &system)
		if !strings.Contains(system.Content, "EXPLICIT_QUEUE_RULE") {
			t.Fatalf("explicit system lost on %d", i)
		}
		if i < 4 && (!strings.Contains(system.Content, "ORIGINAL_QUEUE_RULE") || strings.Contains(system.Content, "NEW_INSTANCE_QUEUE_RULE")) {
			t.Fatalf("continued conversation system changed on %d", i)
		}
		if i == 4 && (!strings.Contains(system.Content, "NEW_INSTANCE_QUEUE_RULE") || queueContainsUser(req.Messages, "initial queue instruction")) {
			t.Fatal("new instance inherited old conversation/system")
		}
		if i < 2 && queueContainsUser(req.Messages, "second queued instruction") {
			t.Fatal("pending queue instruction leaked into active task")
		}
		if i == 2 || i == 3 {
			body, _ := json.Marshal(req.Messages)
			for _, proof := range []string{"initial queue instruction", "first-tool", "tool-result-proof", "first final proof", "keep final reasoning", "opaque-final-signature", "opaque-final-native", "second queued instruction"} {
				if !bytes.Contains(body, []byte(proof)) {
					t.Fatalf("request %d lost %q", i, proof)
				}
			}
			if bytes.Count(body, []byte("first final proof")) != 1 || bytes.Count(body, []byte("second queued instruction")) != 1 {
				t.Fatalf("duplicate retained messages on %d", i)
			}
			var roles []string
			for _, raw := range req.Messages {
				var m struct{ Role string }
				_ = json.Unmarshal(raw, &m)
				roles = append(roles, m.Role)
			}
			expected := "system,user,assistant,tool,assistant,user,system"
			if i == 3 {
				expected = "system,user,assistant,tool,assistant,user,assistant,user,system"
			}
			if strings.Join(roles, ",") != expected {
				t.Fatalf("chronology on %d: %v", i, roles)
			}
		}
	}
	if requests[0].RunID != requests[1].RunID || requests[1].RunID == requests[2].RunID || requests[2].RunID == requests[3].RunID {
		t.Fatal("queue changed run identity contract")
	}
	if requests[0].CacheKey != requests[1].CacheKey || requests[1].CacheKey == requests[2].CacheKey {
		t.Fatal("queue changed existing cache-key contract")
	}
}

func TestQueueAppliedSteersStayProtectedAndCommandIDsAreRunLocal(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "steer.mjs", `export async function beforeModelRequest(event,api) {
 await api.agents.steer_agent({runId:event.runId,commandId:"same-command-id",instruction:"protected steer for "+event.runId});
}`)
	var mu sync.Mutex
	var requests []provider.Request
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		mu.Lock()
		requests = append(requests, req)
		mu.Unlock()
		return agentAnswer("finished " + req.RunID), nil
	})
	source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:1,hooks:["./steer.mjs"]},"first steer task",{keepAlive:true});
await queue_agent({agentId,instruction:"next steer task",commandId:"next"});
while(true){const s=await observe_agent({agentId});if(s.state==="failed"||s.state==="stopped")throw Error(JSON.stringify(s));if(s.state==="idle"&&s.lastResult?.commandId==="next"){if(s.lastResult.state!=="completed")throw Error(JSON.stringify(s));break;}await new Promise(r=>setTimeout(r,5));}
await interrupt_agent({agentId});`
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 2 {
		t.Fatalf("requests=%d", len(requests))
	}
	body, _ := json.Marshal(requests[1].Messages)
	for _, proof := range []string{"first steer task", "next steer task", "protected steer for " + requests[0].RunID, "protected steer for " + requests[1].RunID} {
		if bytes.Count(body, []byte(proof)) != 1 {
			t.Fatalf("lost/duplicated %q in continued request", proof)
		}
	}
}

func TestQueueRecoversCommittedHistoryAfterFailureOrToolInterrupt(t *testing.T) {
	for _, mode := range []string{"provider failure", "tool interrupt"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			started := filepath.Join(cwd, "incomplete-tool-started")
			var mu sync.Mutex
			var requests []provider.Request
			p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
				mu.Lock()
				defer mu.Unlock()
				requests = append(requests, req)
				if queueContainsUser(req.Messages, "resume after interruption or failure") {
					return agentAnswer("recovered"), nil
				}
				if len(requests) == 1 {
					return hookToolReply(map[string]string{"completed-tool": `// Record a completed exchange.
console.log("committed result proof");`}, "completed-tool"), nil
				}
				if mode == "provider failure" {
					return provider.Response{}, errors.New("fixture provider failure")
				}
				return hookToolReply(map[string]string{"incomplete-tool": fmt.Sprintf(`// Wait for interruption.
import {writeFileSync} from "node:fs";
writeFileSync(%q,"ready");
await new Promise(resolve=>setTimeout(resolve,30000));`, started)}, "incomplete-tool"), nil
			})
			interrupt := ""
			if mode == "tool interrupt" {
				interrupt = fmt.Sprintf(`while(!existsSync(%q))await new Promise(r=>setTimeout(r,2));
const active=await observe_agent({agentId});
const ack=await interrupt_agent({runId:active.currentRunId});
if(ack.status!=="accepted")throw Error(JSON.stringify(ack));`, started)
			}
			source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
import {existsSync} from "node:fs";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2},"original unfinished task",{keepAlive:true});
await queue_agent({agentId,instruction:"resume after interruption or failure",commandId:"resume"});
` + interrupt + `
while(true){const s=await observe_agent({agentId});if(s.state==="failed"||s.state==="stopped")throw Error(JSON.stringify(s));if(s.state==="idle"&&s.lastResult?.commandId==="resume"){if(s.lastResult.answer!=="recovered")throw Error(JSON.stringify(s));break;}await new Promise(r=>setTimeout(r,2));}
await interrupt_agent({agentId});`
			ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
			defer cancel()
			var out, stderr bytes.Buffer
			code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
			if code != 0 || err != nil {
				t.Fatalf("code=%d err=%v out=%s stderr=%s", code, err, &out, &stderr)
			}
			mu.Lock()
			defer mu.Unlock()
			if len(requests) != 3 {
				t.Fatalf("requests=%d", len(requests))
			}
			body, _ := json.Marshal(requests[2].Messages)
			for _, proof := range []string{"original unfinished task", "completed-tool", "committed result proof", "resume after interruption or failure"} {
				if !bytes.Contains(body, []byte(proof)) {
					t.Fatalf("continued conversation lost %q", proof)
				}
			}
			if bytes.Contains(body, []byte("incomplete-tool")) {
				t.Fatal("incomplete tool exchange survived interruption")
			}
			if requests[0].RunID != requests[1].RunID || requests[1].RunID == requests[2].RunID {
				t.Fatal("recovery did not start a separate run")
			}
		})
	}
}
