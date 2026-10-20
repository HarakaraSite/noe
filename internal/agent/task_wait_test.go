package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func taskWaitResult(t *testing.T, op *Operation, agentID, commandID string) taskOutcome {
	t.Helper()
	var outcome taskOutcome
	text := lifecycleCall(t, op, "wait_task", taskRequest{AgentID: agentID, CommandID: commandID})
	if err := json.Unmarshal([]byte(text), &outcome); err != nil {
		t.Fatal(err)
	}
	return outcome
}

func TestWaitTaskKeepsResultsAcrossLaterTasksAndMultipleWaiters(t *testing.T) {
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	releaseFirst, releaseSecond := make(chan struct{}), make(chan struct{})
	p := &fixtureProvider{gen: func(ctx context.Context, req provider.Request) (provider.Response, error) {
		var instruction string
		for index := len(req.Messages) - 1; index >= 0; index-- {
			var message struct{ Role, Content string }
			if json.Unmarshal(req.Messages[index], &message) == nil && message.Role == "user" && !strings.HasPrefix(message.Content, "NOE_CONTEXT_STATUS\n") {
				instruction = message.Content
				break
			}
		}
		var release chan struct{}
		switch instruction {
		case "first":
			close(firstStarted)
			release = releaseFirst
		case "second":
			close(secondStarted)
			release = releaseSecond
		case "fail":
			return provider.Response{}, errors.New("fixture failure")
		}
		select {
		case <-release:
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
		body, err := json.Marshal(map[string]string{"role": "assistant", "content": instruction})
		return provider.Response{Message: body, FinishReason: "stop"}, err
	}}
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	text := "first"
	var first spawnResponse
	if err := json.Unmarshal([]byte(lifecycleCall(t, op, "spawn_agent", spawnRequest{
		Config:      taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "wait-worker", Effort: "auto"}},
		Instruction: &text, KeepAlive: true,
	})), &first); err != nil || first.CommandID == "" {
		t.Fatalf("initial task receipt: %+v (%v)", first, err)
	}
	select {
	case <-firstStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	type reply struct {
		text string
		err  error
	}
	startWait := func() <-chan reply {
		done := make(chan reply, 1)
		go func() {
			result, err := op.Call("wait_task", fmt.Sprintf(`{"agentId":%q,"commandId":%q}`, first.AgentID, first.CommandID))
			done <- reply{result, err}
		}()
		return done
	}
	left, right := startWait(), startWait()
	for _, instruction := range []string{"second", "fail"} {
		lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: first.AgentID, CommandID: instruction, Instruction: instruction})
	}
	close(releaseFirst)
	select {
	case <-secondStarted:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for _, waiter := range []<-chan reply{left, right} {
		select {
		case response := <-waiter:
			var outcome taskOutcome
			if response.err != nil || json.Unmarshal([]byte(response.text), &outcome) != nil || outcome.Answer != "first" || outcome.State != "completed" || outcome.CommandID != first.CommandID || outcome.RunID == "" {
				t.Fatalf("waiter returned wrong result: %+v", response)
			}
		case <-ctx.Done():
			t.Fatal("first task wait was blocked by the second task")
		}
	}
	close(releaseSecond)
	if got := taskWaitResult(t, op, first.AgentID, "second"); got.State != "completed" || got.Answer != "second" {
		t.Fatalf("second result: %+v", got)
	}
	if got := taskWaitResult(t, op, first.AgentID, "fail"); got.State != "failed" || got.Error == "" {
		t.Fatalf("task failure must be returned as data: %+v", got)
	}
	if got := taskWaitResult(t, op, first.AgentID, first.CommandID); got.Answer != "first" {
		t.Fatalf("earlier result overwritten or consumed: %+v", got)
	}
	other := h.NewOperation(ctx)
	defer other.Close()
	for _, request := range []struct {
		op      *Operation
		payload string
	}{
		{op, fmt.Sprintf(`{"agentId":%q,"commandId":"missing"}`, first.AgentID)},
		{op, `{"agentId":"missing","commandId":"missing"}`},
		{op, fmt.Sprintf(`{"agentId":%q}`, first.AgentID)},
		{other, fmt.Sprintf(`{"agentId":%q,"commandId":%q}`, first.AgentID, first.CommandID)},
	} {
		if _, err := request.op.Call("wait_task", request.payload); err == nil {
			t.Fatalf("invalid or foreign wait accepted: %s", request.payload)
		}
	}
	if p.callCount() != 3 {
		t.Fatalf("waiting invoked extra model calls: %d", p.callCount())
	}
}

func TestWaitTaskRequestCancellationAndQueuedTaskCancellation(t *testing.T) {
	started := make(chan context.Context, 1)
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		started <- ctx
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	id := spawnLifecycle(t, op, nil, true)
	for _, instruction := range []string{"busy", "pending"} {
		lifecycleCall(t, op, "queue_agent", queueRequest{AgentID: id, CommandID: instruction, Instruction: instruction})
	}
	var modelContext context.Context
	select {
	case modelContext = <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waitContext, stopWait := context.WithCancel(ctx)
	stopWait()
	if _, err := h.waitTask(op, taskRequest{AgentID: id, CommandID: "busy"}, waitContext); !errors.Is(err, errRequestCancelled) {
		t.Fatalf("wait cancellation: %v", err)
	}
	if modelContext.Err() != nil {
		t.Fatal("cancelling only the wait cancelled the task")
	}
	lifecycleCall(t, op, "interrupt_agent", interruptRequest{AgentID: id})
	busy := taskWaitResult(t, op, id, "busy")
	pending := taskWaitResult(t, op, id, "pending")
	if busy.State != "cancelled" || busy.RunID == "" {
		t.Fatalf("active task cancellation: %+v", busy)
	}
	if pending.State != "cancelled" || pending.RunID != "" || pending.CommandID != "pending" {
		t.Fatalf("unstarted task cancellation: %+v", pending)
	}
	lifecycleSnapshot(t, op, id, true)
	if again := taskWaitResult(t, op, id, "pending"); again != pending {
		t.Fatalf("cancelled result was not retained: %+v", again)
	}
}
