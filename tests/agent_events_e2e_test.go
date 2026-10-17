//go:build e2e

package e2e_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAgentEventsParallelRootsChildrenAndToolResults(t *testing.T) {
	work := t.TempDir()
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		task := agentUser(req)
		if len(req.Messages) == 2 {
			switch task {
			case "A":
				return agentModelReply{Deltas: []map[string]any{
					{"reasoning": "考える"},
					{"reasoning_details": []any{map[string]any{"type": "reasoning.summary", "index": 0, "summary": "要約"}}},
					{"content": "子に依頼します", "tool_calls": []any{agentTool("delegate", "// 子に集計を依頼する\nconst child=await run_agent({name:'child'},'C');console.log(child.lastResult.answer);")}},
				}, FinishReason: "tool_calls"}
			case "B":
				return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("fails", "// 失敗を確認する\nconsole.error('tool failure');process.exit(7);")}}}, FinishReason: "tool_calls"}
			}
		}
		return agentModelReply{Content: task + "完了"}
	})
	defer server.Close()
	script := `import {run_agent} from "noe:agent";
const config={provider:'openrouter',model:'fixture/model',effort:'auto'};
console.log(JSON.stringify(await Promise.all([
run_agent({...config,name:'parent-A'},'A'),
run_agent({...config,name:'parent-B'},'B')
]).then(results=>results.map(result=>result.lastResult.answer))));`
	writeFixture(t, filepath.Join(work, "main.js"), script)
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	result := runCLI(t, work, env, "run", "--events=-", "main.js")
	requireExit(t, result, 0)
	if result.stderr != "" {
		t.Fatalf("unexpected diagnostics: %s", result.stderr)
	}
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("invalid event: %s", line)
		}
		records = append(records, record)
	}
	ids := map[string]string{}
	counts := map[string]int{}
	tools := map[string]map[string]any{}
	finals := map[string]string{}
	var executionID string
	for i, record := range records {
		if record["seq"] != float64(i+1) || record["version"] != float64(1) {
			t.Fatalf("envelope: %+v", record)
		}
		id, _ := record["executionId"].(string)
		if i == 0 {
			executionID = id
		}
		if id == "" || id != executionID {
			t.Fatalf("mixed execution IDs: %+v", record)
		}
		kind, _ := record["type"].(string)
		counts[kind]++
		if kind == "agent.started" {
			ids[record["name"].(string)] = record["runId"].(string)
		}
		if kind == "agent.final" {
			finals[record["name"].(string)] = record["text"].(string)
		}
		if kind == "run_js.finished" {
			tools[record["toolCallId"].(string)] = record
		}
		if kind == "run_js.started" && !strings.HasPrefix(record["text"].(string), "// ") {
			t.Fatalf("comment missing: %+v", record)
		}
		if kind == "run_js.started" && record["code"] == nil {
			t.Fatalf("generated source missing: %+v", record)
		}
		if kind == "thinking" || kind == "thinking_summary" || kind == "message_delta" {
			if record["requestId"] == nil || record["runId"] == nil {
				t.Fatalf("uncorrelated delta: %+v", record)
			}
		}
	}
	if len(ids) != 3 || ids["parent-A"] == ids["parent-B"] || ids["child"] == ids["parent-A"] {
		t.Fatalf("IDs: %+v", ids)
	}
	for _, record := range records {
		if record["runId"] == ids["child"] && record["parentRunId"] != ids["parent-A"] {
			t.Fatalf("child parent ID: %+v", record)
		}
	}
	if finals["parent-A"] != "A完了" || finals["parent-B"] != "B完了" || finals["child"] != "C完了" {
		t.Fatalf("finals: %+v", finals)
	}
	if counts["thinking"] != 1 || counts["thinking_summary"] != 1 || counts["agent.message"] != 1 || counts["agent.completed"] != 3 {
		t.Fatalf("events: %+v", counts)
	}
	if tools["delegate"]["success"] != true || tools["fails"]["success"] != false || tools["fails"]["exitCode"] != float64(7) {
		t.Fatalf("tools: %+v", tools)
	}
	if strings.Contains(result.stdout, "agent-fixture-secret") {
		t.Fatal("credentials leaked into events")
	}
}

func TestAgentEventsThinkingArrivesBeforeResponseFinishes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"live\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning\":\"live thinking\"},\"finish_reason\":null}]}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		fmt.Fprint(w, "data: {\"id\":\"live\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"done\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	defer finish()
	work := t.TempDir()
	writeFixture(t, filepath.Join(work, "main.js"), agentScript("live", "{}"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cliPath, "run", "--events=-", "main.js")
	cmd.Dir = work
	cmd.Env = os.Environ()
	for key, value := range agentCLIEnv(t, server.URL) {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	cmd.Env = envWith(cmd.Env, "NOE_AGENT_TRACE", "")
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(pipe)
	seenThinking, seenFinal := false, false
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Error(err)
			continue
		}
		if event["type"] == "thinking" && event["text"] == "live thinking" {
			seenThinking = true
			finish()
		}
		if event["type"] == "agent.final" {
			seenFinal = true
		}
	}
	if err := scanner.Err(); err != nil {
		t.Error(err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("command failed: %v; %s", err, &diagnostic)
	}
	if !seenThinking || !seenFinal {
		t.Fatalf("thinking=%v final=%v", seenThinking, seenFinal)
	}
}
