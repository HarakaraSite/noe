//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/internal/loganalysis"
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
	StdoutFile    *executionresult.OutputFile
	StderrFile    *executionresult.OutputFile
	DataFile      *executionresult.OutputFile
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

func TestAgentSavesOverflowAndReadsTheMiddleInNextTool(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "output")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	source := "// @noe: {\"outputBytes\": 1024, \"saveData\": true}\n" + `
console.log("a".repeat(4096) + "STDOUT_MIDDLE" + "z".repeat(4096));
console.error("b".repeat(4096) + "STDERR_MIDDLE" + "y".repeat(4096));
export default {text: "x".repeat(4096) + "DATA_MIDDLE" + "y".repeat(4096)};`
	writeFixture(t, filepath.Join(work, "check-data.js"), `export function afterTool(event) {
  if (event.toolCallId === "save" && (!event.result.dataFile || Object.hasOwn(event.result, "data"))) {
    throw Error("expected saved data reference");
  }
}`)
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		tools := make(map[string]string)
		for _, message := range req.Messages {
			if message.Role == "tool" {
				var value string
				if err := json.Unmarshal(message.Content, &value); err != nil {
					t.Errorf("tool content: %v", err)
					return agentModelReply{Status: 400}
				}
				tools[message.ToolCallID] = value
			}
		}
		switch calls.Add(1) {
		case 1:
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("save", source)}}}, FinishReason: "tool_calls"}
		case 2:
			var preview modelOutputPreview
			if err := json.Unmarshal([]byte(tools["save"]), &preview); err != nil || preview.StdoutFile == nil || preview.StderrFile == nil || preview.DataFile == nil ||
				preview.OutputLimit.ReturnedBytes != len(tools["save"]) || len(tools["save"]) > 1024 || strings.Contains(preview.OutputPreview, "_MIDDLE") {
				t.Errorf("model preview=%+v err=%v", preview, err)
				return agentModelReply{Status: 400}
			}
			read := fmt.Sprintf(`
import {open, readFile} from "noe:fs";
await using out=await open(%s); await using err=await open(%s);
const decode=new TextDecoder();
const data=JSON.parse(await readFile(%s,"utf8"));
export default [decode.decode(await out.readAt(4096,13)),decode.decode(await err.readAt(4096,13)),data.text.slice(4096,4107)];`,
				mustJSONString(preview.StdoutFile.Path), mustJSONString(preview.StderrFile.Path), mustJSONString(preview.DataFile.Path))
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("read", read)}}}, FinishReason: "tool_calls"}
		case 3:
			var result agentExecutionResult
			if err := json.Unmarshal([]byte(tools["read"]), &result); err != nil || string(result.Data) != `["STDOUT_MIDDLE","STDERR_MIDDLE","DATA_MIDDLE"]` {
				t.Errorf("read saved output: %+v err=%v", result, err)
			}
			return agentModelReply{Content: "read saved output and data"}
		default:
			t.Error("unexpected tool rerun")
			return agentModelReply{Status: 400}
		}
	})
	defer server.Close()

	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	env["NOE_AGENT_TOOL_OUTPUT_DIR"] = "output"
	eventPath := filepath.Join(work, "events.jsonl")
	result := runCLI(t, work, env, "eval", "--no-inst", "--observe", "--events="+eventPath, agentScript("read the omitted bytes", `{maxSteps:3,hooks:["./check-data.js"]}`))
	requireExit(t, result, 0)
	if strings.Contains(result.stdout, "hook failed") {
		t.Fatalf("afterTool did not receive the saved data reference: %s", result.stdout)
	}
	for _, want := range []string{"stdout saved", "stderr saved", "data saved", "read saved output and data"} {
		if !strings.Contains(result.stdout, want) {
			t.Fatalf("output missing %q: %s", want, result.stdout)
		}
	}
	events, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	var prefix strings.Builder
	for line := range strings.SplitSeq(string(events), "\n") {
		prefix.WriteString(line + "\n")
		var event struct {
			Type, RunID, ToolCallID string
			Data                    json.RawMessage `json:"data"`
		}
		if json.Unmarshal([]byte(line), &event) != nil || event.Type != "run_js.finished" || event.ToolCallID != "save" {
			continue
		}
		snapshot, err := loganalysis.Analyze(t.Context(), strings.NewReader(prefix.String()), loganalysis.Query{RunID: event.RunID})
		if err != nil {
			t.Fatal(err)
		}
		last := snapshot.LastToolResult
		if last == nil || last.StdoutFile == nil || last.StderrFile == nil || last.DataFile == nil || len(event.Data) != 0 {
			t.Fatalf("saved-log snapshot omitted file references: %+v", last)
		}
		for _, stream := range []struct {
			file *executionresult.OutputFile
			text string
		}{
			{last.StdoutFile, strings.Repeat("a", 4096) + "STDOUT_MIDDLE" + strings.Repeat("z", 4096) + "\n"},
			{last.StderrFile, strings.Repeat("b", 4096) + "STDERR_MIDDLE" + strings.Repeat("y", 4096) + "\n"},
			{last.DataFile, `{"text":"` + strings.Repeat("x", 4096) + "DATA_MIDDLE" + strings.Repeat("y", 4096) + `"}`},
		} {
			content, err := os.ReadFile(stream.file.Path)
			if err != nil || filepath.Dir(stream.file.Path) != dir || string(content) != stream.text || int64(len(content)) != stream.file.Bytes {
				t.Fatalf("saved file=%+v length=%d err=%v", stream.file, len(content), err)
			}
		}
		return
	}
	t.Fatal("missing run_js.finished record for saved streams")
}

func TestAgentRunJSDeadlineAndLiveProgress(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	releaseTool := func() { once.Do(func() { close(release) }) }
	hold := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
			fmt.Fprint(w, "sibling data")
		case <-r.Context().Done():
		}
	}))
	defer hold.Close()
	defer releaseTool()

	var requests atomic.Int32
	provider := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		switch requests.Add(1) {
		case 1:
			timed := agentTool("timed", "// @noe: {\"timeoutMs\": 1000}\n"+
				`console.log("timed output"); await new Promise(resolve => setTimeout(resolve, 5000));`)
			sibling := agentTool("sibling", fmt.Sprintf(`
console.log("live marker");
console.log("another line");
console.error("live error");
export default await (await fetch(%s)).text();`, mustJSONString(hold.URL)))
			sibling["index"] = 1
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{timed, sibling}}}, FinishReason: "tool_calls"}
		case 2:
			results := make(map[string]agentExecutionResult)
			for _, message := range req.Messages {
				if message.Role == "tool" {
					results[message.ToolCallID] = agentToolResult(t, message)
				}
			}
			timed := results["timed"]
			if timed.Reason != "timeout" || timed.Stdout != "timed output\n" ||
				len(timed.Diagnostics) == 0 || timed.Diagnostics[0].Code != "timeout" {
				t.Errorf("timed tool result = %+v", timed)
			}
			sibling := results["sibling"]
			if sibling.Reason != "completed" || sibling.Stdout != "live marker\nanother line\n" ||
				sibling.Stderr != "live error\n" || string(sibling.Data) != `"sibling data"` {
				t.Errorf("sibling tool result = %+v", sibling)
			}
			return agentModelReply{Content: "agent continued"}
		default:
			t.Error("unexpected model request")
			return agentModelReply{Status: 400}
		}
	})
	defer provider.Close()
	env := agentCLIEnv(t, provider.URL)
	env["NOE_AGENT_TRACE"] = ""
	for name, value := range env {
		t.Setenv(name, value)
	}
	process := startCLIWithOpenStdin(t, t.TempDir(), "eval", "--no-inst", "--observe",
		agentScript("report progress while one tool times out", "{maxSteps:2}"))
	for _, marker := range []string{"run_js stdout: live marker", "run_js stderr: live error"} {
		if !process.stdout.waitFor(marker, 4*time.Second) {
			t.Fatalf("no live %q while tool waited: stdout=%q stderr=%q", marker, process.stdout.String(), process.stderr.String())
		}
	}
	if strings.Contains(process.stdout.String(), "another line") {
		t.Fatal("progress did not skip a second line in the same interval")
	}
	releaseTool()
	result := process.wait(t, 5*time.Second)
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "agent continued") {
		t.Fatalf("agent did not continue: %+v", result)
	}
}

func checkModelOutputPreview(t *testing.T, result modelOutputPreview, exit int) {
	t.Helper()
	reason := "completed"
	if exit != 0 {
		reason = "failed"
	}
	limit := result.OutputLimit
	if result.Execution.ExitCode != exit || result.Execution.Reason != reason || limit.LimitBytes != 65536 || limit.ReturnedBytes > 65536 || limit.OmittedBytes <= 0 || limit.Complete || limit.ShownBytes+limit.OmittedBytes != limit.SourceBytes {
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
			env["NOE_AGENT_TOOL_OUTPUT_DIR"] = ""
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
	tool := verboseToolSource + `const {writeFile}=await import("noe:fs");
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
	env["NOE_AGENT_TOOL_OUTPUT_DIR"] = work
	script := `import {spawn_agent,queue_agent,cancel_task,wait_task,stop_agent} from "noe:agent";
import {readFile} from "noe:fs";
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
			StdoutFile         *executionresult.OutputFile
			StderrFile         *executionresult.OutputFile
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
			for _, file := range []*executionresult.OutputFile{event.StdoutFile, event.StderrFile} {
				if file == nil {
					t.Fatal("cancelled tool lost a saved stream reference")
				}
				info, err := os.Stat(file.Path)
				if err != nil || info.Size() != file.Bytes || file.Bytes <= 65536 {
					t.Fatalf("cancelled saved stream=%+v info=%v err=%v", file, info, err)
				}
			}
		}
	}
	if !seenRunJS || !seenStdout || !seenStderr {
		t.Fatalf("cancelled tool lost bounded output events: run_js=%v stdout=%v stderr=%v", seenRunJS, seenStdout, seenStderr)
	}
}
