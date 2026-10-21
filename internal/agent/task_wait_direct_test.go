package agent

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func newDirectTaskResultFixture(t *testing.T, answer string) (*Host, *Operation, *agentInstance, *command) {
	t.Helper()
	host := NewHost(Options{})
	t.Cleanup(host.Close)
	op := host.NewOperation(context.Background())
	t.Cleanup(op.Close)
	instance := &agentInstance{
		host: host, id: "agent", owner: op, done: make(chan struct{}),
		commands: make(map[string]*command),
	}
	cmd := &command{id: "task", done: make(chan struct{})}
	instance.commands[cmd.id] = cmd
	host.mu.Lock()
	host.instances[instance.id] = instance
	host.mu.Unlock()
	if answer != "" {
		host.mu.Lock()
		cmd.outcome = &taskOutcome{RunID: "run", CommandID: cmd.id, State: "completed", Answer: answer}
		host.mu.Unlock()
		close(cmd.done)
	}
	return host, op, instance, cmd
}

func TestDirectWaitTaskCancellationIsObservationalAndResultsAreRepeatable(t *testing.T) {
	host, op, _, cmd := newDirectTaskResultFixture(t, "")
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	if _, err := host.waitTask(op, taskRequest{AgentID: "agent", CommandID: cmd.id}, requestCtx); !errors.Is(err, errRequestCancelled) {
		t.Fatalf("cancelled wait returned %v, want errRequestCancelled", err)
	}
	if op.Context().Err() != nil || cmd.outcome != nil {
		t.Fatal("cancelling the observation changed the operation or task")
	}

	type result struct {
		outcome taskOutcome
		err     error
	}
	results := make(chan result, 3)
	var started sync.WaitGroup
	started.Add(2)
	for _, includeAnswer := range []*bool{nil, new(bool)} {
		go func(includeAnswer *bool) {
			started.Done()
			outcome, err := host.waitTask(op, taskRequest{AgentID: "agent", CommandID: cmd.id, IncludeAnswer: includeAnswer}, context.Background())
			results <- result{outcome: outcome, err: err}
		}(includeAnswer)
	}
	started.Wait()
	answer := "original result"
	host.mu.Lock()
	cmd.outcome = &taskOutcome{RunID: "run", CommandID: cmd.id, State: "completed", Answer: answer}
	host.mu.Unlock()
	close(cmd.done)

	var omitted int
	for range 2 {
		got := <-results
		if got.err != nil || got.outcome.Answer != answer || got.outcome.RunID != "run" || got.outcome.CommandID != cmd.id {
			t.Fatalf("waiter lost or changed the retained outcome: %+v", got)
		}
		if got.outcome.omitAnswer {
			omitted++
		}
	}
	if omitted != 1 {
		t.Fatalf("includeAnswer projection count = %d, want 1", omitted)
	}
	repeated, err := host.waitTask(op, taskRequest{AgentID: "agent", CommandID: cmd.id}, context.Background())
	if err != nil || repeated.Answer != answer || repeated.omitAnswer {
		t.Fatalf("repeated wait consumed the original result: %+v err=%v", repeated, err)
	}
}

func TestDirectReadTaskResultUsesUTF8ByteBoundariesAndRetainsAnswer(t *testing.T) {
	answer := "結論🙂\n" + strings.Repeat("根拠。", 12)
	host, op, _, _ := newDirectTaskResultFixture(t, answer)
	query := taskResultRequest{AgentID: "agent", CommandID: "task", MaxBytes: int64Pointer(1)}
	var assembled strings.Builder
	for {
		part, err := host.readTaskResult(op, query)
		if err != nil {
			t.Fatal(err)
		}
		assembled.WriteString(part.Text)
		if part.NextOffset <= query.Offset && !part.EOF {
			t.Fatalf("range did not advance: %+v", part)
		}
		query.Offset = part.NextOffset
		if part.EOF {
			break
		}
	}
	if assembled.String() != answer {
		t.Fatalf("ranged read changed the answer: got %q, want %q", assembled.String(), answer)
	}
	query.Offset = 1
	if _, err := host.readTaskResult(op, query); err == nil {
		t.Fatal("read accepted an offset inside a UTF-8 rune")
	}
	query.Offset = 0
	query.MaxBytes = nil
	full, err := host.readTaskResult(op, query)
	if err != nil || full.Text != answer || full.TotalBytes != int64(len(answer)) || !full.EOF {
		t.Fatalf("full retained result = %+v err=%v", full, err)
	}
}

func int64Pointer(value int64) *int64 { return &value }
