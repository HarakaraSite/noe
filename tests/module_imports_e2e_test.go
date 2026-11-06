//go:build e2e

package e2e_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func dataImportFixture(t *testing.T) (string, string, string) {
	t.Helper()
	work := t.TempDir()
	mapPath := filepath.Join(work, "maps", "import-map.json")
	writeFixture(t, mapPath, `{"imports":{"library/":"../lib/","assets/":"../data/","choice":"../data/default.json"},"scopes":{"../lib/scoped/":{"choice":"../data/scoped.json"}}}`)
	writeFixture(t, filepath.Join(work, "data", "default.json"), `{"value":1}`)
	writeFixture(t, filepath.Join(work, "data", "scoped.json"), `{"value":42}`)
	writeFixture(t, filepath.Join(work, "data", "template"), "hello 日本語")
	writeFixture(t, filepath.Join(work, "data", "payload"), string([]byte{0, 255, 128, 10}))
	writeFixture(t, filepath.Join(work, "lib", "scoped", "read.mjs"), `import choice from "choice" with {type:"json"};
import template from "assets/template" with {type:"text"};
import payload from "assets/payload" with {type:"bytes"};
const again=await import("assets/scoped.json",{with:{type:"json"}});
if(again.default!==choice||template!=="hello 日本語"||Array.from(payload).join()!=="0,255,128,10"||!payload.buffer.immutable)throw Error("data import contract");
export const value=choice.value;`)
	code := `import {value} from "library/scoped/read.mjs";if(value!==42)throw Error("scoped import");console.log("data import passed");`
	return work, mapPath, code
}

func TestDataImportsAcrossCLIAndWorker(t *testing.T) {
	for _, mode := range []string{"run", "eval", "worker"} {
		t.Run(mode, func(t *testing.T) {
			work, mapPath, code := dataImportFixture(t)
			var result cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "main.js"), code)
				result = runCLI(t, work, nil, "run", "--import-map", mapPath, "main.js")
			case "eval":
				result = runCLI(t, work, nil, "eval", "--import-map", mapPath, code)
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.txt"), `import {parentPort} from "node:worker_threads";import {value} from "library/scoped/read.mjs";parentPort.postMessage(value);parentPort.close();`)
				result = runCLI(t, work, nil, "eval", "--import-map", mapPath, `import {Worker} from "node:worker_threads";const worker=new Worker("./worker.txt",{type:"module"});const value=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});await worker.terminate();if(value!==42)throw Error("worker data");console.log("data import passed");`)
			}
			requireExit(t, result, 0)
			if result.stdout != "data import passed\n" || result.stderr != "" {
				t.Fatalf("result=%+v", result)
			}
		})
	}
}

func TestDataImportsInFixtureAgentSnapshot(t *testing.T) {
	work, mapPath, code := dataImportFixture(t)
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 0 || result.Stdout != "data import passed\n" || result.Stderr != "" {
					t.Errorf("tool data import=%+v", result)
				}
				return agentModelReply{Content: "scoped data received"}
			}
		}
		// Bootstrap has already captured both imports and scopes. run_js must
		// use that snapshot after the original configuration is removed.
		if err := os.Remove(mapPath); err != nil {
			t.Error(err)
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("read-data", code)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, work, env, "eval", "--import-map", mapPath, `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"read scoped local data");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`)
	requireExit(t, result, 0)
	if result.stdout != "scoped data received\n" || result.stderr != "" {
		t.Fatalf("agent result=%+v", result)
	}
}

func TestHTTPDataImportsAcrossExecutionModes(t *testing.T) {
	assets := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/settings":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"value":42}`)
		case "/template":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "\ufeffhello")
		case "/payload":
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte{0xef, 0xbb, 0xbf, 0, 255, 128})
		default:
			http.NotFound(w, r)
		}
	}))
	defer assets.Close()
	code := fmt.Sprintf(`import settings from %q with {type:"json"};
import text from %q with {type:"text"};import bytes from %q with {type:"bytes"};
const again=await import(%q,{with:{type:"json"}});
if(settings!==again.default||settings.value!==42||text!=="\ufeffhello"||!bytes.buffer.immutable||Array.from(bytes).join()!=="239,187,191,0,255,128")throw Error("HTTP data import contract");
console.log("HTTP data import passed");`, assets.URL+"/settings", assets.URL+"/template", assets.URL+"/payload", assets.URL+"/settings")
	for _, mode := range []string{"run", "eval", "worker", "agent"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var result cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "main.js"), code)
				result = runCLI(t, work, nil, "run", "main.js")
			case "eval":
				result = runCLI(t, work, nil, "eval", code)
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.mjs"), code+`import {parentPort} from "node:worker_threads";parentPort.postMessage(42);parentPort.close();`)
				result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";const worker=new Worker("./worker.mjs");const answer=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});await worker.terminate();if(answer!==42)throw Error("HTTP worker failed");`)
			case "agent":
				provider := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
					for _, message := range request.Messages {
						if message.Role == "tool" {
							tool := agentToolResult(t, message)
							if tool.ExitCode != 0 || tool.Stdout != "HTTP data import passed\n" || tool.Stderr != "" {
								t.Errorf("HTTP tool result=%+v", tool)
							}
							return agentModelReply{Content: "HTTP data import passed"}
						}
					}
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("http-data", code)}}}, FinishReason: "tool_calls"}
				})
				defer provider.Close()
				env := agentCLIEnv(t, provider.URL)
				env["NOE_AGENT_TRACE"] = ""
				result = runCLI(t, work, env, "eval", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"read HTTP data");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`)
			}
			requireExit(t, result, 0)
			if result.stdout != "HTTP data import passed\n" || result.stderr != "" {
				t.Fatalf("%s result=%+v", mode, result)
			}
		})
	}
}
