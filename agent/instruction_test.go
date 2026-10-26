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

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func instructionSystem(request model.Request) string {
	for _, raw := range request.Messages {
		var message struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "system" {
			continue
		}
		var text string
		if json.Unmarshal(message.Content, &text) == nil {
			return text
		}
	}
	return ""
}

func instructionHost(t *testing.T, ctx context.Context, models *fixtureModels, source agent.InstructionSource, policy string, profile *stdlib.Profile) (*agent.Host, *agent.Client) {
	t.Helper()
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: profile, Instructions: source, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	client, err := host.NewClient(ctx)
	if err != nil {
		host.Close()
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	t.Cleanup(client.Close)
	return host, client
}

func instructionConfig(name, system string, hooks agent.HookModules) agent.TaskConfig {
	config := agent.TaskConfig{Config: agent.AgentConfig{
		Provider: "fixture", Model: "fixture-model", Name: name, Effort: "none", System: system,
	}, MaxSteps: 3}
	if hooks != nil {
		config.Hooks = &hooks
	}
	return config
}

func TestGoHostInstructionSnapshotIsPerInstanceAndComposedInOrder(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	var mu sync.Mutex
	body := "source-body-one"
	sourceCalls := 0
	source := func(context.Context) (string, error) {
		mu.Lock()
		defer mu.Unlock()
		sourceCalls++
		return body, nil
	}
	_, client := instructionHost(t, ctx, models, source, "policy-marker", &stdlib.Profile{})

	first, err := client.Spawn(ctx, agent.SpawnRequest{
		Config: instructionConfig("first", "instance-system", nil), Instruction: textPointer("first task"), KeepAlive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	firstResult, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: first.AgentID, CommandID: first.CommandID})
	if err != nil || firstResult.State != "completed" {
		t.Fatalf("first result=%+v err=%v", firstResult, err)
	}
	mu.Lock()
	body = "source-body-two"
	mu.Unlock()
	queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: first.AgentID, Instruction: "queued task"})
	if err != nil {
		t.Fatal(err)
	}
	queuedResult, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: first.AgentID, CommandID: queued.CommandID})
	if err != nil || queuedResult.State != "completed" {
		t.Fatalf("queued result=%+v err=%v", queuedResult, err)
	}
	second, err := client.Spawn(ctx, agent.SpawnRequest{
		Config: instructionConfig("second", "second-system", nil), Instruction: textPointer("new instance"),
	})
	if err != nil {
		t.Fatal(err)
	}
	secondResult, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: second.AgentID, CommandID: second.CommandID})
	if err != nil || secondResult.State != "completed" {
		t.Fatalf("new instance result=%+v err=%v", secondResult, err)
	}

	mu.Lock()
	calls := sourceCalls
	mu.Unlock()
	if calls != 2 {
		t.Fatalf("instruction source calls=%d, want one per instance", calls)
	}
	models.mu.Lock()
	requests := append([]model.Request(nil), models.requests...)
	models.mu.Unlock()
	if len(requests) != 3 {
		t.Fatalf("model requests=%d, want first, queued, and new-instance calls", len(requests))
	}
	wantFirst := strings.Join([]string{core.Facts(), core.URLImportFacts("disabled"), "policy-marker", "source-body-one", "instance-system"}, "\n\n")
	for index, request := range requests[:2] {
		if got := instructionSystem(request); got != wantFirst {
			t.Fatalf("instance system message %d changed across queue:\n got: %q\nwant: %q", index, got, wantFirst)
		}
	}
	wantSecond := strings.Join([]string{core.Facts(), core.URLImportFacts("disabled"), "policy-marker", "source-body-two", "second-system"}, "\n\n")
	if got := instructionSystem(requests[2]); got != wantSecond {
		t.Fatalf("new instance system message:\n got: %q\nwant: %q", got, wantSecond)
	}
}

func TestGoHostEmptyPolicyAddsOnlyGenericFacts(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	_, client := instructionHost(t, ctx, models, nil, "", &stdlib.Profile{})
	started, err := client.Spawn(ctx, agent.SpawnRequest{
		Config: instructionConfig("facts-only", "instance-system", nil), Instruction: textPointer("facts task"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if result, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID}); err != nil || result.State != "completed" {
		t.Fatalf("task result=%+v err=%v", result, err)
	}
	models.mu.Lock()
	requests := append([]model.Request(nil), models.requests...)
	models.mu.Unlock()
	if len(requests) != 1 {
		t.Fatalf("model requests=%d", len(requests))
	}
	want := core.Facts() + "\n\n" + core.URLImportFacts("disabled") + "\n\ninstance-system"
	if got := instructionSystem(requests[0]); got != want {
		t.Fatalf("system message includes content beyond facts and explicit system:\n got: %q\nwant: %q", got, want)
	}
	for _, policy := range []string{"autonomous assistant", "first line must be", "openrouter", ".noe/instructions"} {
		if strings.Contains(core.Facts(), policy) {
			t.Fatalf("generic facts contain NOE policy %q", policy)
		}
	}
}

func TestGoHostInstructionSourceErrorIsCachedBeforeHooksOrModel(t *testing.T) {
	for _, message := range []string{"instruction source unavailable", ""} {
		name := "nonempty-message"
		if message == "" {
			name = "empty-message"
		}
		t.Run(name, func(t *testing.T) {
			ctx := testContext(t)
			models := new(fixtureModels)
			var mu sync.Mutex
			sourceCalls, hookLoads := 0, 0
			source := func(context.Context) (string, error) {
				mu.Lock()
				sourceCalls++
				mu.Unlock()
				return "", errors.New(message)
			}
			profile := stdlib.Profile{NewLoader: func(_ context.Context, _ stdlib.Origin) (quickjs.ModuleLoader, error) {
				return func(specifier, _ string) (string, string, error) {
					if specifier == "fixture:instruction-hook" {
						mu.Lock()
						hookLoads++
						mu.Unlock()
						return `export function beforeModelRequest(){}`, "memory:/instruction-hook.mjs", nil
					}
					return "", "", fmt.Errorf("unexpected module %q", specifier)
				}, nil
			}}
			_, client := instructionHost(t, ctx, models, source, "policy-marker", &profile)
			hooks := agent.HookModules{"fixture:instruction-hook"}
			started, err := client.Spawn(ctx, agent.SpawnRequest{
				Config: instructionConfig("source-error", "", hooks), Instruction: textPointer("must fail before model"), KeepAlive: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			first, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID})
			if err != nil || first.State != "failed" || (message != "" && !strings.Contains(first.Error, message)) {
				t.Fatalf("first failure=%+v err=%v", first, err)
			}
			queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: started.AgentID, Instruction: "still uses cached failure"})
			if err != nil {
				t.Fatal(err)
			}
			second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: queued.CommandID})
			if err != nil || second.State != "failed" || (message != "" && !strings.Contains(second.Error, message)) {
				t.Fatalf("queued failure=%+v err=%v", second, err)
			}
			mu.Lock()
			calls, loads := sourceCalls, hookLoads
			mu.Unlock()
			if calls != 1 || loads != 0 {
				t.Fatalf("source calls=%d hook loads=%d; want cached error before hooks", calls, loads)
			}
			models.mu.Lock()
			requestCount := len(models.requests)
			models.mu.Unlock()
			if requestCount != 0 {
				t.Fatalf("provider was called %d times after instruction failure", requestCount)
			}
		})
	}
}

func TestGoHostInstructionSourcesRunConcurrentlyAcrossInstances(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var mu sync.Mutex
	active, maximum := 0, 0
	source := func(ctx context.Context) (string, error) {
		mu.Lock()
		active++
		if active > maximum {
			maximum = active
		}
		mu.Unlock()
		arrived <- struct{}{}
		select {
		case <-release:
			mu.Lock()
			active--
			mu.Unlock()
			return "concurrent source", nil
		case <-ctx.Done():
			mu.Lock()
			active--
			mu.Unlock()
			return "", ctx.Err()
		}
	}
	_, client := instructionHost(t, ctx, models, source, "", &stdlib.Profile{})
	first, err := client.Spawn(ctx, agent.SpawnRequest{Config: instructionConfig("parallel-one", "", nil), Instruction: textPointer("first")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.Spawn(ctx, agent.SpawnRequest{Config: instructionConfig("parallel-two", "", nil), Instruction: textPointer("second")})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			close(release)
			t.Fatalf("instruction sources did not overlap: %v", ctx.Err())
		case <-time.After(2 * time.Second):
			close(release)
			t.Fatal("instruction sources were serialized across instances")
		}
	}
	close(release)
	for _, receipt := range []agent.SpawnResponse{first, second} {
		outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: receipt.AgentID, CommandID: receipt.CommandID})
		if err != nil || outcome.State != "completed" {
			t.Fatalf("parallel task=%+v err=%v", outcome, err)
		}
	}
	mu.Lock()
	gotMaximum := maximum
	mu.Unlock()
	if gotMaximum < 2 {
		t.Fatalf("maximum concurrent instruction callbacks=%d", gotMaximum)
	}
}

func TestGoHostInstructionSourceCancellationJoinsBeforeTaskCompletes(t *testing.T) {
	ctx := testContext(t)
	models := new(fixtureModels)
	entered := make(chan struct{})
	canceled := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	source := func(ctx context.Context) (string, error) {
		close(entered)
		<-ctx.Done()
		close(canceled)
		<-release
		return "", ctx.Err()
	}
	_, client := instructionHost(t, ctx, models, source, "", &stdlib.Profile{})
	started, err := client.Spawn(ctx, agent.SpawnRequest{
		Config: instructionConfig("cancel-source", "", nil), Instruction: textPointer("cancel while loading"), KeepAlive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := client.CancelTask(ctx, agent.CancelTaskRequest(started)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	waited := make(chan agent.TaskOutcome, 1)
	go func() {
		outcome, _ := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID})
		waited <- outcome
	}()
	select {
	case outcome := <-waited:
		t.Fatalf("task finished before source callback joined: %+v", outcome)
	case <-time.After(30 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	select {
	case outcome := <-waited:
		if outcome.State != "cancelled" {
			t.Fatalf("cancelled task outcome=%+v", outcome)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}
