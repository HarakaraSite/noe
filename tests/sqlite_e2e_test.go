//go:build e2e

package e2e_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteAcrossExecutionModes(t *testing.T) {
	example := filepath.Join(repoRootPath, "examples", "sqlite", "notes.js")
	source, err := os.ReadFile(example)
	if err != nil {
		t.Fatal(err)
	}
	const want = "{\"inserted\":[1,2],\"notes\":[{\"id\":1,\"text\":\"hello\"},{\"id\":2,\"text\":\"日本語\"}]}\n"
	for _, mode := range []string{"run", "eval", "worker", "agent"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var result cliResult
			switch mode {
			case "run":
				result = runCLI(t, work, nil, "run", example)
			case "eval":
				result = runCLI(t, work, nil, "eval", string(source))
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.mjs"), string(source)+`import {parentPort} from "node:worker_threads";parentPort.postMessage("ok");parentPort.close();`)
				result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";const worker=new Worker("./worker.mjs");const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});await worker.terminate();if(result!=="ok")throw Error("Worker SQLite");`)
			case "agent":
				provider := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
					for _, message := range request.Messages {
						if message.Role == "tool" {
							tool := agentToolResult(t, message)
							if tool.ExitCode != 0 || tool.Stdout != want || tool.Stderr != "" {
								t.Errorf("SQLite tool result=%+v", tool)
							}
							return agentModelReply{Content: "SQLite passed"}
						}
					}
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("sqlite", "// Use an in-memory SQL database.\n"+string(source))}}}, FinishReason: "tool_calls"}
				})
				defer provider.Close()
				env := agentCLIEnv(t, provider.URL)
				env["NOE_AGENT_TRACE"] = ""
				result = runCLI(t, work, env, "eval", `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"check SQLite");if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`)
			}
			requireExit(t, result, 0)
			expected := want
			if mode == "agent" {
				expected = "SQLite passed\n"
			}
			if result.stdout != expected || result.stderr != "" {
				t.Fatalf("%s result=%+v", mode, result)
			}
		})
	}
}

func TestSQLiteExamplePersistsThroughCLIExecutions(t *testing.T) {
	work := t.TempDir()
	example := filepath.Join(repoRootPath, "examples", "sqlite", "notes.js")
	for i, want := range []string{
		"{\"inserted\":[1,2],\"notes\":[{\"id\":1,\"text\":\"hello\"},{\"id\":2,\"text\":\"日本語\"}]}\n",
		"{\"inserted\":[3,4],\"notes\":[{\"id\":1,\"text\":\"hello\"},{\"id\":2,\"text\":\"日本語\"},{\"id\":3,\"text\":\"hello\"},{\"id\":4,\"text\":\"日本語\"}]}\n",
	} {
		result := runCLI(t, work, nil, "run", example, "notes.db")
		requireExit(t, result, 0)
		if result.stdout != want || result.stderr != "" {
			t.Fatalf("execution %d result=%+v", i, result)
		}
	}
}
