package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type fixtureModels struct {
	mu       sync.Mutex
	requests []model.Request
	generate func(context.Context, model.Request) (model.Response, error)
}

func (f *fixtureModels) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return nil, nil
}
func (f *fixtureModels) Generate(ctx context.Context, q model.Request) (model.Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, q)
	f.mu.Unlock()
	if f.generate != nil {
		return f.generate(ctx, q)
	}
	return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"こんにちは世界"}`), FinishReason: "stop", Usage: json.RawMessage(`{"prompt_tokens":11,"completion_tokens":3,"total_tokens":14}`)}, nil
}
func (*fixtureModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, model.ErrUnsupported
}
func (*fixtureModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, model.ErrUnsupported
}
func taskConfig() agent.TaskConfig {
	return agent.TaskConfig{Config: agent.AgentConfig{Provider: "fixture", Model: "fixture-model", Name: "fixture", Effort: "none"}, MaxSteps: 3}
}
func textPointer(s string) *string { return &s }
func testContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestGoHostRunsQueueAndRetainsSessionWithoutEntryRuntime(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	var initializerOutputs []string
	var initMu sync.Mutex
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}, Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		if !b.Worker() {
			n, err := b.Environment().Stdout.Write([]byte("initializer output\n"))
			if err != nil || n == 0 {
				t.Errorf("capture n=%d err=%v", n, err)
			}
			initMu.Lock()
			initializerOutputs = append(initializerOutputs, b.Environment().Cwd)
			initMu.Unlock()
		}
		return nil, nil
	}}
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
	config.CacheKey = textPointer("explicit-queue-cache")
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("first"), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil || first.State != "completed" || first.Answer != "こんにちは世界" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	includeAnswer := false
	metadata, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID, IncludeAnswer: &includeAnswer})
	if err != nil || metadata.Answer != "" || metadata.AnswerBytes == nil || *metadata.AnswerBytes != len(first.Answer) {
		t.Fatalf("metadata=%+v err=%v", metadata, err)
	}
	var reconstructed strings.Builder
	offset, maxBytes := int64(0), int64(4)
	for {
		part, err := client.ReadTaskResult(ctx, agent.TaskResultRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID, Offset: offset, MaxBytes: &maxBytes})
		if err != nil {
			t.Fatal(err)
		}
		reconstructed.WriteString(part.Text)
		if part.EOF {
			break
		}
		if part.NextOffset <= offset {
			t.Fatal("result read did not advance")
		}
		offset = part.NextOffset
	}
	if reconstructed.String() != first.Answer {
		t.Fatalf("retained answer=%q", reconstructed.String())
	}
	queueRequest := agent.QueueRequest{AgentID: spawned.AgentID, Instruction: "second", CommandID: "second-task"}
	queued, err := client.Queue(ctx, queueRequest)
	if err != nil {
		t.Fatal(err)
	}
	if repeated, err := client.Queue(ctx, queueRequest); err != nil || repeated.CommandID != queued.CommandID {
		t.Fatalf("repeated queue=%+v err=%v", repeated, err)
	}
	if _, err := client.Queue(ctx, agent.QueueRequest{AgentID: spawned.AgentID, Instruction: "different", CommandID: queued.CommandID}); err == nil {
		t.Fatal("queue accepted a reused command ID for a different instruction")
	}
	second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: queued.CommandID})
	if err != nil || second.State != "completed" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if second.RunID == first.RunID {
		t.Fatal("queue reused task run ID")
	}
	if repeated, err := client.Queue(ctx, queueRequest); err != nil || repeated.CommandID != queued.CommandID || repeated.Status != "completed" {
		t.Fatalf("completed queue replay=%+v err=%v", repeated, err)
	}
	models.mu.Lock()
	requests := append([]model.Request(nil), models.requests...)
	models.mu.Unlock()
	if len(requests) != 2 || requests[0].SessionID == "" || requests[0].SessionID != requests[1].SessionID || requests[0].RunID == requests[1].RunID {
		t.Fatalf("sessions=%+v", requests)
	}
	if requests[0].CacheKey != *config.CacheKey || requests[1].CacheKey != *config.CacheKey {
		t.Fatalf("explicit cache key changed across queue: %+v", requests)
	}
	if profile.Environment.Stdout != nil || profile.Environment.Stdin != nil {
		t.Fatal("Host mutated template stdio")
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: spawned.AgentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Wait(ctx, agent.InstanceRequest{AgentID: spawned.AgentID}); err != nil {
		t.Fatal(err)
	}
	initMu.Lock()
	count := len(initializerOutputs)
	initMu.Unlock()
	if count != 2 {
		t.Fatalf("initializer calls=%d", count)
	}
}

func TestGoHostMissingRuntimeRejectsBeforeModelAndReservedStreams(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if _, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("task")}); err == nil {
		t.Fatal("spawn without Runtime succeeded")
	}
	if len(models.requests) != 0 {
		t.Fatal("rejected spawn reached model")
	}
	profile := stdlib.Profile{Environment: stdlib.Environment{Stdout: new(strings.Builder)}}
	if host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile}); err == nil {
		host.Close()
		t.Fatal("reserved Host stream accepted")
	}
}

func TestBindingCloseBorrowsHostAndWaitCancellationIsObservational(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	profile := stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	root, err := stdlib.NewRuntime(ctx, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		binding, err := agent.Attach(ctx, b, host)
		if err != nil {
			return nil, err
		}
		return binding.Close, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := root.Run(stdlib.Source{Name: "[root]", Virtual: true, Module: true, Code: `import {list_models} from "noe:agent"; await list_models();`}); err != nil {
		t.Fatal(err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := client.Wait(waitCtx, agent.InstanceRequest{AgentID: spawned.AgentID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait error=%v", err)
	}
	snapshot, err := client.Observe(ctx, agent.InstanceRequest{AgentID: spawned.AgentID})
	if err != nil || snapshot.State == "stopped" {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: spawned.AgentID}); err != nil {
		t.Fatal(err)
	}
}

func TestGoClientCancelsPendingAndRunningTaskButKeepsOtherQueue(t *testing.T) {
	ctx := testContext(t)
	entered := make(chan struct{})
	var mu sync.Mutex
	calls := 0
	models := &fixtureModels{generate: func(ctx context.Context, q model.Request) (model.Response, error) {
		mu.Lock()
		calls++
		index := calls
		mu.Unlock()
		if index == 1 {
			close(entered)
			<-ctx.Done()
			return model.Response{}, ctx.Err()
		}
		return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"remaining task"}`), FinishReason: "stop"}, nil
	}}
	profile := stdlib.Profile{}
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
	first, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("running"), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pending, err := client.Queue(ctx, agent.QueueRequest{AgentID: first.AgentID, Instruction: "cancel before model"})
	if err != nil {
		t.Fatal(err)
	}
	remaining, err := client.Queue(ctx, agent.QueueRequest{AgentID: first.AgentID, Instruction: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{pending.CommandID, first.CommandID} {
		if _, err := client.CancelTask(ctx, agent.CancelTaskRequest{AgentID: first.AgentID, CommandID: id}); err != nil {
			t.Fatal(err)
		}
		outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: first.AgentID, CommandID: id})
		if err != nil || outcome.State != "cancelled" {
			t.Fatalf("cancelled=%+v err=%v", outcome, err)
		}
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: first.AgentID, CommandID: remaining.CommandID})
	if err != nil || outcome.State != "completed" || outcome.Answer != "remaining task" {
		t.Fatalf("remaining=%+v err=%v", outcome, err)
	}
	mu.Lock()
	count := calls
	mu.Unlock()
	if count != 2 {
		t.Fatalf("model calls=%d", count)
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: first.AgentID}); err != nil {
		t.Fatal(err)
	}
}

func TestGoHostBareHooksResolvePerTaskThroughActualLoader(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	var mu sync.Mutex
	factories := 0
	var seen []int
	profile := stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		return nil, b.Runtime().Set("hookSeen", func(value int) { mu.Lock(); seen = append(seen, value); mu.Unlock() })
	}, NewLoader: func(ctx context.Context, origin stdlib.Origin) (quickjs.ModuleLoader, error) {
		mu.Lock()
		factories++
		generation := factories
		mu.Unlock()
		canonical := fmt.Sprintf("memory:/hooks/%d/main.mjs", generation)
		return func(specifier, referrer string) (string, string, error) {
			switch specifier {
			case "fixture:hook":
				return `import {value} from "./part.mjs"; export function beforeModelRequest(){hookSeen(value);}`, canonical, nil
			case "./part.mjs":
				if referrer != canonical {
					return "", "", fmt.Errorf("relative referrer=%q", referrer)
				}
				return fmt.Sprintf("export const value=%d", generation), fmt.Sprintf("memory:/hooks/%d/part.mjs", generation), nil
			}
			return "", "", fmt.Errorf("unexpected import %q from %q", specifier, referrer)
		}, nil
	}}
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
	hooks := agent.HookModules{"fixture:hook"}
	config.Hooks = &hooks
	first, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("first"), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		commandID := first.CommandID
		if index == 1 {
			queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: first.AgentID, Instruction: "second"})
			if err != nil {
				t.Fatal(err)
			}
			commandID = queued.CommandID
		}
		outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: first.AgentID, CommandID: commandID})
		if err != nil || outcome.State != "completed" {
			t.Fatalf("outcome=%+v err=%v", outcome, err)
		}
	}
	mu.Lock()
	count := factories
	values := append([]int(nil), seen...)
	mu.Unlock()
	if count != 2 || len(values) != 2 || values[0] != 1 || values[1] != 2 {
		t.Fatalf("factories=%d hooks=%v", count, values)
	}
	if _, err := client.StopAgent(ctx, agent.StopAgentRequest{AgentID: first.AgentID}); err != nil {
		t.Fatal(err)
	}
}

func TestGoScopedModelCloseCancelsAndJoinsBorrowedService(t *testing.T) {
	ctx := testContext(t)
	entered := make(chan struct{})
	cancelled := make(chan struct{})
	release := make(chan struct{})
	models := &fixtureModels{generate: func(ctx context.Context, q model.Request) (model.Response, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		<-release
		return model.Response{}, ctx.Err()
	}}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	generated := make(chan error, 1)
	go func() {
		_, err := client.Models().Generate(ctx, model.Request{Provider: "fixture", Model: "fixture-model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"hello"}`)}})
		generated <- err
	}()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	closed := make(chan struct{})
	go func() { client.Close(); close(closed) }()
	select {
	case <-cancelled:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	select {
	case <-closed:
		close(release)
		t.Fatal("Client.Close returned before provider work joined")
	default:
	}
	close(release)
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := <-generated; !errors.Is(err, context.Canceled) {
		t.Fatalf("model error=%v", err)
	}
	// Reusing the borrowed service through another client is still valid.
	another, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer another.Close()
	if _, err := another.Models().ListModels(ctx, model.ModelQuery{}); err != nil {
		t.Fatal(err)
	}
}

func TestHostAdminAndIndependentClientOwnership(t *testing.T) {
	ctx := testContext(t)
	profile := stdlib.Profile{}
	models := new(fixtureModels)
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	first, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	other, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	a, err := first.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := other.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := other.Observe(ctx, agent.InstanceRequest{AgentID: a.AgentID}); err == nil {
		t.Fatal("Client observed another Client's agent")
	}
	if _, err := other.StopAgent(ctx, agent.StopAgentRequest{AgentID: a.AgentID}); err == nil {
		t.Fatal("Client stopped another Client's agent")
	}
	agents, err := host.ListAgents(ctx)
	if err != nil || len(agents) != 2 {
		t.Fatalf("admin listing=%+v err=%v", agents, err)
	}
	queued, err := host.Queue(ctx, agent.QueueRequest{AgentID: a.AgentID, Instruction: "admin queue"})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := first.WaitTask(ctx, agent.TaskRequest{AgentID: a.AgentID, CommandID: queued.CommandID})
	if err != nil || outcome.State != "completed" {
		t.Fatalf("admin queue=%+v err=%v", outcome, err)
	}
	first.Close()
	snapshot, err := other.Observe(ctx, agent.InstanceRequest{AgentID: b.AgentID})
	if err != nil || snapshot.State == "stopped" {
		t.Fatalf("other scope=%+v err=%v", snapshot, err)
	}
	if _, err := host.StopAgent(ctx, agent.StopAgentRequest{AgentID: b.AgentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := other.Wait(ctx, agent.InstanceRequest{AgentID: b.AgentID}); err != nil {
		t.Fatal(err)
	}
}

func TestGoSpawnPreservesExplicitExecutionLimits(t *testing.T) {
	ctx := testContext(t)
	profile := stdlib.Profile{}
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
	zero := int64(0)
	config.ExecutionLimits = &agent.ExecutionLimits{ProviderCalls: &zero}
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("must not call model")})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil {
		t.Fatal(err)
	}
	if outcome.State == "completed" {
		t.Fatalf("zero provider limit completed task: %+v", outcome)
	}
	models.mu.Lock()
	count := len(models.requests)
	models.mu.Unlock()
	if count != 0 {
		t.Fatalf("zero provider limit allowed %d calls", count)
	}
	config = taskConfig()
	config.ExecutionLimits = &agent.ExecutionLimits{AgentStarts: &zero}
	if _, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("must not start agent")}); err == nil {
		t.Fatal("zero agentStarts limit accepted spawn")
	}
}

type blockingCatalogModels struct {
	fixtureModels
	entered chan struct{}
	once    sync.Once
}

func (f *blockingCatalogModels) ListModels(ctx context.Context, q model.ModelQuery) ([]model.Model, error) {
	f.once.Do(func() { close(f.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}
func TestGoSpawnCatalogUsesRequestContext(t *testing.T) {
	ctx := testContext(t)
	models := &blockingCatalogModels{entered: make(chan struct{})}
	profile := stdlib.Profile{}
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
	requestCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := client.Spawn(requestCtx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("cancel catalog")})
		done <- err
	}()
	select {
	case <-models.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Spawn cancellation error=%v", err)
		}
	case <-ctx.Done():
		t.Fatal("Spawn ignored request cancellation")
	}
	if len(models.requests) != 0 {
		t.Fatal("canceled catalog reached model")
	}
}
