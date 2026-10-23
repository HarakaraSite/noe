package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

func TestDirectExecutorFailureClosesAdmissionAndSettlesAcceptedQueue(t *testing.T) {
	entered := make(chan struct{})
	releaseExecutor := make(chan struct{})
	failedEvent := make(chan struct{})
	releaseObserver := make(chan struct{})
	var releaseObserverOnce sync.Once
	defer releaseObserverOnce.Do(func() { close(releaseObserver) })
	host := NewHost(Options{
		Models: directToolModels{},
		Execute: func(ctx context.Context, _ Execution) (Result, error) {
			close(entered)
			select {
			case <-releaseExecutor:
				return Result{ExitCode: -1, Reason: "failed"}, errors.New("executor failed")
			case <-ctx.Done():
				return Result{ExitCode: -1, Reason: "cancelled"}, ctx.Err()
			}
		},
		Observe: func(event events.Event) {
			if event["type"] == "command.failed" {
				close(failedEvent)
				<-releaseObserver
			}
		},
	})
	t.Cleanup(host.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := host.NewOperation(ctx)
	t.Cleanup(op.Close)
	initial := "initial"
	var spawned spawnResponse
	response := lifecycleCall(t, op, "spawn_agent", spawnRequest{
		Config:      taskConfig{Config: Config{Provider: "fixture", Model: "model", Name: "managed", MaxTokens: 32, Effort: "none"}},
		Instruction: &initial,
		KeepAlive:   true,
	})
	if err := json.Unmarshal([]byte(response), &spawned); err != nil || spawned.AgentID == "" || spawned.CommandID == "" {
		t.Fatalf("spawn response %q: %v", response, err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("initial executor did not start")
	}
	for _, id := range []string{"queued-1", "queued-2"} {
		lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: spawned.AgentID, CommandID: id, Instruction: id})
	}
	close(releaseExecutor)
	select {
	case <-failedEvent:
	case <-ctx.Done():
		t.Fatal("executor failure event was not published")
	}
	if _, err := op.Call("queue_agent", fmt.Sprintf(`{"agentId":%q,"commandId":"too-late","instruction":"too late"}`, spawned.AgentID)); err == nil {
		t.Fatal("fatal executor failure admitted a new queued task")
	}
	host.mu.Lock()
	instance := host.instances[spawned.AgentID]
	if instance == nil {
		host.mu.Unlock()
		t.Fatal("registered instance disappeared before shutdown")
	}
	done := instance.done
	host.mu.Unlock()
	releaseObserverOnce.Do(func() { close(releaseObserver) })
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("instance did not finish settling the accepted queue")
	}
	initialResult, err := host.waitTask(op, taskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}, context.Background())
	if err != nil || initialResult.State != "failed" {
		t.Fatalf("initial outcome=%+v err=%v", initialResult, err)
	}
	for _, id := range []string{"queued-1", "queued-2"} {
		outcome, err := host.waitTask(op, taskRequest{AgentID: spawned.AgentID, CommandID: id}, context.Background())
		if err != nil || outcome.State != "cancelled" || outcome.RunID != "" {
			t.Fatalf("accepted reservation %q outcome=%+v err=%v", id, outcome, err)
		}
	}
}

func TestCommittedFinalSurvivesExecutorExitWithoutFinishReport(t *testing.T) {
	const answer = "committed before executor exit"
	observed := make(chan events.Event, 32)
	models := modelsWithChat(&fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		message, _ := json.Marshal(map[string]string{"role": "assistant", "content": answer})
		return provider.Response{Message: message, FinishReason: "stop"}, nil
	}})
	host := NewHost(Options{
		Models:  models,
		Observe: func(event events.Event) { observed <- event },
		Execute: func(ctx context.Context, execution Execution) (Result, error) {
			var config taskConfig
			if err := json.Unmarshal(execution.Config, &config); err != nil {
				return Result{}, err
			}
			startPayload, _ := json.Marshal(startRequest{Provider: config.Provider, Model: config.Model, Name: config.Name,
				MaxTokens: config.MaxTokens, Effort: config.Effort, MaxSteps: config.MaxSteps})
			startJSON, err := execution.Operation.CallContext(ctx, "start", string(startPayload))
			if err != nil {
				return Result{}, err
			}
			var started struct {
				RunID    string                `json:"runId"`
				Snapshot *conversationSnapshot `json:"snapshot"`
			}
			if err := json.Unmarshal([]byte(startJSON), &started); err != nil || started.RunID == "" || started.Snapshot == nil {
				return Result{}, fmt.Errorf("invalid start response %q: %v", startJSON, err)
			}
			var messages []json.RawMessage
			for _, entry := range started.Snapshot.Entries {
				messages = append(messages, entry.Messages...)
			}
			stepPayload, _ := json.Marshal(beginModelStepRequest{RunID: started.RunID, Messages: messages})
			stepJSON, err := execution.Operation.CallContext(ctx, "begin_model_step", string(stepPayload))
			if err != nil {
				return Result{}, err
			}
			var step beginModelStepResponse
			if err := json.Unmarshal([]byte(stepJSON), &step); err != nil || step.ModelStepID == "" {
				return Result{}, fmt.Errorf("invalid model step response %q: %v", stepJSON, err)
			}
			modelPayload, _ := json.Marshal(modelRequest{RunID: started.RunID, ModelStepID: step.ModelStepID,
				Model: config.Model, Messages: messages, MaxTokens: config.MaxTokens, Effort: config.Effort})
			modelJSON, err := execution.Operation.CallContext(ctx, "model", string(modelPayload))
			if err != nil {
				return Result{}, err
			}
			var response modelResponse
			if err := json.Unmarshal([]byte(modelJSON), &response); err != nil || len(response.Message) == 0 {
				return Result{}, fmt.Errorf("invalid model response %q: %v", modelJSON, err)
			}
			var final struct {
				Content string `json:"content"`
			}
			if err := json.Unmarshal(response.Message, &final); err != nil || final.Content != answer {
				return Result{}, fmt.Errorf("unexpected final response %s: %v", response.Message, err)
			}
			candidate := cloneConversationSnapshot(started.Snapshot)
			if err := candidate.appendRaw("block", []json.RawMessage{response.Message}); err != nil {
				return Result{}, err
			}
			commitPayload, _ := json.Marshal(commitFinalRequest{RunID: started.RunID, Answer: answer, Snapshot: candidate})
			commitJSON, err := execution.Operation.CallContext(ctx, "commit_final", string(commitPayload))
			if err != nil {
				return Result{}, err
			}
			var committed commitFinalResponse
			if err := json.Unmarshal([]byte(commitJSON), &committed); err != nil || !committed.Committed {
				return Result{}, fmt.Errorf("final answer was not committed: %s (%v)", commitJSON, err)
			}
			// Deliberately omit finish: an executor's clean exit is not an
			// authenticated task completion report.
			return Result{
				ExitCode: 0, Reason: "completed", Stderr: "captured task stderr",
				Diagnostics: []executionresult.Diagnostic{{
					Code: "execution_error", Stage: "execute", Message: "top-level task failure",
					Stack: "Error: top-level task failure\n    at main.js:1:1",
				}},
			}, nil
		},
	})
	t.Cleanup(host.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	op := host.NewOperation(ctx)
	t.Cleanup(op.Close)
	instruction := "commit, then omit finish"
	var started spawnResponse
	response := lifecycleCall(t, op, "spawn_agent", spawnRequest{
		Config:      taskConfig{Config: Config{Provider: "fixture", Model: "model", Name: "managed", MaxTokens: 128, Effort: "none"}},
		Instruction: &instruction,
	})
	if err := json.Unmarshal([]byte(response), &started); err != nil || started.AgentID == "" || started.CommandID == "" {
		t.Fatalf("spawn response %q: %v", response, err)
	}
	outcome, err := host.waitTask(op, taskRequest{AgentID: started.AgentID, CommandID: started.CommandID}, ctx)
	if err != nil || outcome.State != "failed" || outcome.Answer != answer || outcome.Error != "task process exited without an authenticated completion report" {
		t.Fatalf("committed final after missing finish: %+v err=%v", outcome, err)
	}
	finals, failed, completed, diagnosticEvents, stderrEvents := 0, 0, 0, 0, 0
	for len(observed) > 0 {
		event := <-observed
		switch event["type"] {
		case "agent.final":
			finals++
		case "agent.failed":
			failed++
		case "agent.completed":
			completed++
		case "execution.diagnostic":
			diagnosticEvents++
			if event["source"] != "agent_task" || event["runId"] != outcome.RunID {
				t.Errorf("task diagnostic event metadata=%#v", event)
			}
			diagnostics, ok := event["diagnostics"].([]executionresult.Diagnostic)
			if !ok || len(diagnostics) != 1 || diagnostics[0].Stack != "Error: top-level task failure\n    at main.js:1:1" {
				t.Errorf("task diagnostic event lost stack: %#v", event["diagnostics"])
			}
		case "stderr":
			if event["source"] == "agent_task" {
				stderrEvents++
				if event["text"] != "captured task stderr" {
					t.Errorf("task diagnostic was merged into stderr: %#v", event)
				}
			}
		}
	}
	if finals != 1 || failed != 1 || completed != 0 {
		t.Fatalf("final/failure/completion events=%d/%d/%d", finals, failed, completed)
	}
	if diagnosticEvents != 1 || stderrEvents != 1 {
		t.Fatalf("task diagnostic/stderr events=%d/%d; want one each", diagnosticEvents, stderrEvents)
	}
}

func TestTaskDiagnosticsAreObservedBeforeAndAfterFinishReport(t *testing.T) {
	for _, test := range []struct {
		name         string
		finishReport bool
	}{
		{name: "failure before report"},
		{name: "executor exit after report", finishReport: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			observed := make(chan events.Event, 64)
			models := modelsWithChat(&fixtureProvider{})
			host := NewHost(Options{
				Models:  models,
				Observe: func(event events.Event) { observed <- event },
				Execute: func(ctx context.Context, execution Execution) (Result, error) {
					var runID string
					if test.finishReport {
						var config taskConfig
						if err := json.Unmarshal(execution.Config, &config); err != nil {
							return Result{}, err
						}
						startPayload, _ := json.Marshal(startRequest{
							Provider: config.Provider, Model: config.Model, Name: config.Name,
							MaxTokens: config.MaxTokens, Effort: config.Effort,
						})
						startedJSON, err := execution.Operation.CallContext(ctx, "start", string(startPayload))
						if err != nil {
							return Result{}, err
						}
						var started struct {
							RunID string `json:"runId"`
						}
						if err := json.Unmarshal([]byte(startedJSON), &started); err != nil || started.RunID == "" {
							return Result{}, fmt.Errorf("invalid start response %q: %v", startedJSON, err)
						}
						runID = started.RunID
						finishPayload, _ := json.Marshal(finishRequest{RunID: runID, Status: "failed", Error: "reported task failure"})
						if _, err := execution.Operation.CallContext(ctx, "finish", string(finishPayload)); err != nil {
							return Result{}, err
						}
					}
					return Result{
						ExitCode: 1, Reason: "failed", Stderr: "captured task stderr",
						Diagnostics: []executionresult.Diagnostic{{
							Code: "execution_error", Stage: "execute", Message: "task VM failure",
							Stack: "Error: task VM failure\n    at task.mjs:4:2",
						}},
					}, nil
				},
			})
			t.Cleanup(host.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			op := host.NewOperation(ctx)
			t.Cleanup(op.Close)
			instruction := "run the fixture"
			var spawned spawnResponse
			response := lifecycleCall(t, op, "spawn_agent", spawnRequest{
				Config:      taskConfig{Config: Config{Provider: "fixture", Model: "model", Name: "diagnostic-worker", MaxTokens: 64, Effort: "none"}},
				Instruction: &instruction,
			})
			if err := json.Unmarshal([]byte(response), &spawned); err != nil || spawned.AgentID == "" || spawned.CommandID == "" {
				t.Fatalf("spawn response %q: %v", response, err)
			}
			outcome, err := host.waitTask(op, taskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}, ctx)
			if err != nil || outcome.State != "failed" {
				t.Fatalf("task outcome=%+v err=%v", outcome, err)
			}
			if test.finishReport && outcome.Error != "reported task failure" {
				t.Fatalf("finish report was not retained: %+v", outcome)
			}

			diagnosticEvents, stderrEvents := 0, 0
			for len(observed) > 0 {
				event := <-observed
				switch event["type"] {
				case "execution.diagnostic":
					diagnosticEvents++
					if event["agentId"] != spawned.AgentID || event["commandId"] != spawned.CommandID || event["source"] != "agent_task" {
						t.Errorf("task diagnostic event metadata=%#v", event)
					}
					if test.finishReport {
						if event["runId"] != outcome.RunID {
							t.Errorf("task diagnostic event run ID=%#v, want %q", event["runId"], outcome.RunID)
						}
					} else if _, exists := event["runId"]; exists {
						t.Errorf("task diagnostic event invented a run ID: %#v", event)
					}
					diagnostics, ok := event["diagnostics"].([]executionresult.Diagnostic)
					if !ok || len(diagnostics) != 1 || diagnostics[0].Stack != "Error: task VM failure\n    at task.mjs:4:2" {
						t.Errorf("task diagnostic event lost VM stack: %#v", event["diagnostics"])
					}
				case "stderr":
					if event["source"] == "agent_task" {
						stderrEvents++
						if event["text"] != "captured task stderr" {
							t.Errorf("diagnostic was merged into task stderr: %#v", event)
						}
					}
				}
			}
			if diagnosticEvents != 1 || stderrEvents != 1 {
				t.Fatalf("task diagnostic/stderr events=%d/%d; want one each", diagnosticEvents, stderrEvents)
			}
		})
	}
}
