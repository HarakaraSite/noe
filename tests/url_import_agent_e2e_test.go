//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestURLImportAgentDelegationAndFreshRunJSSources(t *testing.T) {
	var revision, sources, dependencies, modelCalls atomic.Int32
	revision.Store(1)
	modules := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		switch r.URL.Path {
		case "/library":
			sources.Add(1)
			fmt.Fprint(w, `import {version} from "./dependency.data";export {version};
globalThis.urlEvaluations=(globalThis.urlEvaluations||0)+1;export const evaluations=globalThis.urlEvaluations;`)
		case "/dependency.data":
			dependencies.Add(1)
			fmt.Fprintf(w, "export const version=%d;", revision.Load())
		default:
			http.NotFound(w, r)
		}
	}))
	defer modules.Close()
	resultFor := func(req agentChatRequest, id string) (agentExecutionResult, bool) {
		for _, message := range req.Messages {
			if message.Role == "tool" && message.ToolCallID == id {
				return agentToolResult(t, message), true
			}
		}
		return agentExecutionResult{}, false
	}
	reply := func(id, code string) agentModelReply {
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool(id, code)}}}, FinishReason: "tool_calls"}
	}
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		modelCalls.Add(1)
		var system string
		if len(req.Messages) == 0 || json.Unmarshal(req.Messages[0].Content, &system) != nil ||
			!strings.Contains(system, "HTTP/HTTPS ESM URL imports: enabled.") {
			t.Error("parent/child did not receive enabled URL import facts")
			return agentModelReply{Status: 500}
		}
		switch agentUser(req) {
		case "url-parent":
			if result, ok := resultFor(req, "parent-second"); ok {
				if result.ExitCode != 0 || result.Stdout != "2:child-ready\n" {
					t.Errorf("second parent run_js=%+v", result)
				}
				return agentModelReply{Content: "URL imports complete"}
			}
			if result, ok := resultFor(req, "parent-first"); ok {
				if result.ExitCode != 0 || result.Stdout != "1\n" {
					t.Errorf("first parent run_js=%+v", result)
				}
				revision.Store(2)
				return reply("parent-second", `// Import fresh source and delegate with the same Host profile.
import {run_agent} from "noe:agent";
const lib=await import("remote/lib");
if(lib.version!==2||lib.evaluations!==1)throw Error("source or JS state leaked between run_js calls");
const child=await run_agent({},"url-child");
if(child.lastResult?.state!=="completed")throw Error(JSON.stringify(child));
console.log(lib.version+":"+child.lastResult.answer);`)
			}
			return reply("parent-first", `// Read the library and its relative URL dependency.
import * as lib from "remote/lib";
const again=await import("remote/lib");
if(lib!==again||lib.evaluations!==1)throw Error("module identity");
console.log(lib.version);`)
		case "url-child":
			if result, ok := resultFor(req, "child-import"); ok {
				if result.ExitCode != 0 || result.Stdout != "2:1\n" {
					t.Errorf("child run_js=%+v", result)
				}
				return agentModelReply{Content: "child-ready"}
			}
			return reply("child-import", `// Use the inherited URL mapping in an independent runtime.
const lib=await import("remote/lib");console.log(lib.version+":"+lib.evaluations);`)
		default:
			t.Errorf("unexpected instruction %q", agentUser(req))
			return agentModelReply{Status: 500}
		}
	})
	defer server.Close()
	work := t.TempDir()
	mapPath := filepath.Join(work, "map.json")
	encoded, err := json.Marshal(map[string]any{"imports": map[string]string{"remote/lib": modules.URL + "/library"}})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, mapPath, string(encoded))
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"none",maxSteps:4},"url-parent");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, work, env, "eval", "--import-map", mapPath, source)
	requireExit(t, result, 0)
	if result.stdout != "URL imports complete\n" || result.stderr != "" {
		t.Fatalf("CLI result=%+v", result)
	}
	if sources.Load() != 3 || dependencies.Load() != 3 || modelCalls.Load() != 5 {
		t.Fatalf("requests source=%d dependency=%d model=%d", sources.Load(), dependencies.Load(), modelCalls.Load())
	}
}
