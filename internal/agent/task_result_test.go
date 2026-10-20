package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestTaskResultProjectionAndUTF8ReadsRetainOriginal(t *testing.T) {
	answer := "結論🙂\n" + strings.Repeat("根拠をここへ。", 400)
	started, release := make(chan struct{}), make(chan struct{})
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
		message, err := json.Marshal(map[string]string{"role": "assistant", "content": answer})
		return provider.Response{Message: message, FinishReason: "stop"}, err
	}}
	h := NewHost(Options{Provider: p, Executable: lifecycleBinary(t), Cwd: t.TempDir()})
	defer h.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	instruction := "return the report"
	var receipt spawnResponse
	if err := json.Unmarshal([]byte(lifecycleCall(t, op, "spawn_agent", spawnRequest{
		Config:      taskConfig{Config: Config{Provider: "openrouter", Model: "fixture/model", Name: "report", Effort: "auto"}},
		Instruction: &instruction,
	})), &receipt); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	query := taskResultRequest{AgentID: receipt.AgentID, CommandID: receipt.CommandID}
	if _, err := op.Call("read_task_result", mustJSON(t, query)); err == nil {
		t.Fatal("unfinished result was readable")
	}
	close(release)
	include := false
	checkProjection := func(raw string) {
		t.Helper()
		var view map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &view); err != nil {
			t.Fatal(err)
		}
		if _, present := view["answer"]; present || string(view["answerBytes"]) != mustJSON(t, len(answer)) || string(view["state"]) != `"completed"` {
			t.Fatalf("wrong answer projection: %s", raw)
		}
	}
	checkProjection(lifecycleCall(t, op, "wait_task", taskRequest{AgentID: receipt.AgentID, CommandID: receipt.CommandID, IncludeAnswer: &include}))
	// Waiting for stop also confirms the retained result survives instance cleanup.
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal([]byte(lifecycleCall(t, op, "wait_agent", instanceRequest{AgentID: receipt.AgentID, IncludeAnswer: &include})), &snapshot); err != nil {
		t.Fatal(err)
	}
	checkProjection(string(snapshot["lastResult"]))
	if err := json.Unmarshal([]byte(lifecycleCall(t, op, "observe_agent", instanceRequest{AgentID: receipt.AgentID, IncludeAnswer: &include})), &snapshot); err != nil {
		t.Fatal(err)
	}
	checkProjection(string(snapshot["lastResult"]))
	read := func(req taskResultRequest) taskResultSlice {
		t.Helper()
		var part taskResultSlice
		if err := json.Unmarshal([]byte(lifecycleCall(t, op, "read_task_result", req)), &part); err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(part.Text) || part.RunID == "" || part.CommandID != receipt.CommandID || part.State != "completed" || part.TotalBytes != int64(len(answer)) {
			t.Fatalf("bad slice metadata or text: %+v", part)
		}
		return part
	}
	first := read(query)
	if len(first.Text) > 4096 || first.EOF || first.NextOffset != int64(len(first.Text)) {
		t.Fatalf("default range: %+v", first)
	}
	// A one-byte limit still returns a complete character and makes progress.
	one := int64(1)
	query.MaxBytes = &one
	var reconstructed strings.Builder
	for {
		part := read(query)
		reconstructed.WriteString(part.Text)
		if part.EOF {
			break
		}
		if part.NextOffset <= query.Offset {
			t.Fatal("read made no progress")
		}
		query.Offset = part.NextOffset
		// Exercise small character boundaries, then finish the large report at once.
		if query.Offset >= 12 {
			query.MaxBytes = nil
		}
	}
	if reconstructed.String() != answer {
		t.Fatal("ranged reads did not reconstruct the original answer")
	}
	query.Offset = int64(len(answer))
	if end := read(query); !end.EOF || end.Text != "" || end.NextOffset != query.Offset {
		t.Fatalf("end of answer: %+v", end)
	}
	for _, offset := range []int64{-1, 1, int64(len(answer) + 1)} {
		query.Offset = offset
		if _, err := op.Call("read_task_result", mustJSON(t, query)); err == nil {
			t.Fatalf("invalid UTF-8 offset %d accepted", offset)
		}
	}
	query.Offset = 0
	zero := int64(0)
	query.MaxBytes = &zero
	if _, err := op.Call("read_task_result", mustJSON(t, query)); err == nil {
		t.Fatal("zero maxBytes accepted")
	}
	query.MaxBytes = nil
	other := h.NewOperation(ctx)
	defer other.Close()
	if _, err := other.Call("read_task_result", mustJSON(t, query)); err == nil {
		t.Fatal("foreign result was readable")
	}
	if full := taskWaitResult(t, op, receipt.AgentID, receipt.CommandID); full.Answer != answer {
		t.Fatal("reading or projecting changed the retained answer")
	}
	if full := lifecycleSnapshot(t, op, receipt.AgentID, false); full.LastResult == nil || full.LastResult.Answer != answer {
		t.Fatal("projection changed the stored snapshot result")
	}
	if p.callCount() != 1 {
		t.Fatalf("result reads made extra model calls: %d", p.callCount())
	}
}
