package agent_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestGoClientRetainsHookFailuresWithoutChangingAnswer(t *testing.T) {
	ctx := testContext(t)
	cwd := t.TempDir()
	path := filepath.Join(cwd, "hook.mjs")
	if err := os.WriteFile(path, []byte(`export function beforeModelRequest(){throw Error("before failure");}
export function afterModelResponse(){throw Error("after failure");}`), 0600); err != nil {
		t.Fatal(err)
	}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: cwd},
		Modules:     stdlib.Modules{FS: &stdlib.FileSystemOptions{ReadOnly: true}},
		NewLoader:   stdlib.FileLoader(nil),
	}
	models := new(fixtureModels)
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	config := taskConfig()
	hooks := agent.HookModules{path}
	config.Hooks = &hooks
	config.MaxSteps = 1
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("first"), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	ref := agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}
	first, err := client.WaitTask(ctx, ref)
	if err != nil || first.State != "completed" || first.Answer != "こんにちは世界" || first.Error != "" || len(first.HookFailures) != 2 {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	before, after := first.HookFailures[0], first.HookFailures[1]
	var publicFailure agent.HookFailure = after
	if before.Phase != "beforeModelRequest" || before.RequestID != "" || after.Phase != "afterModelResponse" || publicFailure.RequestID == "" || before.InvocationID == after.InvocationID || before.NotifiedRequestID != after.RequestID || after.NotifiedRequestID != "" {
		t.Fatalf("failures=%+v", first.HookFailures)
	}
	include := false
	ref.IncludeAnswer = &include
	metadata, err := client.WaitTask(ctx, ref)
	if err != nil || metadata.Answer != "" || metadata.AnswerBytes == nil || *metadata.AnswerBytes != len(first.Answer) || !reflect.DeepEqual(metadata.HookFailures, first.HookFailures) {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	// Returned slices belong to the consumer, not the command's retained state.
	metadata.HookFailures[0].Error = "changed by consumer"
	observed, err := client.Observe(ctx, agent.InstanceRequest{AgentID: spawned.AgentID, IncludeAnswer: &include})
	if err != nil || observed.LastResult == nil || observed.LastResult.Answer != "" || !reflect.DeepEqual(observed.LastResult.HookFailures, first.HookFailures) {
		t.Fatalf("observed=%+v err=%v", observed, err)
	}
	observed.LastResult.HookFailures[0].Error = "changed by observer"
	waited, err := client.WaitTasks(ctx, agent.WaitTasksRequest{Tasks: []agent.TaskReference{{AgentID: spawned.AgentID, CommandID: spawned.CommandID}}, IncludeAnswer: &include})
	if err != nil || len(waited.Completed) != 1 || !reflect.DeepEqual(waited.Completed[0].Outcome.HookFailures, first.HookFailures) {
		t.Fatalf("waited=%+v err=%v", waited, err)
	}
	waited.Completed[0].Outcome.HookFailures[0].Error = "changed by waiter"
	part, err := client.ReadTaskResult(ctx, agent.TaskResultRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil || part.Text != first.Answer {
		t.Fatalf("answer modified: %+v err=%v", part, err)
	}
	encoded, err := json.Marshal(metadata)
	if err != nil || !strings.Contains(string(encoded), "\"answerBytes\":21") || !strings.Contains(string(encoded), "\"hookFailures\":[") {
		t.Fatalf("marshal=%s err=%v", encoded, err)
	}
	queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: spawned.AgentID, Instruction: "second", CommandID: "second"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: queued.CommandID})
	if err != nil || second.State != "completed" || second.RunID == first.RunID || len(second.HookFailures) != 2 || second.HookFailures[0].InvocationID == first.HookFailures[0].InvocationID {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	ref.IncludeAnswer = nil
	retained, err := client.WaitTask(ctx, ref)
	if err != nil || !reflect.DeepEqual(retained, first) {
		t.Fatalf("retained first changed: %+v err=%v", retained, err)
	}
	models.mu.Lock()
	requests := append([]model.Request(nil), models.requests...)
	calls := len(requests)
	models.mu.Unlock()
	if second.HookFailures[0].NotifiedRequestID != second.HookFailures[1].RequestID || second.HookFailures[1].NotifiedRequestID != "" {
		t.Fatalf("second delivery state=%+v", second.HookFailures)
	}
	if calls == 2 {
		for index, outcome := range []agent.TaskOutcome{first, second} {
			var tail struct{ Role, Content string }
			messages := requests[index].Messages
			if err := json.Unmarshal(messages[len(messages)-1], &tail); err != nil || tail.Role != "user" || !strings.Contains(tail.Content, outcome.HookFailures[0].InvocationID) || strings.Contains(tail.Content, outcome.HookFailures[1].InvocationID) {
				t.Fatalf("notification tail=%+v err=%v", tail, err)
			}
			if index == 1 && (strings.Contains(tail.Content, first.HookFailures[0].InvocationID) || strings.Contains(tail.Content, first.HookFailures[1].InvocationID)) {
				t.Fatalf("old failures newly notified: %+v", tail)
			}
		}
		history, _ := json.Marshal(requests[1].Messages)
		if !strings.Contains(string(history), first.HookFailures[0].InvocationID) || strings.Contains(string(history), first.HookFailures[1].InvocationID) {
			t.Fatalf("sent prefix lost or old pending transferred: %s", history)
		}
	}
	if calls != 2 {
		t.Fatalf("hook failure forced extra model requests: %d", calls)
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: spawned.AgentID}); err != nil {
		t.Fatal(err)
	}
}
