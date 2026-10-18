//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"noe/internal/agent"
)

const verboseToolSource = `// Emit more output than the retained stream budgets
console.log("OUT-HEAD"); console.error("ERR-HEAD");
const chunk="x".repeat(8191);
for(let i=0;i<256;i++){console.log(chunk);console.error(chunk);}
console.log("OUT-TAIL"); console.error("ERR-TAIL");
`

func checkTruncatedToolResult(t *testing.T, result agent.Result, exit int) {
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

func TestAgentOutputTruncationReachesHooksModelAndEvents(t *testing.T) {
	for _, exit := range []int{0, 7} {
		t.Run(fmt.Sprintf("exit=%d", exit), func(t *testing.T) {
			work := t.TempDir()
			writeFixture(t, filepath.Join(work, "check-output.js"), `export function afterTool(event) {
  if (!(event.result.stdoutOmittedBytes>0 && event.result.stderrOmittedBytes>0)) throw Error("missing truncation metadata");
  if (!event.result.stdout.includes("OUT-TAIL") || !event.result.stderr.includes("ERR-TAIL")) throw Error("missing output tail");
  return {result:{...event.result,hookSawTruncation:true}};
}`)
			observed := make(chan agent.Result, 1)
			server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
				if len(req.Messages) == 2 {
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("verbose", verboseToolSource+fmt.Sprintf("process.exit(%d);", exit))}}}, FinishReason: "tool_calls"}
				}
				for _, message := range req.Messages {
					if message.Role != "tool" {
						continue
					}
					result := agentToolResult(t, message)
					var text string
					_ = json.Unmarshal(message.Content, &text)
					if !strings.Contains(text, `"hookSawTruncation":true`) {
						t.Error("model did not receive hook result")
					}
					observed <- result
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
				checkTruncatedToolResult(t, received, exit)
			default:
				t.Fatal("model did not receive a captured result")
			}
			seen := map[string]bool{}
			for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
				var record struct {
					Type               string       `json:"type"`
					Source             string       `json:"source"`
					Text               string       `json:"text"`
					OmittedBytes       int64        `json:"omittedBytes"`
					StdoutOmittedBytes int64        `json:"stdoutOmittedBytes"`
					StderrOmittedBytes int64        `json:"stderrOmittedBytes"`
					RawResult          agent.Result `json:"rawResult"`
					Result             agent.Result `json:"result"`
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
					checkTruncatedToolResult(t, record.Result, exit)
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
	source := verboseToolSource + `const {writeFile}=await import("node:fs/promises");
await writeFile(` + string(encoded) + `,"ready");
setInterval(()=>{},1000);await new Promise(()=>{});`
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		result agent.Result
		err    error
	}
	done := make(chan outcome, 1)
	joined := false
	go func() {
		result, err := (agent.Runner{Executable: cliPath, Cwd: work}).Run(ctx, source)
		done <- outcome{result, err}
	}()
	defer func() {
		cancel()
		if joined {
			return
		}
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("runner cleanup did not finish")
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child blocked before writing its ready file")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	select {
	case got := <-done:
		joined = true
		if got.err != nil || got.result.Reason != "cancelled" || got.result.StdoutOmittedBytes <= 0 || got.result.StderrOmittedBytes <= 0 {
			t.Fatalf("err=%v reason=%s stdout omitted=%d stderr omitted=%d", got.err, got.result.Reason, got.result.StdoutOmittedBytes, got.result.StderrOmittedBytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not finish")
	}
}
