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

func writeAgentInstructionFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgentBehaviorInstructionsCanBeReplacedAndRemoved(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	path := filepath.Join(cwd, ".noe", "instructions", "behavior.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	behaviors := map[string]string{
		"without":  "",
		"custom":   "USER_BEHAVIOR: ask questions and explain in Japanese.",
		"replaced": "USER_REPLACEMENT: proceed autonomously and use terse English.",
		"removed":  "",
	}
	var mu sync.Mutex
	calls := map[string]int{}
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		input := interruptUserPrompt(req)
		behavior, exists := behaviors[input]
		if !exists {
			return provider.Response{}, fmt.Errorf("unexpected instruction %q", input)
		}
		want := agent.Facts() + "\n\n" + agent.URLImportFacts("enabled")
		if behavior != "" {
			want += "\n\n" + behavior
		}
		want += "\n\nUSER_SYSTEM"
		var system string
		var toolResult bool
		for _, raw := range req.Messages {
			var message runtimeProviderMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			if message.Role == "system" {
				system = message.Content
			}
			if message.Role == "tool" && strings.Contains(message.Content, `"exitCode":0`) && strings.Contains(message.Content, `"value":7`) {
				toolResult = true
			}
		}
		if system != want {
			return provider.Response{}, fmt.Errorf("%s includes an unexpected policy or lost external instructions:\ngot: %q\nwant: %q", input, system, want)
		}
		mu.Lock()
		calls[input]++
		n := calls[input]
		mu.Unlock()
		if input == "without" {
			if n == 1 {
				return hookToolReply(map[string]string{"plain": `export default {value:7};`}, "plain"), nil
			}
			if !toolResult {
				return provider.Response{}, fmt.Errorf("run_js without an operation comment did not return its data")
			}
		}
		return agentAnswer(input + " done"), nil
	})
	source := fmt.Sprintf(`import {run_agent} from "noe:agent";
import {writeFileSync,unlinkSync} from "node:fs";
const config={provider:"openrouter",model:"fixture/model",effort:"auto",system:"USER_SYSTEM"};
async function run(instruction) {
 const result=await run_agent(config,instruction);
 if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result));
 console.log(result.lastResult.answer);
}
await run("without");
writeFileSync(%q,%q);
await run("custom");
writeFileSync(%q,%q);
await run("replaced");
unlinkSync(%q);
await run("removed");`, path, behaviors["custom"], path, behaviors["replaced"], path)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
		Cwd: cwd, Agent: agentTestOptions(p), Stdout: &stdout, Stderr: &stderr,
	})
	if want := "without done\ncustom done\nreplaced done\nremoved done\n"; err != nil || code != 0 || stdout.String() != want {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, &stdout, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	for input := range behaviors {
		want := 1
		if input == "without" {
			want = 2
		}
		if calls[input] != want {
			t.Errorf("%s model requests=%d, want %d", input, calls[input], want)
		}
	}
}

func TestAgentInstructionsFreezeParentAndReloadChild(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	common := filepath.Join(cwd, "selected-a", "10-common.md")
	workspace := filepath.Join(cwd, "selected-b", "20-project.md")
	writeAgentInstructionFixture(t, filepath.Join(config, "noe", "instructions", "default.md"), "EXCLUDED_DEFAULT")
	writeAgentInstructionFixture(t, filepath.Join(cwd, ".noe", "instructions", "default.md"), "EXCLUDED_DEFAULT")
	writeAgentInstructionFixture(t, common, "GLOBAL_VERSION_ZERO")
	writeAgentInstructionFixture(t, workspace, "WORKSPACE_VERSION_ZERO")
	writeAgentInstructionFixture(t, filepath.Join(cwd, "first.md"), "EXPLICIT_FIRST")
	writeAgentInstructionFixture(t, filepath.Join(cwd, "second.md"), "EXPLICIT_SECOND")
	var mu sync.Mutex
	seen := map[string][]string{}
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		var system string
		for _, raw := range req.Messages {
			var message runtimeProviderMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			if message.Role == "system" {
				system = message.Content
				break
			}
		}
		input := interruptUserPrompt(req)
		if system == "" || input == "" {
			return provider.Response{}, fmt.Errorf("provider request lost task/system message: %+v", req.Messages)
		}
		mu.Lock()
		seen[input] = append(seen[input], system)
		n := len(seen[input])
		mu.Unlock()
		if input == "parent" && n == 1 {
			code := fmt.Sprintf(`// Change instruction files and start a child with its own snapshot.
import {run_agent} from "noe:agent";
import {writeFileSync} from "node:fs";
writeFileSync(%q,"GLOBAL_VERSION_ONE");
writeFileSync(%q,"WORKSPACE_VERSION_ONE");
const child=await run_agent({system:"CHILD_EXPLICIT"},"child");
if(child.lastResult?.state!=="completed") throw Error(JSON.stringify(child));
console.log(child.lastResult.answer);`, common, workspace)
			return hookToolReply(map[string]string{"instruction-child": code}, "instruction-child"), nil
		}
		return agentAnswer(input + " done"), nil
	})
	source := fmt.Sprintf(`import {run_agent} from "noe:agent";
import {readFile} from "node:fs/promises";
const system=(await Promise.all([%q,%q].map(path=>readFile(path,"utf8")))).join("\n\n");
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",system,maxSteps:2},"parent");
if(result.lastResult?.state!=="completed") throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`, filepath.Join(cwd, "first.md"), filepath.Join(cwd, "second.md"))
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "scripts", "entry.js"), Code: source}, Options{
		Cwd: cwd, InstructionDirs: []string{"selected-a", "selected-b"}, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr,
	})
	if code != 0 || err != nil || out.String() != "parent done\n" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen["parent"]) != 2 || len(seen["child"]) != 1 {
		t.Fatalf("requests per task: %v", seen)
	}
	if seen["parent"][0] != seen["parent"][1] {
		t.Fatal("parent instructions changed after tool rewrote files")
	}
	for task, markers := range map[string][]string{
		"parent": {"GLOBAL_VERSION_ZERO", "WORKSPACE_VERSION_ZERO", "EXPLICIT_FIRST", "EXPLICIT_SECOND"},
		"child":  {"GLOBAL_VERSION_ONE", "WORKSPACE_VERSION_ONE", "CHILD_EXPLICIT"},
	} {
		text := seen[task][0]
		if !strings.HasPrefix(text, (agent.Facts() + "\n\n" + agent.URLImportFacts("enabled"))) {
			t.Fatalf("%s lost built-in instructions", task)
		}
		position := len((agent.Facts() + "\n\n" + agent.URLImportFacts("enabled")))
		for _, marker := range markers {
			next := strings.Index(text, marker)
			if next < position || strings.Count(text, marker) != 1 {
				t.Fatalf("%s instruction missing, duplicated, or out of order: %s", task, marker)
			}
			position = next + len(marker)
		}
	}
	if strings.Contains(seen["parent"][0], "EXCLUDED_DEFAULT") || strings.Contains(seen["child"][0], "EXCLUDED_DEFAULT") || strings.Contains(seen["child"][0], "EXPLICIT_FIRST") || strings.Contains(seen["child"][0], "VERSION_ZERO") || strings.Contains(seen["parent"][1], "VERSION_ONE") {
		t.Fatal("parent/child instruction snapshots leaked across runs")
	}
}

func TestAgentInstructionsLazyIdleFailureAndFrozenQueue(t *testing.T) {
	cwd, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", config)
	// A malformed instruction directory must not affect import or idle spawn.
	bad := filepath.Join(cwd, "selected-instructions")
	writeAgentInstructionFixture(t, bad, "not a directory")
	var mu sync.Mutex
	seen := map[string]string{}
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		var system string
		for _, raw := range req.Messages {
			var message runtimeProviderMessage
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			if message.Role == "system" {
				system = message.Content
				break
			}
		}
		input := interruptUserPrompt(req)
		if system == "" || input == "" {
			return provider.Response{}, fmt.Errorf("provider request lost task/system message: %+v", req.Messages)
		}
		mu.Lock()
		seen[input] = system
		mu.Unlock()
		return agentAnswer("done"), nil
	})
	source := fmt.Sprintf(`import {spawn_agent,queue_agent,observe_agent,stop_agent} from "noe:agent";
import {unlink,mkdir,writeFile} from "node:fs/promises";
const dir=%q;
const config={provider:"openrouter",model:"fixture/model",effort:"auto",system:"QUEUE_EXPLICIT"};
let {agentId}=await spawn_agent(config,undefined,{keepAlive:true});
async function task(instruction) {
 await queue_agent({agentId,instruction,commandId:instruction});
 while(true) {
  const state=await observe_agent({agentId});
  if(state.lastResult?.commandId===instruction && state.state==="idle") return state.lastResult;
  if(state.state==="failed" || state.state==="stopped") throw Error(JSON.stringify(state));
  await new Promise(resolve=>setTimeout(resolve,5));
 }
}
const failed=await task("bad-directory");
if(failed.state!=="failed" || !failed.runId || !failed.error.includes(dir)) throw Error(JSON.stringify(failed));
await unlink(dir);
await mkdir(dir);
await writeFile(dir+"/project.md","QUEUED_VERSION_ONE");
if((await task("still-bad")).state!=="failed")throw Error("cached source error lost");
await stop_agent({agentId});
({agentId}=await spawn_agent(config,undefined,{keepAlive:true}));
if((await task("first-good")).state!=="completed") throw Error("first repaired task failed");
await writeFile(dir+"/project.md","QUEUED_VERSION_TWO");
if((await task("second-good")).state!=="completed") throw Error("next task failed");
await stop_agent({agentId});
console.log("queue snapshots verified");`, bad)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	code, err := Execute(ctx, Source{Name: filepath.Join(cwd, "main.js"), Code: source}, Options{
		Cwd: cwd, InstructionDirs: []string{"selected-instructions"}, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr,
	})
	if code != 0 || err != nil || out.String() != "queue snapshots verified\n" {
		t.Fatalf("code=%d err=%v stdout=%q stderr=%q", code, err, &out, &stderr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 || !strings.Contains(seen["first-good"], "QUEUED_VERSION_ONE") || strings.Contains(seen["first-good"], "VERSION_TWO") || !strings.Contains(seen["second-good"], "QUEUED_VERSION_ONE") || strings.Contains(seen["second-good"], "VERSION_TWO") {
		t.Fatalf("queue task instruction snapshots: %v", seen)
	}
	for _, system := range seen {
		if strings.Count(system, "QUEUE_EXPLICIT") != 1 {
			t.Fatal("explicit system not retained in queued task")
		}
	}
}
