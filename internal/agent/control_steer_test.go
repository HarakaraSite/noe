package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func controlCall(t *testing.T, op *Operation, method string, payload any) string {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := op.Call(method, string(encoded))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return result
}

func startControlRun(t *testing.T, op *Operation, maxSteps int) string {
	t.Helper()
	var started struct {
		RunID string `json:"runId"`
	}
	result := controlCall(t, op, "start", startRequest{
		Provider: "openrouter", Model: "fixture/model", Name: "control-test",
		MaxTokens: 128, Effort: "auto", MaxSteps: maxSteps,
	})
	if err := json.Unmarshal([]byte(result), &started); err != nil || started.RunID == "" {
		t.Fatalf("invalid start response %q: %v", result, err)
	}
	return started.RunID
}

func callControlModel(t *testing.T, op *Operation, runID string) (modelResponse, error) {
	t.Helper()
	message := json.RawMessage(`{"role":"user","content":"initial request"}`)
	encoded, err := json.Marshal(modelRequest{
		RunID: runID, Model: "fixture/model", Messages: []json.RawMessage{message},
		MaxTokens: 128, Effort: "auto",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := op.Call("model", string(encoded))
	if err != nil {
		return modelResponse{}, err
	}
	var response modelResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("decode model response %q: %v", result, err)
	}
	return response, nil
}

func TestSteerIsOrderedIdempotentAndDoesNotConsumeModelBudget(t *testing.T) {
	var observed []events.Event
	p := &fixtureProvider{}
	h := NewHost(Options{Provider: p, Observe: func(event events.Event) { observed = append(observed, event) }})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	other := h.NewOperation(context.Background())
	t.Cleanup(other.Close)
	runID := startControlRun(t, op, 1)

	for _, command := range []steerRequest{
		{RunID: runID, Instruction: "first steer", CommandID: "steer-1"},
		{RunID: runID, Instruction: "second steer", CommandID: "steer-2"},
	} {
		var accepted map[string]string
		if err := json.Unmarshal([]byte(controlCall(t, op, "steer_agent", command)), &accepted); err != nil || accepted["status"] != "accepted" {
			t.Fatalf("steer response %#v: %v", accepted, err)
		}
	}
	replay := controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "first steer", CommandID: "steer-1"})
	if !strings.Contains(replay, `"status":"accepted"`) {
		t.Fatalf("identical replay changed command state: %s", replay)
	}
	if _, err := other.Call("steer_agent", fmt.Sprintf(`{"runId":%q,"instruction":"foreign"}`, runID)); err == nil {
		t.Fatal("foreign scope steered a run")
	}
	if _, err := op.Call("steer_agent", fmt.Sprintf(`{"runId":%q,"instruction":"different","commandId":"steer-1"}`, runID)); err == nil {
		t.Fatal("command ID was reused with a different instruction")
	}

	response, err := callControlModel(t, op, runID)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Steers) != 2 || response.Steers[0].CommandID != "steer-1" || response.Steers[1].CommandID != "steer-2" {
		t.Fatalf("steer batch order: %+v", response.Steers)
	}
	if p.callCount() != 0 {
		t.Fatal("synthetic steer batch called the provider")
	}
	controlCall(t, op, "apply_steers", applySteersRequest{RunID: runID, CommandIDs: []string{"steer-1", "steer-2"}})
	for _, command := range []steerRequest{
		{RunID: runID, Instruction: "first steer", CommandID: "steer-1"},
		{RunID: runID, Instruction: "second steer", CommandID: "steer-2"},
	} {
		replay := controlCall(t, op, "steer_agent", command)
		if !strings.Contains(replay, `"status":"applied"`) {
			t.Fatalf("applied command replay changed state: %s", replay)
		}
	}
	response, err = callControlModel(t, op, runID)
	if err != nil || len(response.Steers) != 0 || p.callCount() != 1 {
		t.Fatalf("one provider call after steer batch: response=%+v calls=%d err=%v", response, p.callCount(), err)
	}

	controlCall(t, op, "commit_final", commitFinalRequest{RunID: runID, Answer: "fixture answer"})
	if _, err := op.Call("commit_final", fmt.Sprintf(`{"runId":%q,"answer":"fixture answer"}`, runID)); err != nil {
		t.Fatalf("idempotent final commit: %v", err)
	}
	if _, err := op.Call("steer_agent", fmt.Sprintf(`{"runId":%q,"instruction":"too late"}`, runID)); err == nil {
		t.Fatal("steer was accepted after final commit")
	}
	controlCall(t, op, "finish", finishRequest{RunID: runID, Status: "completed", Answer: "fixture answer"})
	if _, err := op.Call("steer_agent", fmt.Sprintf(`{"runId":%q,"instruction":"finished"}`, runID)); err == nil {
		t.Fatal("steer was accepted after run finish")
	}

	accepted, applied, finals := 0, 0, 0
	positions := make(map[string]map[string]int)
	for index, event := range observed {
		kind, _ := event["type"].(string)
		id, _ := event["commandId"].(string)
		if id != "" {
			if positions[id] == nil {
				positions[id] = make(map[string]int)
			}
			positions[id][kind] = index
		}
		switch kind {
		case "command.accepted":
			accepted++
		case "command.applied":
			applied++
		case "agent.final":
			finals++
		}
	}
	if accepted != 2 || applied != 2 || finals != 1 {
		t.Fatalf("event counts accepted/applied/final=%d/%d/%d; events=%#v", accepted, applied, finals, observed)
	}
	for _, id := range []string{"steer-1", "steer-2"} {
		if positions[id]["command.accepted"] >= positions[id]["command.applied"] {
			t.Fatalf("accepted event did not precede applied for %s: %#v", id, positions[id])
		}
	}
}

func TestFailedFinishCancelsAcceptedSteersAfterAcceptanceEvent(t *testing.T) {
	observed := make(chan events.Event, 32)
	h := NewHost(Options{Provider: &fixtureProvider{}, Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	t.Cleanup(op.Close)
	runID := startControlRun(t, op, 0)
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "not applied", CommandID: "cancel-me"})
	controlCall(t, op, "finish", finishRequest{RunID: runID, Status: "failed", Error: "fixture failure"})

	var order []string
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(order) < 2 {
		select {
		case event := <-observed:
			if event["commandId"] == "cancel-me" {
				order = append(order, event["type"].(string))
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for command lifecycle events: %v", order)
		}
	}
	if strings.Join(order, ",") != "command.accepted,command.cancelled" {
		t.Fatalf("command lifecycle order = %v", order)
	}
}

func TestOperationCloseCancelsAcceptedSteers(t *testing.T) {
	observed := make(chan events.Event, 32)
	h := NewHost(Options{Provider: &fixtureProvider{}, Observe: func(event events.Event) { observed <- event }})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	runID := startControlRun(t, op, 0)
	controlCall(t, op, "steer_agent", steerRequest{RunID: runID, Instruction: "cancel on owner close", CommandID: "owner-cancel"})
	op.Close()
	var order []string
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	for len(order) < 2 {
		select {
		case event := <-observed:
			if event["commandId"] == "owner-cancel" {
				order = append(order, event["type"].(string))
			}
		case <-deadline.C:
			t.Fatalf("timed out waiting for owner cancellation events: %v", order)
		}
	}
	if strings.Join(order, ",") != "command.accepted,command.cancelled" {
		t.Fatalf("owner cancellation order = %v", order)
	}
}
