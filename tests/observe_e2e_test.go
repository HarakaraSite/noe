//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func observationRunID(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil && event["type"] == "agent.started" {
			return event["runId"].(string)
		}
	}
	t.Fatal("no agent.started record")
	return ""
}

func TestObserveRecordedRunThroughHumanCLIAndLogParser(t *testing.T) {
	work := t.TempDir()
	data, err := os.ReadFile(filepath.Join(repoRootPath, "internal", "observation", "testdata", "real-run.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(work, "events.jsonl")
	writeFixture(t, path, string(data))
	runID := observationRunID(t, path)
	human := runCLI(t, work, nil, "observe", "--run", runID, "events.jsonl")
	requireExit(t, human, 0)
	if !strings.Contains(human.stdout, "run_js succeeded") || !strings.Contains(human.stdout, "thinking: Done. Report in Japanese.") || !strings.Contains(human.stdout, "final answer") || !strings.Contains(human.stdout, "5050") {
		t.Fatalf("human output:\n%s", human.stdout)
	}
	encodedID, _ := json.Marshal(runID)
	script := `import {readFile} from "node:fs/promises";
import {analyze_log} from "noe:agent-log";
const text=await readFile("events.jsonl","utf8");
const first=analyze_log(text,{runId:` + string(encodedID) + `});
const second=analyze_log(text,{runId:` + string(encodedID) + `,cursor:first.cursor});
console.log(JSON.stringify({status:second.status,finalText:second.finalText,delta:second.events.length,lastToolResult:second.lastToolResult}));`
	writeFixture(t, filepath.Join(work, "nested", "read.js"), script)
	result := runCLI(t, work, map[string]string{"NOE_OPENROUTER_API_KEY_FILE": filepath.Join(work, "absent-key")}, "run", "nested/read.js")
	requireExit(t, result, 0)
	var snapshot struct {
		Status, FinalText string
		Delta             int
		LastToolResult    struct{ Success bool }
	}
	if err := json.Unmarshal([]byte(result.stdout), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "completed" || !strings.Contains(snapshot.FinalText, "5050") || snapshot.Delta != 0 || !snapshot.LastToolResult.Success {
		t.Fatalf("JS snapshot: %s", result.stdout)
	}
}

func TestObserveFailedAgentReturnsFailureInformation(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(context.Context, agentChatRequest) agentModelReply { return agentModelReply{Status: 401} })
	defer server.Close()
	writeFixture(t, filepath.Join(work, "main.js"), agentScript("failure", "{}"))
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	run := runCLI(t, work, env, "run", "--events=failed.jsonl", "main.js")
	// Accepted task failures are data; this script prints the failed snapshot
	// and exits normally. The recorded TaskRun must still be observed as failed.
	requireExit(t, run, 0)
	var result struct {
		State, Reason string
		LastResult    struct{ RunID, State, Error string }
	}
	if err := json.Unmarshal([]byte(run.stdout), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "stopped" || result.Reason != "task_failed" || result.LastResult.State != "failed" || !strings.Contains(result.LastResult.Error, "authentication") {
		t.Fatalf("failed task snapshot: %s", run.stdout)
	}
	runID := observationRunID(t, filepath.Join(work, "failed.jsonl"))
	if runID != result.LastResult.RunID {
		t.Fatalf("recorded run=%q returned run=%q", runID, result.LastResult.RunID)
	}
	query := runCLI(t, work, nil, "observe", "--json", "--run", runID, "--wait", "failed.jsonl")
	requireExit(t, query, 0)
	var snapshot struct{ Status, Error string }
	if err := json.Unmarshal([]byte(query.stdout), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.Status != "failed" || !strings.Contains(snapshot.Error, "authentication") {
		t.Fatalf("failure result: %s", query.stdout)
	}
}

func TestRunObserveDirectMatchesSavedLogRenderer(t *testing.T) {
	work := t.TempDir()
	const finalMarkdown = "結果は3です\n\n**成功**\n\n| 項目 | 結果 |\n| --- | --- |\n| 日本語 | OK |\n"
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		if len(req.Messages) == 2 {
			return agentModelReply{Deltas: []map[string]any{
				{"reasoning": "計算"},
				{"reasoning": "します\n\n次に確認します。\n"},
				{"tool_calls": []any{agentTool("call_direct-test", "// 足し算をする\nconsole.log(1+2);")}},
			}, FinishReason: "tool_calls"}
		}
		return agentModelReply{Content: finalMarkdown}
	})
	defer server.Close()
	writeFixture(t, filepath.Join(work, "main.js"), agentScript("direct", "{}"))
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	env["COLUMNS"] = "80"
	direct := runCLI(t, work, env, "run", "--observe", "--events=record.jsonl", "main.js")
	requireExit(t, direct, 0)
	if direct.stderr != "" || !strings.Contains(direct.stdout, "thinking: 計算します") || !strings.Contains(direct.stdout, "run_js succeeded") || !strings.Contains(direct.stdout, "final answer:\n結果は3です") {
		t.Fatalf("direct output: %+v", direct)
	}
	replayed := runCLI(t, work, nil, "observe", "record.jsonl")
	requireExit(t, replayed, 0)
	if direct.stdout != replayed.stdout {
		t.Fatalf("format mismatch\ndirect:\n%s\nreplayed:\n%s", direct.stdout, replayed.stdout)
	}
	data, err := os.ReadFile(filepath.Join(work, "record.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	started, updated := map[string]bool{}, map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Type, Status, RequestID string
		}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "model.started" {
			started[event.RequestID] = true
		}
		if event.Type == "context.updated" {
			if !started[event.RequestID] || updated[event.RequestID] || event.Status != "completed" {
				t.Fatalf("context update must occur once after its model response: %s", line)
			}
			updated[event.RequestID] = true
		}
	}
	if len(started) != 2 || len(updated) != 2 || strings.Count(direct.stdout, "\ncontext:") != 2 {
		t.Fatalf("model requests=%d context updates=%d:\n%s", len(started), len(updated), direct.stdout)
	}
	if strings.Count(direct.stdout, "\nthinking:") != 1 || !strings.Contains(direct.stdout, "thinking: 計算します\n\n  次に確認します。\n") {
		t.Fatalf("thinking fragments were not grouped:\n%s", direct.stdout)
	}
	if strings.Contains(direct.stdout, "\nstdout:") || strings.Contains(direct.stdout, "**成功**") {
		t.Fatalf("raw stdout leaked into human output:\n%s", direct.stdout)
	}
	if !strings.Contains(direct.stdout, "\n成功\n\n| 項目   | 結果 |\n| ------ | ---- |\n| 日本語 | OK   |\n") || strings.Contains(direct.stdout, "\x1b") {
		t.Fatalf("Markdown was not formatted as plain text:\n%s", direct.stdout)
	}
	runID := observationRunID(t, filepath.Join(work, "record.jsonl"))
	structured := runCLI(t, work, nil, "observe", "--json", "--run", runID, "record.jsonl")
	requireExit(t, structured, 0)
	var snapshot struct{ FinalText string }
	if err := json.Unmarshal([]byte(structured.stdout), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.FinalText != finalMarkdown {
		t.Fatalf("recorded Markdown changed: %q", snapshot.FinalText)
	}
}
