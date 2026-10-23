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
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

func writeRuntimeHookFixture(t *testing.T, cwd string) string {
	t.Helper()
	dir := filepath.Join(cwd, "hooks")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dependency.mjs"), []byte(`export const label = "dependency";`), 0600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(cwd, "module-loads.txt")
	source := fmt.Sprintf(`import {label} from "./dependency.mjs";
import {readFileSync,writeFileSync,existsSync} from "node:fs";
let loads = 0;
const path = %q;
writeFileSync(path, (existsSync(path) ? readFileSync(path,"utf8") : "") + label + ":" + (++loads) + "\n");
export const loaded = loads;
`, log)
	if err := os.WriteFile(filepath.Join(dir, "entry.mjs"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	return log
}

func TestHookModuleMapDependenciesAndFreshQueueRuntime(t *testing.T) {
	cwd := t.TempDir()
	log := writeRuntimeHookFixture(t, cwd)
	snapshot, err := modulemap.Parse([]byte(`{"imports":{"project-hooks":"./hooks/entry.mjs"}}`), cwd)
	if err != nil {
		t.Fatal(err)
	}
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		return agentAnswer(interruptUserPrompt(req)), nil
	})
	source := `import {spawn_agent,queue_agent,observe_agent,stop_agent} from "noe:agent";
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["project-hooks"]},undefined,{keepAlive:true});
await queue_agent({agentId,instruction:"first",commandId:"first"});
await queue_agent({agentId,instruction:"second",commandId:"second"});
while (true) {
  const current=await observe_agent({agentId});
  if (current.state==="failed" || current.state==="stopped") throw Error(JSON.stringify(current));
  if (current.state==="idle" && current.lastResult?.commandId==="second") {
    if (current.lastResult.state!=="completed") throw Error(JSON.stringify(current));
    console.log(current.lastResult.answer);
    break;
  }
  await new Promise(resolve=>setTimeout(resolve,5));
}
await stop_agent({agentId});`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
		Cwd: cwd, ImportMap: snapshot, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr,
	})
	if code != 0 || err != nil || out.String() != "second\n" {
		t.Fatalf("code=%d error=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	loads, err := os.ReadFile(log)
	if err != nil || string(loads) != "dependency:1\ndependency:1\n" {
		t.Fatalf("hook/dependency load states=%q error=%v", loads, err)
	}
}

func TestHookModuleLoadFailureReportsTaskWithoutCallingProvider(t *testing.T) {
	for _, name := range []string{"missing.mjs", "broken.mjs"} {
		t.Run(name, func(t *testing.T) {
			cwd := t.TempDir()
			if name == "broken.mjs" {
				if err := os.WriteFile(filepath.Join(cwd, name), []byte(`export const = ;`), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var calls atomic.Int32
			p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
				calls.Add(1)
				return agentAnswer("unexpected"), nil
			})
			source := fmt.Sprintf(`import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:[%q]},"unused task")));`, "./"+name)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			var out, stderr bytes.Buffer
			code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
				Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr,
			})
			var result struct {
				Reason     string `json:"reason"`
				LastResult struct {
					State string `json:"state"`
					Error string `json:"error"`
				} `json:"lastResult"`
			}
			decode := json.Unmarshal(out.Bytes(), &result)
			if code != 0 || err != nil || decode != nil || result.Reason != "task_failed" || result.LastResult.State != "failed" || !strings.Contains(result.LastResult.Error, name) || calls.Load() != 0 {
				t.Fatalf("code=%d error=%v decode=%v result=%+v calls=%d stderr=%q stdout=%q", code, err, decode, result, calls.Load(), &stderr, &out)
			}
		})
	}
}

func TestHookModuleChildInheritanceAndExplicitClear(t *testing.T) {
	cwd := t.TempDir()
	log := writeRuntimeHookFixture(t, cwd)
	var mu sync.Mutex
	seen := make(map[string]int)
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		input := interruptUserPrompt(req)
		if input == "" {
			return provider.Response{}, fmt.Errorf("provider request lost task instruction: %+v", req.Messages)
		}
		mu.Lock()
		seen[req.RunID]++
		call := seen[req.RunID]
		mu.Unlock()
		if input == "parent" && call == 1 {
			arguments, _ := json.Marshal(map[string]string{"code": `// Check inherited and explicitly cleared hooks.
import {run_agent} from "noe:agent";
const inherited=await run_agent({name:"inherited"},"inherited");
const cleared=await run_agent({name:"cleared",hooks:[]},"cleared");
if(inherited.lastResult?.state!=="completed" || cleared.lastResult?.state!=="completed") throw Error("child failed");
console.log("children completed");`})
			message, _ := json.Marshal(map[string]any{"role": "assistant", "content": "", "tool_calls": []any{
				map[string]any{"id": "inherit-call", "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(arguments)}},
			}})
			return provider.Response{Message: message, FinishReason: "tool_calls"}, nil
		}
		return agentAnswer(input + " answer"), nil
	})
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./hooks/entry.mjs"]},"parent");
if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
		Cwd: cwd, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr,
	})
	if code != 0 || err != nil || out.String() != "parent answer\n" {
		t.Fatalf("code=%d error=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	loads, err := os.ReadFile(log)
	if err != nil || string(loads) != "dependency:1\ndependency:1\n" {
		t.Fatalf("parent+inherited child loads=%q error=%v", loads, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 {
		t.Fatalf("expected parent and both children to reach provider, run calls=%v", seen)
	}
}

func TestHookScopedAPIDefaultsAndAcceptedChildLifetime(t *testing.T) {
	cwd := t.TempDir()
	marker := filepath.Join(cwd, "hook-child.json")
	hook := fmt.Sprintf(`import {withHookAPI} from "noe:agent/hooks";
import {observe_agent} from "noe:agent";
import {writeFileSync} from "node:fs";
let retained, childId, groupId;
await withHookAPI(async api => {
  groupId=(await api.execution.get()).groupId;
  childId=(await api.agents.spawn_agent({name:"scoped-child",hooks:[]},undefined,{keepAlive:true})).agentId;
  const snapshot=await api.agents.observe_agent({agentId:childId});
  if(snapshot.groupId!==groupId) throw Error("hook child belongs to another group");
  retained=api.agents.observe_agent;
});
let rejected=false;
try { await retained({agentId:childId}); } catch { rejected=true; }
if(!rejected) throw Error("retained hook API survived callback");
const snapshot=await observe_agent({agentId:childId});
if(snapshot.state!=="idle" && snapshot.state!=="starting") throw Error("scope close stopped accepted child: "+JSON.stringify(snapshot));
writeFileSync(%q,JSON.stringify({childId,groupId}));`, marker)
	if err := os.WriteFile(filepath.Join(cwd, "scope.mjs"), []byte(hook), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		if _, err := os.Stat(marker); err != nil {
			return provider.Response{}, fmt.Errorf("hook callback did not complete before provider: %w", err)
		}
		return agentAnswer("scoped task done"), nil
	})
	var eventMu sync.Mutex
	var observed []events.Event
	settings := agentTestOptions(p)
	settings.Observe = func(event events.Event) {
		eventMu.Lock()
		observed = append(observed, event)
		eventMu.Unlock()
	}
	source := `import {run_agent} from "noe:agent";
console.log(JSON.stringify(await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./scope.mjs"]},"scope lifetime")));`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
		Cwd: cwd, Agent: settings, Stdout: &out, Stderr: &stderr,
	})
	var result struct {
		AgentID    string `json:"agentId"`
		LastResult struct {
			State  string `json:"state"`
			Answer string `json:"answer"`
		} `json:"lastResult"`
	}
	decode := json.Unmarshal(out.Bytes(), &result)
	if code != 0 || err != nil || decode != nil || result.LastResult.State != "completed" || result.LastResult.Answer != "scoped task done" || calls.Load() != 1 {
		t.Fatalf("code=%d error=%v decode=%v result=%+v calls=%d stderr=%q stdout=%q", code, err, decode, result, calls.Load(), &stderr, &out)
	}
	var saved struct {
		ChildID string `json:"childId"`
		GroupID string `json:"groupId"`
	}
	data, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &saved); err != nil || saved.GroupID != result.AgentID || saved.ChildID == "" {
		t.Fatalf("invalid hook group/child: %+v error=%v", saved, err)
	}
	eventMu.Lock()
	defer eventMu.Unlock()
	stopped := 0
	for _, event := range observed {
		if event["agentId"] == saved.ChildID && event["type"] == "instance.stopped" {
			stopped++
			if event["groupId"] != result.AgentID {
				t.Fatalf("child terminal event changed group: %+v", event)
			}
		}
	}
	if stopped != 1 {
		t.Fatalf("accepted hook child was not reaped exactly once by task end: %d", stopped)
	}
}
