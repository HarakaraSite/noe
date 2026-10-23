package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestTypedClientSpawnPreservesJSONConfigErrors(t *testing.T) {
	ctx := testContext(t)
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: new(fixtureModels)})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	invalidRatio, negative := 1.0, int64(-1)
	tests := []struct {
		name   string
		mutate func(*agent.TaskConfig)
	}{
		{name: "context budget", mutate: func(config *agent.TaskConfig) { config.ContextBudget = &agent.ContextBudget{} }},
		{name: "context policy", mutate: func(config *agent.TaskConfig) {
			config.ContextPolicy = &agent.ContextPolicy{RetainRatio: &invalidRatio}
		}},
		{name: "execution limits", mutate: func(config *agent.TaskConfig) {
			config.ExecutionLimits = &agent.ExecutionLimits{AgentStarts: &negative}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := taskConfig()
			test.mutate(&config)
			_, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("invalid config")})
			var requestErr *core.RequestError
			if !errors.As(err, &requestErr) || requestErr.Code != "request" || requestErr.Message != "invalid agent configuration" {
				t.Fatalf("Spawn error = %v, want request error for invalid agent configuration", err)
			}
		})
	}

	config := taskConfig()
	infinite := math.Inf(1)
	config.ContextPolicy = &agent.ContextPolicy{RetainRatio: &infinite}
	_, err = client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("invalid ratio")})
	var unsupported *json.UnsupportedValueError
	if !errors.As(err, &unsupported) {
		t.Fatalf("non-finite policy error = %v, want JSON unsupported-value error", err)
	}
}

type gatedCatalogModels struct {
	fixtureModels
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *gatedCatalogModels) ListModels(ctx context.Context, query model.ModelQuery) ([]model.Model, error) {
	m.once.Do(func() { close(m.entered) })
	select {
	case <-m.release:
		return nil, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestTypedClientSnapshotsMutableSpawnInput(t *testing.T) {
	ctx := testContext(t)
	models := &gatedCatalogModels{entered: make(chan struct{}), release: make(chan struct{})}
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

	starts := int64(1)
	config := taskConfig()
	config.ExecutionLimits = &agent.ExecutionLimits{AgentStarts: &starts}
	type spawnResult struct {
		response agent.SpawnResponse
		err      error
	}
	spawned := make(chan spawnResult, 1)
	go func() {
		response, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, KeepAlive: true})
		spawned <- spawnResult{response: response, err: err}
	}()
	select {
	case <-models.entered:
	case <-ctx.Done():
		t.Fatal("spawn did not reach its catalog wait")
	}

	// Spawn is paused after typed request snapshotting and before it consumes the
	// execution limit. Mutating the caller's pointer must not change admission.
	starts = 0
	close(models.release)
	var result spawnResult
	select {
	case result = <-spawned:
	case <-ctx.Done():
		t.Fatal("spawn did not finish after the catalog was released")
	}
	if result.err != nil {
		t.Fatalf("Spawn observed caller mutation: %v", result.err)
	}
	snapshot, err := client.Observe(ctx, agent.InstanceRequest{AgentID: result.response.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Execution == nil || snapshot.Execution.Limits.AgentStarts == nil || *snapshot.Execution.Limits.AgentStarts != 1 {
		t.Fatalf("spawned execution limits = %+v, want agentStarts=1", snapshot.Execution)
	}
}

func TestTypedClientResponseMutationDoesNotChangeHostSnapshot(t *testing.T) {
	ctx := testContext(t)
	profile := stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: new(fixtureModels), Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	started, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("record usage")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID}); err != nil {
		t.Fatal(err)
	}
	first, err := client.Observe(ctx, agent.InstanceRequest{AgentID: started.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	if first.Execution == nil || first.Execution.Usage == nil || first.Execution.Usage.TotalTokens == nil {
		t.Fatalf("fixture did not publish execution usage: %+v", first.Execution)
	}
	expected := *first.Execution.Usage.TotalTokens
	*first.Execution.Usage.TotalTokens = -1
	second, err := client.Observe(ctx, agent.InstanceRequest{AgentID: started.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	if got := *second.Execution.Usage.TotalTokens; got != expected {
		t.Fatalf("mutating response changed host usage: got %d, want %d", got, expected)
	}
}

func TestTypedClientNormalizesInvalidUTF8LikeJSONAdapter(t *testing.T) {
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

	badName := "agent-" + string([]byte{0xff, 0xfe})
	config := taskConfig()
	config.Name = badName
	started, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, KeepAlive: true})
	if err != nil {
		t.Fatal(err)
	}
	wantName := "agent-\ufffd\ufffd"
	snapshot, err := client.Observe(ctx, agent.InstanceRequest{AgentID: started.AgentID})
	if err != nil || snapshot.Name != wantName {
		t.Fatalf("snapshot name=%q err=%v, want %q", snapshot.Name, err, wantName)
	}

	badInstruction := "work-" + string([]byte{0xff, 0xfe})
	badCommandID := "command-" + string([]byte{0xff})
	receipt, err := client.Queue(ctx, agent.QueueRequest{AgentID: started.AgentID, Instruction: badInstruction, CommandID: badCommandID})
	if err != nil {
		t.Fatal(err)
	}
	wantCommandID := "command-\ufffd"
	if receipt.CommandID != wantCommandID {
		t.Fatalf("command ID=%q, want %q", receipt.CommandID, wantCommandID)
	}
	if _, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: receipt.CommandID}); err != nil {
		t.Fatal(err)
	}

	models.mu.Lock()
	requests := append([]model.Request(nil), models.requests...)
	models.mu.Unlock()
	wantInstruction := "work-\ufffd\ufffd"
	found := false
	for _, request := range requests {
		for _, raw := range request.Messages {
			var message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}
			if json.Unmarshal(raw, &message) == nil && message.Role == "user" && strings.Contains(message.Content, wantInstruction) {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("model request did not contain JSON-normalized instruction %q: %+v", wantInstruction, requests)
	}
}
