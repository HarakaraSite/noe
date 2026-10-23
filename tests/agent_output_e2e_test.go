//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const verboseToolSource = `// Emit more output than the retained stream budgets
console.log("OUT-HEAD"); console.error("ERR-HEAD");
const chunk="x".repeat(8191);
for(let i=0;i<256;i++){console.log(chunk);console.error(chunk);}
console.log("OUT-TAIL"); console.error("ERR-TAIL");
`

func checkTruncatedToolResult(t *testing.T, result agentExecutionResult, exit int) {
	t.Helper()
	const limit = 65536
	const emitted = len("OUT-HEAD\n") + 256*8192 + len("OUT-TAIL\n")
	wantReason := "completed"
	if exit != 0 {
		wantReason = "failed"
	}
	if result.ExitCode != exit || result.Reason != wantReason {
		t.Errorf("exit=%d reason=%s, want exit=%d reason=%s", result.ExitCode, result.Reason, exit, wantReason)
	}
	for _, stream := range []struct {
		name, text string
		omitted    int64
	}{
		{"OUT", result.Stdout, result.StdoutOmittedBytes},
		{"ERR", result.Stderr, result.StderrOmittedBytes},
	} {
		if !strings.HasPrefix(stream.text, stream.name+"-HEAD\n") || !strings.HasSuffix(stream.text, stream.name+"-TAIL\n") {
			t.Errorf("%s lost head or tail", stream.name)
		}
		if stream.omitted != int64(emitted-limit) || len(stream.text) > limit+100 || !strings.Contains(stream.text, "[NOE: output truncated;") {
			t.Errorf("%s captured=%d omitted=%d want omitted=%d", stream.name, len(stream.text), stream.omitted, emitted-limit)
		}
	}
}

type modelOutputPreview struct {
	OutputPreview string `json:"outputPreview"`
	OutputLimit   struct {
		LimitBytes    int  `json:"limitBytes"`
		SourceBytes   int  `json:"sourceBytes"`
		ShownBytes    int  `json:"shownBytes"`
		OmittedBytes  int  `json:"omittedBytes"`
		ReturnedBytes int  `json:"returnedBytes"`
		Complete      bool `json:"complete"`
	} `json:"outputLimit"`
	Execution struct {
		ExitCode int    `json:"exitCode"`
		Reason   string `json:"reason"`
	} `json:"execution"`
}

func checkModelOutputPreview(t *testing.T, result modelOutputPreview, exit int) {
	t.Helper()
	reason := "completed"
	if exit != 0 {
		reason = "failed"
	}
	limit := result.OutputLimit
	if result.Execution.ExitCode != exit || result.Execution.Reason != reason || limit.LimitBytes != 3072 || limit.ReturnedBytes > 3072 || limit.OmittedBytes <= 0 || limit.Complete || limit.ShownBytes+limit.OmittedBytes != limit.SourceBytes {
		t.Errorf("incorrect model projection: %+v", result)
	}
}

func TestAgentOutputTruncationReachesHooksModelAndEvents(t *testing.T) {
	for _, exit := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit=%d", exit), func(t *testing.T) {
			work := t.TempDir()
			writeFixture(t, filepath.Join(work, "check-output.js"), `export function afterTool(event) {
  if (!(event.result.stdoutOmittedBytes>0 && event.result.stderrOmittedBytes>0)) throw Error("missing truncation metadata");
  if (!event.result.stdout.includes("OUT-TAIL") || !event.result.stderr.includes("ERR-TAIL")) throw Error("missing output tail");
  return {result:{...event.result,hookSawTruncation:true}};
}`)
			observed := make(chan modelOutputPreview, 1)
			server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
				if len(req.Messages) == 2 {
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("verbose", verboseToolSource+fmt.Sprintf("process.exit(%d);", exit))}}}, FinishReason: "tool_calls"}
				}
				for _, message := range req.Messages {
					if message.Role != "tool" {
						continue
					}
					var text string
					_ = json.Unmarshal(message.Content, &text)
					var preview modelOutputPreview
					if err := json.Unmarshal([]byte(text), &preview); err != nil {
						t.Fatal(err)
					}
					checkModelOutputPreview(t, preview, exit)
					if !strings.Contains(preview.OutputPreview, `"hookSawTruncation":true`) {
						t.Error("model did not receive hook result preview tail")
					}
					observed <- preview
					return agentModelReply{Content: "output inspected"}
				}
				t.Error("missing model tool result")
				return agentModelReply{Content: "missing result"}
			})
			defer server.Close()
			env := agentCLIEnv(t, server.URL)
			env["NOE_AGENT_TRACE"] = ""
			script := agentScript("inspect verbose output", `{maxSteps:2,hooks:["./check-output.js"]}`)
			result := runCLI(t, work, env, "eval", "--events=-", script)
			requireExit(t, result, 0)
			select {
			case received := <-observed:
				checkModelOutputPreview(t, received, exit)
			default:
				t.Fatal("model did not receive a captured result")
			}
			seen := map[string]bool{}
			for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
				var record struct {
					Type               string               `json:"type"`
					Source             string               `json:"source"`
					Text               string               `json:"text"`
					OmittedBytes       int64                `json:"omittedBytes"`
					StdoutOmittedBytes int64                `json:"stdoutOmittedBytes"`
					StderrOmittedBytes int64                `json:"stderrOmittedBytes"`
					RawResult          agentExecutionResult `json:"rawResult"`
					Result             modelOutputPreview   `json:"result"`
					HookResult         agentExecutionResult `json:"hookResult"`
				}
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatal(err)
				}
				switch record.Type {
				case "stdout", "stderr":
					if record.Source == "run_js" {
						if record.OmittedBytes <= 0 || len(record.Text) > 65536+100 {
							t.Errorf("unbounded or unmarked %s event", record.Type)
						}
						seen[record.Type] = true
					}
				case "run_js.finished":
					if record.StdoutOmittedBytes <= 0 || record.StderrOmittedBytes <= 0 {
						t.Error("finished event lost omission metadata")
					}
					seen[record.Type] = true
				case "tool.finished":
					checkTruncatedToolResult(t, record.RawResult, exit)
					checkModelOutputPreview(t, record.Result, exit)
					checkTruncatedToolResult(t, record.HookResult, exit)
					seen[record.Type] = true
				}
			}
			for _, kind := range []string{"stdout", "stderr", "run_js.finished", "tool.finished"} {
				if !seen[kind] {
					t.Errorf("missing %s event", kind)
				}
			}
		})
	}
}

func TestAgentOutputTruncationStillAllowsCancellation(t *testing.T) {
	work := t.TempDir()
	ready := filepath.Join(work, "ready")
	encoded, _ := json.Marshal(ready)
	tool := verboseToolSource + `const {writeFile}=await import("node:fs/promises");
await writeFile(` + string(encoded) + `,"ready");
setInterval(()=>{},1000);await new Promise(()=>{});`
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, _ agentChatRequest) agentModelReply {
		if calls.Add(1) != 1 {
			t.Errorf("canceled task made an extra model request: %d", calls.Load())
			return agentModelReply{Content: "unexpected"}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("verbose", tool)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	eventPath := filepath.Join(work, "events.jsonl")
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	script := `import {spawn_agent,queue_agent,cancel_task,wait_task,stop_agent} from "noe:agent";
import {readFile} from "node:fs/promises";
const worker=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto",name:"output-cancel"},undefined,{keepAlive:true});
const commandId="output-cancel-task";
await queue_agent({agentId:worker.agentId,commandId,instruction:"emit verbose output and wait"});
let ready=false;for(let i=0;i<5000;i++){try{await readFile("ready");ready=true;break}catch{await new Promise(resolve=>setTimeout(resolve,2));}}
if(!ready)throw Error("tool did not write its readiness marker");
const receipt=await cancel_task({agentId:worker.agentId,commandId});
const outcome=await wait_task({agentId:worker.agentId,commandId});
await stop_agent({agentId:worker.agentId});
console.log(JSON.stringify({receipt,outcome}));`
	result := runCLI(t, work, env, "eval", "--events="+eventPath, script)
	requireExit(t, result, 0)
	if calls.Load() != 1 || result.stderr != "" {
		t.Fatalf("canceled output task: calls=%d result=%+v", calls.Load(), result)
	}
	var outcome struct {
		Receipt struct {
			Status    string `json:"status"`
			CommandID string `json:"commandId"`
		} `json:"receipt"`
		Outcome struct {
			State     string `json:"state"`
			CommandID string `json:"commandId"`
		} `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(result.stdout)), &outcome); err != nil {
		t.Fatalf("invalid cancellation result: %v\n%s", err, result.stdout)
	}
	if outcome.Receipt.Status != "accepted" || outcome.Receipt.CommandID != "output-cancel-task" || outcome.Outcome.State != "cancelled" || outcome.Outcome.CommandID != "output-cancel-task" {
		t.Fatalf("task cancellation contract: %+v", outcome)
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	seenRunJS, seenStdout, seenStderr := false, false, false
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Type               string `json:"type"`
			Source             string `json:"source"`
			OmittedBytes       int64  `json:"omittedBytes"`
			StdoutOmittedBytes int64  `json:"stdoutOmittedBytes"`
			StderrOmittedBytes int64  `json:"stderrOmittedBytes"`
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}
		switch event.Type {
		case "stdout":
			if event.Source == "run_js" && event.OmittedBytes > 0 {
				seenStdout = true
			}
		case "stderr":
			if event.Source == "run_js" && event.OmittedBytes > 0 {
				seenStderr = true
			}
		case "run_js.finished":
			seenRunJS = event.StdoutOmittedBytes > 0 && event.StderrOmittedBytes > 0
		}
	}
	if !seenRunJS || !seenStdout || !seenStderr {
		t.Fatalf("cancelled tool lost bounded output events: run_js=%v stdout=%v stderr=%v", seenRunJS, seenStdout, seenStderr)
	}
}
