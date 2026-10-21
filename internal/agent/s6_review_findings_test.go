package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

type reviewModels struct {
	list     func(context.Context, model.ModelQuery) ([]model.Model, error)
	generate func(context.Context, model.Request) (model.Response, error)
}

type reviewModelsWithProviderCheck struct{ reviewModels }

func (reviewModelsWithProviderCheck) HasProvider(name string) (bool, error) {
	return name == "fixture", nil
}

func (m reviewModels) ListModels(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
	if m.list != nil {
		return m.list(ctx, query)
	}
	return nil, nil
}

func (m reviewModels) Generate(ctx context.Context, request model.Request) (model.Response, error) {
	if m.generate != nil {
		return m.generate(ctx, request)
	}
	return model.Response{}, model.ErrUnsupported
}

func (reviewModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, model.ErrUnsupported
}

func (reviewModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, model.ErrUnsupported
}

func TestDirectModelCatalogUsesRequestContext(t *testing.T) {
	entered := make(chan struct{})
	models := reviewModels{list: func(ctx context.Context, _ model.ModelQuery) ([]model.Model, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	h := NewHost(Options{Models: models})
	op := h.NewOperation(context.Background())
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := op.Models().Generate(requestCtx, model.Request{Provider: "fixture", Model: "fixture-model"})
		result <- err
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("provider catalog was not queried")
	}
	cancelRequest()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Generate error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop the provider catalog query")
	}
	op.Close()
	h.Close()
}

func TestDirectSpawnCatalogLimitLookupUsesRequestContext(t *testing.T) {
	limitsEntered := make(chan struct{})
	models := reviewModelsWithProviderCheck{reviewModels{list: func(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
		if query.Provider != "" {
			close(limitsEntered)
			<-ctx.Done()
			return nil, ctx.Err()
		}
		return []model.Model{{Provider: "fixture", Model: "fixture-model"}}, nil
	}}}
	h := NewHost(Options{Models: models, Execute: func(context.Context, Execution) (Result, error) {
		return Result{ExitCode: 0, Reason: "completed"}, nil
	}})
	op := h.NewOperation(context.Background())
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	instruction := "work"
	result := make(chan error, 1)
	go func() {
		_, err := h.spawn(requestCtx, op, spawnRequest{
			Config:      taskConfig{Config: Config{Provider: "fixture", Model: "fixture-model", Name: "worker", Effort: "none"}},
			Instruction: &instruction,
		})
		result <- err
	}()
	select {
	case <-limitsEntered:
	case <-time.After(time.Second):
		t.Fatal("spawn did not reach its model-limit catalog query")
	}
	cancelRequest()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("spawn succeeded after its request was canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation did not stop the model-limit catalog query")
	}
	h.mu.Lock()
	instanceCount := len(h.instances)
	h.mu.Unlock()
	if instanceCount != 0 {
		t.Fatalf("spawn admitted %d instances after request cancellation", instanceCount)
	}
	op.Close()
	h.Close()
}

func TestCancelTaskRejectsAfterTerminalOutcomeCommit(t *testing.T) {
	stdoutEntered := make(chan struct{})
	releaseStdout := make(chan struct{})
	var releaseOnce sync.Once
	h := NewHost(Options{

		Models: reviewModels{list: func(context.Context, model.ModelQuery) ([]model.Model, error) {
			return []model.Model{{Provider: "fixture", Model: "fixture-model"}}, nil
		}},
		Execute: func(context.Context, Execution) (Result, error) {
			return Result{ExitCode: 0, Reason: "completed", Stdout: "complete"}, nil
		},
		Observe: func(event events.Event) {
			if event["type"] != "stdout" || event["source"] != "agent_task" {
				return
			}
			close(stdoutEntered)
			<-releaseStdout
		},
	})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	instruction := "work"
	spawned, err := h.spawn(context.Background(), op, spawnRequest{
		Config:      taskConfig{Config: Config{Provider: "fixture", Model: "fixture-model", Name: "worker", Effort: "none"}},
		Instruction: &instruction,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer func() { releaseOnce.Do(func() { close(releaseStdout) }) }()
	select {
	case <-stdoutEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("task did not reach the terminal-output observer")
	}
	if _, err := h.cancelTask(op, CancelTaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}); err == nil {
		t.Fatal("CancelTask accepted after the command outcome had been committed")
	}
	releaseOnce.Do(func() { close(releaseStdout) })
	outcome, err := h.waitTask(op, taskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}, context.Background())
	if err != nil {
		t.Fatalf("wait task: %v", err)
	}
	if outcome.State == "cancelled" {
		t.Fatalf("terminal outcome changed to cancelled after the observer ran: %+v", outcome)
	}
}

func TestCancelTaskRejectsCommittedFinalWhileFinishCleanupRuns(t *testing.T) {
	h := NewHost(Options{})
	owner := h.NewOperation(context.Background())
	defer h.Close()
	closed := make(chan struct{})
	close(closed)
	cmd := &command{
		id: "active", active: true, finalCommitted: true,
		accepted: closed, done: make(chan struct{}),
	}
	instance := &agentInstance{
		host: h, id: "agent", owner: owner, keepAlive: true, state: "running",
		commands: map[string]*command{cmd.id: cmd}, currentRunID: "finished-run", done: closed,
	}
	owner.instances[instance.id] = instance
	h.mu.Lock()
	h.instances[instance.id] = instance
	// finish has removed the run from Host.runs, but command cleanup is pending.
	h.mu.Unlock()
	if _, err := h.cancelTask(owner, CancelTaskRequest{AgentID: instance.id, CommandID: cmd.id}); err == nil {
		t.Fatal("CancelTask accepted after final commit while finish cleanup was pending")
	}
}

func TestHostCloseJoinsOperationAlreadyClosing(t *testing.T) {
	providerEntered := make(chan struct{})
	providerCanceled := make(chan struct{})
	releaseProvider := make(chan struct{})
	var releaseOnce sync.Once
	models := reviewModels{
		list: func(context.Context, model.ModelQuery) ([]model.Model, error) {
			return []model.Model{{Provider: "fixture", Model: "fixture-model"}}, nil
		},
		generate: func(ctx context.Context, _ model.Request) (model.Response, error) {
			close(providerEntered)
			<-ctx.Done()
			close(providerCanceled)
			<-releaseProvider
			return model.Response{}, ctx.Err()
		},
	}
	h := NewHost(Options{Models: models})
	defer func() { releaseOnce.Do(func() { close(releaseProvider) }) }()
	op := h.NewOperation(context.Background())
	generateDone := make(chan struct{})
	go func() {
		defer close(generateDone)
		_, _ = op.Models().Generate(context.Background(), model.Request{Provider: "fixture", Model: "fixture-model"})
	}()
	select {
	case <-providerEntered:
	case <-time.After(time.Second):
		t.Fatal("provider Generate was not entered")
	}
	opCloseDone := make(chan struct{})
	go func() {
		op.Close()
		close(opCloseDone)
	}()
	select {
	case <-providerCanceled:
	case <-time.After(time.Second):
		t.Fatal("Operation.Close did not cancel the provider request")
	}
	hostCloseDone := make(chan struct{})
	go func() {
		h.Close()
		close(hostCloseDone)
	}()
	select {
	case <-opCloseDone:
		t.Fatal("Operation.Close returned before provider cleanup was released")
	case <-hostCloseDone:
		t.Fatal("Host.Close missed the operation already being closed")
	case <-time.After(40 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(releaseProvider) })
	for name, done := range map[string]<-chan struct{}{"operation": opCloseDone, "host": hostCloseDone, "generate": generateDone} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not return after provider cleanup", name)
		}
	}
}
