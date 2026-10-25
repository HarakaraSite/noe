//go:build e2e

package e2e_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"testing"
)

func TestContextObservationRunAndEval(t *testing.T) {
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		if !strings.Contains(request.RuntimeInfo, "10000") {
			t.Errorf("budget omitted from notification: %s", request.RuntimeInfo)
		}
		return agentModelReply{Content: "fixture complete"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	source := `const am=await import("noe:agent"); await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",contextBudget:{inputTokens:10000}},"fixture task");`
	for _, mode := range []string{"run", "eval"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "entry.mjs")
			writeFixture(t, path, source)
			args := []string{mode, "--observe", "--events=" + filepath.Join(dir, "events.jsonl")}
			switch mode {
			case "run":
				args = append(args, path)
			case "eval":
				args = append(args, source)
			}
			result := runCLI(t, dir, env, args...)
			requireExit(t, result, 0)
			if strings.Count(result.stdout, "\ncontext:") != 1 || strings.Contains(result.stdout, "budget=") || strings.Contains(result.stdout, "previous=") {
				t.Fatalf("context not observed: %s\n%s", result.stdout, result.stderr)
			}
			data, err := os.ReadFile(filepath.Join(dir, "events.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			updates := 0
			for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
				var event struct {
					Type, Status string
					Context      struct {
						Budget   struct{ InputTokens int }
						Previous struct{ PromptTokens int }
					}
				}
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatal(err)
				}
				if event.Type == "context.updated" {
					updates++
					if event.Status != "completed" || event.Context.Budget.InputTokens != 10000 || event.Context.Previous.PromptTokens != 100 {
						t.Fatalf("context details missing from completed event: %s", line)
					}
				}
			}
			if updates != 1 {
				t.Fatalf("context updates=%d, want one", updates)
			}
		})
	}
}

func TestHookFailuresCLIWithoutEventFile(t *testing.T) {
	for _, mode := range []string{"run", "eval_observe"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			delivered := make(chan agent.HookFailure, 1)
			server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
				if calls.Add(1) != 1 {
					t.Error("hook failure caused another provider request")
				}
				var failures []agent.HookFailure
				for _, line := range strings.Split(req.RuntimeInfo, "\n") {
					if !strings.HasPrefix(line, "- {") {
						continue
					}
					var failure agent.HookFailure
					if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "- ")), &failure); err != nil {
						t.Error(err)
						continue
					}
					failures = append(failures, failure)
				}
				if len(failures) != 1 || failures[0].Phase != "beforeModelRequest" || failures[0].InvocationID == "" || failures[0].NotifiedRequestID != "" {
					t.Errorf("missing fresh before-model fact: %s", req.RuntimeInfo)
				} else {
					select {
					case delivered <- failures[0]:
					default:
					}
				}
				return agentModelReply{Content: "fixture complete"}
			})
			defer server.Close()
			env := agentCLIEnv(t, server.URL)
			dir := t.TempDir()
			for _, name := range []string{"main.mjs", "hooks.mjs"} {
				code, err := os.ReadFile(filepath.Join(repoRootPath, "tests", "fixtures", "hook-isolation", name))
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, filepath.Join(dir, name), string(code))
			}
			args := []string{"run", filepath.Join(dir, "main.mjs")}
			if mode == "eval_observe" {
				source, err := os.ReadFile(filepath.Join(dir, "main.mjs"))
				if err != nil {
					t.Fatal(err)
				}
				args = []string{"eval", "--observe", string(source)}
			}
			result := runCLI(t, dir, env, args...)
			requireExit(t, result, 0)
			data, err := os.ReadFile(filepath.Join(dir, "result.json"))
			if err != nil {
				t.Fatal(err)
			}
			var retained struct {
				Outcome  agent.TaskOutcome
				Metadata struct {
					State        string
					Answer       *string
					AnswerBytes  int
					HookFailures []agent.HookFailure
				}
				Answer string
			}
			if err := json.Unmarshal(data, &retained); err != nil {
				t.Fatal(err)
			}
			outcome := retained.Outcome
			if calls.Load() != 1 || outcome.State != "completed" || outcome.Answer != "fixture complete" || outcome.Error != "" || len(outcome.HookFailures) != 2 || retained.Answer != outcome.Answer {
				t.Fatalf("result=%s providerCalls=%d", data, calls.Load())
			}
			before, after := outcome.HookFailures[0], outcome.HookFailures[1]
			if before.Phase != "beforeModelRequest" || before.RequestID != "" || before.NotifiedRequestID == "" || before.NotifiedRequestID != after.RequestID || after.Phase != "afterModelResponse" || after.NotifiedRequestID != "" || after.Error != "final hook fixture failed" || !strings.Contains(before.Error, "missing-directory") {
				t.Fatalf("delivery/retention=%+v", outcome.HookFailures)
			}
			select {
			case notice := <-delivered:
				if notice.InvocationID != before.InvocationID || notice.ModelStepID != before.ModelStepID || notice.Module != before.Module || notice.Error != before.Error || notice.HookIndex != before.HookIndex {
					t.Fatalf("notice=%+v outcome=%+v", notice, before)
				}
			default:
				t.Fatal("provider received no hook notification")
			}
			metadata := retained.Metadata
			if metadata.State != "completed" || metadata.Answer != nil || metadata.AnswerBytes != len(outcome.Answer) || len(metadata.HookFailures) != 2 || metadata.HookFailures[0].NotifiedRequestID != before.NotifiedRequestID || metadata.HookFailures[1].InvocationID != after.InvocationID {
				t.Fatalf("answer-only projection lost hook records: %s", data)
			}
			if _, err := os.Stat(filepath.Join(dir, ".noe", "events")); !os.IsNotExist(err) {
				t.Fatalf("event directory unexpectedly created: %v", err)
			}
			if mode == "run" {
				if strings.TrimSpace(result.stdout) != string(data) {
					t.Fatalf("plain CLI result changed: stdout=%s artifact=%s", result.stdout, data)
				}
			} else {
				failed := strings.Index(result.stdout, "hook failed")
				completed := strings.LastIndex(result.stdout, "completed")
				if failed < 0 || completed < failed || strings.Contains(result.stdout, "task failed") {
					t.Fatalf("recovered hook display did not finish: %s", result.stdout)
				}
			}
		})
	}
}
