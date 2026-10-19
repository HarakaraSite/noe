//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func awaitControlAgent(t *testing.T, ctx context.Context, dir string, predicate func(agent.AgentSnapshot) bool) agent.AgentSnapshot {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		hosts, err := agent.DiscoverControls(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, host := range hosts {
			for _, snapshot := range host.Listing.Agents {
				if predicate(snapshot) {
					return snapshot
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for control snapshot: %v", ctx.Err())
		case <-ticker.C:
		}
	}
}

func TestAgentHumanControlCLIWhileEvalRuns(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	firstStarted, corrected := make(chan agentChatRequest, 1), make(chan agentChatRequest, 1)
	resumed := make(chan agentChatRequest, 1)
	blocked, cancelled := make(chan struct{}), make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	server := agentAPIFixture(t, func(callCtx context.Context, req agentChatRequest) agentModelReply {
		switch calls.Add(1) {
		case 1:
			firstStarted <- req
			select {
			case <-release:
			case <-callCtx.Done():
				return agentModelReply{Status: 500}
			}
			return agentModelReply{Content: "candidate", Deltas: []map[string]any{{"reasoning_details": []any{map[string]any{"type": "reasoning.text", "text": "preserve this"}}}}}
		case 2:
			corrected <- req
			return agentModelReply{Content: "corrected answer"}
		case 3:
			close(blocked)
			<-callCtx.Done()
			close(cancelled)
			return agentModelReply{Status: 500}
		default:
			resumed <- req
			for index := len(req.Messages) - 1; index >= 0; index-- {
				if req.Messages[index].Role == "user" {
					var text string
					_ = json.Unmarshal(req.Messages[index].Content, &text)
					return agentModelReply{Content: text + " answer"}
				}
			}
			return agentModelReply{Status: 500}
		}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	dir := t.TempDir()
	source := `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",name:"human-control",effort:"auto",maxSteps:2},"first task",{keepAlive:true});console.log(JSON.stringify(result));`
	eventPath := filepath.Join(dir, "events.jsonl")
	cmd := exec.CommandContext(ctx, cliPath, "eval", "--events="+eventPath, source)
	cmd.Dir, cmd.Env = dir, os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); <-ctx.Done() }()
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatal("first provider request did not start")
	}
	snapshot := awaitControlAgent(t, ctx, env["NOE_AGENT_RUNTIME_DIR"], func(s agent.AgentSnapshot) bool { return s.CurrentRunID != "" })
	command := func(args ...string) cliResult {
		t.Helper()
		result := runCLI(t, dir, env, args...)
		if result.exitCode != 0 {
			t.Fatalf("control %v: %+v", args, result)
		}
		return result
	}
	listed := command("agents")
	rows := strings.Split(strings.TrimSpace(listed.stdout), "\n")
	if len(rows) != 2 {
		t.Fatalf("listing: %+v", listed)
	}
	fields := strings.Fields(rows[1])
	if len(fields) != 6 || fields[1] != snapshot.AgentID[:8] || fields[2] != snapshot.CurrentRunID[:8] {
		t.Fatalf("listing: %+v", listed)
	}
	if full := command("agents", "--full"); !strings.Contains(full.stdout, snapshot.AgentID) || !strings.Contains(full.stdout, snapshot.CurrentRunID) {
		t.Fatalf("full listing: %+v", full)
	}
	if result := command("steer", fields[2], "add a correction"); !strings.Contains(result.stdout, "steer accepted:") {
		t.Fatalf("steer receipt: %+v", result)
	}
	close(release)
	var second agentChatRequest
	select {
	case second = <-corrected:
	case <-ctx.Done():
		t.Fatal("steer not applied")
	}
	var finalInput string
	if len(second.Messages) != 4 || json.Unmarshal(second.Messages[3].Content, &finalInput) != nil || finalInput != "add a correction" || len(second.Messages[2].ReasoningDetails) == 0 {
		t.Fatalf("steer history: %+v", second)
	}
	if second.SessionID != snapshot.CurrentRunID {
		t.Fatalf("steer changed cache session: %q", second.SessionID)
	}
	awaitControlAgent(t, ctx, env["NOE_AGENT_RUNTIME_DIR"], func(s agent.AgentSnapshot) bool {
		return s.State == "idle" && s.LastResult != nil && s.LastResult.Answer == "corrected answer"
	})
	command("queue", fields[1], "blocked task")
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("queued provider did not start")
	}
	busy := awaitControlAgent(t, ctx, env["NOE_AGENT_RUNTIME_DIR"], func(s agent.AgentSnapshot) bool { return s.CurrentRunID != "" })
	command("queue", snapshot.AgentID, "reserved task")
	command("interrupt", "--run", busy.CurrentRunID)
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("run interrupt left model request active")
	}
	awaitControlAgent(t, ctx, env["NOE_AGENT_RUNTIME_DIR"], func(s agent.AgentSnapshot) bool {
		return s.State == "idle" && s.LastResult != nil && s.LastResult.Answer == "reserved task answer"
	})
	select {
	case request := <-resumed:
		var history []string
		for _, message := range request.Messages {
			var text string
			_ = json.Unmarshal(message.Content, &text)
			if message.Role != "system" {
				history = append(history, message.Role+":"+text)
			}
		}
		if strings.Join(history, "|") != "user:first task|assistant:candidate|user:add a correction|assistant:corrected answer|user:blocked task|user:reserved task" {
			t.Fatalf("human queue/interrupt lost the continuing conversation: %v", history)
		}
	case <-ctx.Done():
		t.Fatal("resumed provider request was not captured")
	}
	command("interrupt", "--agent", snapshot.AgentID)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("owner did not return normally: %v stdout=%s stderr=%s", err, &stdout, &stderr)
		}
	case <-ctx.Done():
		t.Fatal("owner did not finish after instance interrupt")
	}
	var result struct {
		State, Reason string
		LastResult    struct{ State, Answer string }
	}
	if json.Unmarshal(bytes.TrimSpace(stdout.Bytes()), &result) != nil || result.State != "stopped" || result.Reason != "interrupted" || result.LastResult.State != "completed" || result.LastResult.Answer != "reserved task answer" {
		t.Fatalf("terminal snapshot: %s", &stdout)
	}
	if calls.Load() != 4 {
		t.Fatalf("unexpected provider calls: %d", calls.Load())
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	creationIDs := map[string]bool{}
	interruptIDs := map[string]bool{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Type       string   `json:"type"`
			ActivityID string   `json:"activityId"`
			CausedBy   []string `json:"causedBy"`
			Origin     struct {
				Kind string `json:"kind"`
			} `json:"origin"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Type == "agent.started" || event.Type == "instance.started" {
			creationIDs[event.ActivityID] = true
		}
		if event.Type != "interrupt.accepted" {
			continue
		}
		if event.ActivityID == "" || interruptIDs[event.ActivityID] || creationIDs[event.ActivityID] || event.Origin.Kind != "human" || len(event.CausedBy) != 0 {
			t.Fatalf("human interrupt sender lost: %s", line)
		}
		interruptIDs[event.ActivityID] = true
	}
	if len(interruptIDs) != 2 {
		t.Fatalf("human run/instance interrupt records=%v", interruptIDs)
	}
	entries, err := os.ReadDir(env["NOE_AGENT_RUNTIME_DIR"])
	if err != nil || len(entries) != 0 {
		t.Fatalf("root/child registry cleanup: %v %v", entries, err)
	}
	if result := command("agents"); result.stdout != "No active agents.\n" {
		t.Fatalf("stale host: %+v", result)
	}
}
