//go:build e2e

package e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type controlAgentSnapshot struct {
	AgentID, CurrentRunID, State, Name string
	QueueLength                        int
}

func awaitControlAgent(t *testing.T, ctx context.Context, work string, env map[string]string, predicate func(controlAgentSnapshot) bool) controlAgentSnapshot {
	t.Helper()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	var last cliResult
	for {
		last = runCLI(t, work, env, "agents", "--full")
		if last.exitCode == 0 {
			lines := strings.Split(strings.TrimSpace(last.stdout), "\n")
			for _, line := range lines[1:] {
				fields := strings.Fields(line)
				if len(fields) < 6 {
					continue
				}
				queueLength, err := strconv.Atoi(fields[4])
				if err != nil {
					continue
				}
				snapshot := controlAgentSnapshot{AgentID: fields[1], CurrentRunID: strings.TrimSuffix(fields[2], "-"), State: fields[3], QueueLength: queueLength, Name: strings.Join(fields[5:], " ")}
				if fields[2] == "-" {
					snapshot.CurrentRunID = ""
				}
				if predicate(snapshot) {
					return snapshot
				}
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for CLI control row: %v; last output: %+v", ctx.Err(), last)
		case <-ticker.C:
		}
	}
}

// controlObserveOutput permits checking acceptance while the provider is blocked.
type controlObserveOutput struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *controlObserveOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *controlObserveOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestAgentHumanControlCLIWhileEvalRuns(t *testing.T) {
	const steerInstruction = "add a correction\n\n**keep this plain**\n  preserve indentation"
	const queuedInstruction = "blocked task\n\n- **queue literally**"
	const reservedInstruction = "reserved task\n\n`keep reserved text plain`"
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
	cmd := exec.CommandContext(ctx, cliPath, "eval", "--observe", "--events="+eventPath, source)
	cmd.Dir, cmd.Env = dir, os.Environ()
	for key, value := range env {
		cmd.Env = envWith(cmd.Env, key, value)
	}
	var stdout controlObserveOutput
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() { cancel(); <-ctx.Done() }()
	var first agentChatRequest
	select {
	case first = <-firstStarted:
	case <-ctx.Done():
		t.Fatal("first provider request did not start")
	}
	snapshot := awaitControlAgent(t, ctx, dir, env, func(s controlAgentSnapshot) bool { return s.CurrentRunID != "" })
	if first.SessionID != snapshot.AgentID || first.SessionID == snapshot.CurrentRunID {
		t.Fatalf("session must identify the instance, not its task: %q %+v", first.SessionID, snapshot)
	}
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
	type humanCommand struct{ operation, instruction, terminalStatus string }
	humanCommands := map[string]humanCommand{}
	accept := func(operation, target, instruction, terminalStatus string) {
		t.Helper()
		receipt := command(operation, target, instruction)
		prefix := operation + " accepted: "
		id := strings.TrimSuffix(strings.TrimPrefix(receipt.stdout, prefix), "\n")
		if !strings.HasPrefix(receipt.stdout, prefix) || id == "" || strings.ContainsAny(id, " \n") {
			t.Fatalf("%s receipt: %+v", operation, receipt)
		}
		humanCommands[id] = humanCommand{operation, instruction, terminalStatus}
		// Assert before releasing the provider: reserved input must be visible
		// on acceptance, not just when it eventually reaches a model response.
		want := prefix + id + "\n" + instruction + "\n"
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for !strings.Contains(stdout.String(), want) {
			select {
			case <-ctx.Done():
				t.Fatalf("live observe missing %q:\n%s", want, stdout.String())
			case <-ticker.C:
			}
		}
	}
	accept("steer", fields[2], steerInstruction, "applied")
	close(release)
	var second agentChatRequest
	select {
	case second = <-corrected:
	case <-ctx.Done():
		t.Fatal("steer not applied")
	}
	var finalInput string
	if len(second.Messages) != 4 || json.Unmarshal(second.Messages[3].Content, &finalInput) != nil || finalInput != steerInstruction || len(second.Messages[2].ReasoningDetails) == 0 {
		t.Fatalf("steer history: %+v", second)
	}
	if second.SessionID != snapshot.AgentID {
		t.Fatalf("steer changed cache session: %q", second.SessionID)
	}
	awaitControlAgent(t, ctx, dir, env, func(s controlAgentSnapshot) bool { return s.State == "idle" && s.CurrentRunID == "" })
	accept("queue", fields[1], queuedInstruction, "cancelled")
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal("queued provider did not start")
	}
	busy := awaitControlAgent(t, ctx, dir, env, func(s controlAgentSnapshot) bool { return s.CurrentRunID != "" })
	if busy.AgentID != snapshot.AgentID || busy.CurrentRunID == snapshot.CurrentRunID {
		t.Fatalf("queued task must keep its instance and have a new run: %+v", busy)
	}
	accept("queue", snapshot.AgentID, reservedInstruction, "completed")
	awaitControlAgent(t, ctx, dir, env, func(s controlAgentSnapshot) bool { return s.QueueLength == 1 && s.CurrentRunID == busy.CurrentRunID })
	command("interrupt", "--run", busy.CurrentRunID)
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal("run interrupt left model request active")
	}
	awaitControlAgent(t, ctx, dir, env, func(s controlAgentSnapshot) bool { return s.State == "idle" && s.CurrentRunID == "" })
	select {
	case request := <-resumed:
		if request.SessionID != snapshot.AgentID {
			t.Fatalf("queue/interrupt changed the instance session: %q", request.SessionID)
		}
		var history []string
		for _, message := range request.Messages {
			var text string
			_ = json.Unmarshal(message.Content, &text)
			if message.Role != "system" {
				history = append(history, message.Role+":"+text)
			}
		}
		if strings.Join(history, "|") != "user:first task|assistant:candidate|user:"+steerInstruction+"|assistant:corrected answer|user:"+queuedInstruction+"|user:"+reservedInstruction {
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
	if calls.Load() != 4 {
		t.Fatalf("unexpected provider calls: %d", calls.Load())
	}
	data, err := os.ReadFile(eventPath)
	if err != nil {
		t.Fatal(err)
	}
	live := stdout.String()
	if stderr.Len() != 0 {
		t.Fatalf("observe stderr: %s", &stderr)
	}
	replayed := command("observe", eventPath)
	if replayed.stdout != live || replayed.stderr != "" {
		t.Fatalf("live/playback mismatch:\nlive:\n%s\nplayback: %+v", live, replayed)
	}
	var scriptStdout strings.Builder
	accepted, settled := map[string]bool{}, map[string]bool{}
	internalQueues := 0
	creationIDs := map[string]bool{}
	interruptIDs := map[string]bool{}
	interruptTargets := map[string]bool{}
	for _, line := range bytes.Split(data, []byte("\n")) {
		var event struct {
			Type        string   `json:"type"`
			Operation   string   `json:"operation"`
			Status      string   `json:"status"`
			Instruction *string  `json:"instruction"`
			Text        string   `json:"text"`
			ActivityID  string   `json:"activityId"`
			Target      string   `json:"target"`
			CommandID   string   `json:"commandId"`
			CausedBy    []string `json:"causedBy"`
			Origin      struct {
				Kind string `json:"kind"`
			} `json:"origin"`
		}
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Type == "stdout" {
			scriptStdout.WriteString(event.Text)
		}
		if strings.HasPrefix(event.Type, "command.") && (event.Operation == "queue" || event.Operation == "steer") {
			if event.Type == "command.accepted" {
				if event.Instruction == nil {
					t.Fatalf("accepted instruction missing: %s", line)
				}
				if event.Origin.Kind == "human" {
					want, ok := humanCommands[event.CommandID]
					if !ok || accepted[event.CommandID] || want.operation != event.Operation || want.instruction != *event.Instruction {
						t.Fatalf("human acceptance lost original instruction: %s", line)
					}
					accepted[event.CommandID] = true
				} else {
					internalQueues++
					if event.Operation != "queue" || *event.Instruction != "first task" || strings.Contains(live, event.CommandID) || strings.Contains(live, *event.Instruction) {
						t.Fatalf("initial/internal queue leaked into human view: %s\n%s", line, live)
					}
				}
			} else {
				if event.Instruction != nil {
					t.Fatalf("later command event repeated instruction: %s", line)
				}
				if want, ok := humanCommands[event.CommandID]; ok {
					statusLine := want.operation + " " + want.terminalStatus + ": " + event.CommandID + "\n"
					if event.Origin.Kind != "human" || event.Status != want.terminalStatus || !accepted[event.CommandID] || settled[event.CommandID] || strings.Count(live, statusLine) != 1 || !strings.Contains(live, statusLine+"\n[") {
						t.Fatalf("human command status must show ID only: %s\n%s", line, live)
					}
					settled[event.CommandID] = true
				}
			}
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
		switch event.Target {
		case "task":
			if event.CommandID == "" {
				t.Fatalf("task cancellation event lost commandId: %s", line)
			}
		case "agent":
			if event.CommandID != "" {
				t.Fatalf("agent stop event unexpectedly targets a command: %s", line)
			}
		default:
			t.Fatalf("unexpected human control target: %s", line)
		}
		interruptIDs[event.ActivityID] = true
		interruptTargets[event.Target] = true
	}
	if len(interruptIDs) != 2 || !interruptTargets["task"] || !interruptTargets["agent"] {
		t.Fatalf("human task-cancel/instance-stop records=%v targets=%v", interruptIDs, interruptTargets)
	}
	if len(accepted) != len(humanCommands) || len(settled) != len(humanCommands) || internalQueues != 1 {
		t.Fatalf("command lifecycle: accepted=%v settled=%v internalQueues=%d", accepted, settled, internalQueues)
	}
	for id, command := range humanCommands {
		if strings.Count(live, command.instruction) != 1 {
			t.Fatalf("instruction must appear once, unchanged, on acceptance (%s):\n%s", id, live)
		}
	}
	// --observe suppresses script stdout, but its recorded terminal snapshot
	// must retain the same lifecycle/result contract as the non-observe path.
	var result struct {
		State, Reason string
		LastResult    struct{ State, Answer string }
	}
	if json.Unmarshal([]byte(strings.TrimSpace(scriptStdout.String())), &result) != nil || result.State != "stopped" || result.Reason != "interrupted" || result.LastResult.State != "completed" || result.LastResult.Answer != reservedInstruction+" answer" {
		t.Fatalf("terminal snapshot: %s", scriptStdout.String())
	}
	if strings.Contains(live, `"lastResult"`) {
		t.Fatalf("script stdout leaked into human view:\n%s", live)
	}
	entries, err := os.ReadDir(env["NOE_AGENT_RUNTIME_DIR"])
	if err != nil || len(entries) != 0 {
		t.Fatalf("root/child registry cleanup: %v %v", entries, err)
	}
	if result := command("agents"); result.stdout != "No active agents.\n" {
		t.Fatalf("stale host: %+v", result)
	}
}
