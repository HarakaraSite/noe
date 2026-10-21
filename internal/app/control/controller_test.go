package control

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type controllerFixtureModels struct {
	mu      sync.Mutex
	calls   int
	entered chan struct{}
}

func (m *controllerFixtureModels) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return nil, nil
}

func (m *controllerFixtureModels) Generate(ctx context.Context, _ model.Request) (model.Response, error) {
	m.mu.Lock()
	m.calls++
	call := m.calls
	m.mu.Unlock()
	if call == 1 {
		close(m.entered)
		<-ctx.Done()
		return model.Response{}, ctx.Err()
	}
	return model.Response{Message: json.RawMessage(`{"role":"assistant","content":"finished"}`), FinishReason: "stop"}, nil
}

func (*controllerFixtureModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, model.ErrUnsupported
}

func (*controllerFixtureModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, model.ErrUnsupported
}

func newControllerFixtureHost(t *testing.T, ctx context.Context, models model.Service) *agent.Host {
	t.Helper()
	profile := stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(host.Close)
	return host
}

func TestControllerCloseBeforeAcceptDoesNotResolveDirectory(t *testing.T) {
	var calls int
	controller := New(context.Background(), Options{Directory: func() (string, error) {
		calls++
		return "", errors.New("directory must remain lazy")
	}})
	if err := controller.Close(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("directory resolved %d times before Accept", calls)
	}
}

func TestControllerAcceptedActivationFailureSurvivesImmediateClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := newControllerFixtureHost(t, ctx, &controllerFixtureModels{entered: make(chan struct{})})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	activationErr := errors.New("control directory unavailable")
	controller := New(ctx, Options{Host: host, Directory: func() (string, error) {
		close(started)
		<-release
		return "", activationErr
	}, Cancel: cancel})
	controller.Accept()
	closed := make(chan error, 1)
	go func() { closed <- controller.Close() }()
	select {
	case <-started:
	case err := <-closed:
		t.Fatalf("Close skipped accepted activation: %v", err)
	case <-time.After(time.Second):
		t.Fatal("accepted controller did not begin activation")
	}
	unblock()
	select {
	case err := <-closed:
		if !errors.Is(err, activationErr) {
			t.Fatalf("Close error=%v, want activation error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not join accepted activation")
	}
	if ctx.Err() == nil {
		t.Fatal("activation failure did not cancel the application")
	}
}

func TestControllerCloseJoinsActivationAndRemovesRegistration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := newControllerFixtureHost(t, ctx, &controllerFixtureModels{entered: make(chan struct{})})
	dir := t.TempDir()
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	ready := make(chan agent.Event, 1)
	controller := New(ctx, Options{Host: host, Directory: func() (string, error) {
		close(started)
		<-release
		return dir, nil
	}, Observe: func(event agent.Event) { ready <- event }})
	controller.Accept()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("accepted controller did not begin activation")
	}
	closed := make(chan error, 1)
	go func() { closed <- controller.Close() }()
	unblock()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not join activation and server shutdown")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("registration remains after Close: entries=%v err=%v", entries, err)
	}
	select {
	case event := <-ready:
		if event["type"] != "control.ready" {
			t.Fatalf("ready event=%v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("accepted activation did not publish readiness")
	}
}

func TestControllerPublicHostControlHTTPAndDiscovery(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	models := &controllerFixtureModels{entered: make(chan struct{})}
	host := newControllerFixtureHost(t, ctx, models)
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	instruction := "wait for cancellation"
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{
		Config:      agent.TaskConfig{Config: agent.AgentConfig{Provider: "fixture", Model: "fixture-model", Name: "control-test", Effort: "none"}},
		Instruction: &instruction,
		KeepAlive:   true,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-models.entered:
	case <-ctx.Done():
		t.Fatal("first model call did not start")
	}

	dir := t.TempDir()
	ready := make(chan agent.Event, 1)
	controller := New(ctx, Options{
		Host:      host,
		Directory: func() (string, error) { return dir, nil },
		Observe:   func(event agent.Event) { ready <- event },
	})
	controller.Accept()
	t.Cleanup(func() {
		if err := controller.Close(); err != nil {
			t.Errorf("controller.Close: %v", err)
		}
	})
	select {
	case event := <-ready:
		if event["type"] != "control.ready" || event["url"] == "" || event["token"] != nil {
			t.Fatalf("unsafe or incomplete ready event: %v", event)
		}
	case <-ctx.Done():
		t.Fatal("controller did not become ready")
	}
	hosts, err := DiscoverControls(ctx, dir)
	if err != nil || len(hosts) != 1 {
		t.Fatalf("discovery hosts=%d err=%v", len(hosts), err)
	}
	connection := hosts[0].Connection

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, connection.URL+"/control/agents", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer wrong-capability")
	unauthorized, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong control capability status=%d", unauthorized.StatusCode)
	}

	raw, err := connection.Call(ctx, "agents", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var listing ControlListing
	if err := json.Unmarshal(raw, &listing); err != nil {
		t.Fatal(err)
	}
	if len(listing.Agents) != 1 || listing.Agents[0].AgentID != spawned.AgentID || listing.Agents[0].CurrentRunID == "" || listing.Agents[0].CurrentCommandID != spawned.CommandID {
		t.Fatalf("live host listing=%+v", listing)
	}
	runID := listing.Agents[0].CurrentRunID

	raw, err = connection.Call(ctx, "steer", map[string]any{"runId": runID, "instruction": "keep waiting"})
	if err != nil {
		t.Fatal(err)
	}
	var steered agent.CommandReceipt
	if err := json.Unmarshal(raw, &steered); err != nil || steered.Status != "accepted" {
		t.Fatalf("steer receipt=%s err=%v", raw, err)
	}

	raw, err = connection.Call(ctx, "queue", agent.QueueRequest{AgentID: spawned.AgentID, Instruction: "finish after cancellation"})
	if err != nil {
		t.Fatal(err)
	}
	var queued agent.CommandReceipt
	if err := json.Unmarshal(raw, &queued); err != nil || queued.CommandID == "" || queued.Status != "accepted" {
		t.Fatalf("queue receipt=%s err=%v", raw, err)
	}

	raw, err = connection.Call(ctx, "interrupt", map[string]string{"runId": runID})
	if err != nil {
		t.Fatal(err)
	}
	var interrupted map[string]string
	if err := json.Unmarshal(raw, &interrupted); err != nil || interrupted["target"] != "run" || interrupted["status"] != "accepted" || interrupted["runId"] != runID {
		t.Fatalf("run interrupt receipt=%s err=%v", raw, err)
	}
	first, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil || first.State != "cancelled" {
		t.Fatalf("interrupted task=%+v err=%v", first, err)
	}
	second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: queued.CommandID})
	if err != nil || second.State != "completed" || second.Answer != "finished" {
		t.Fatalf("queued task after run interrupt=%+v err=%v", second, err)
	}

	raw, err = connection.Call(ctx, "interrupt", map[string]string{"agentId": spawned.AgentID})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &interrupted); err != nil || interrupted["target"] != "agent" || interrupted["status"] != "accepted" {
		t.Fatalf("agent interrupt receipt=%s err=%v", raw, err)
	}
	if _, err := client.Wait(ctx, agent.InstanceRequest{AgentID: spawned.AgentID}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, connection.HostID+".json")); err != nil {
		t.Fatalf("registration missing before orderly close: %v", err)
	}
}

func TestControllerRegistrationFailureCancelsAppAndReturnsError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	host := newControllerFixtureHost(t, ctx, &controllerFixtureModels{entered: make(chan struct{})})
	file := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	controller := New(ctx, Options{Host: host, Directory: func() (string, error) { return file, nil }, Cancel: cancel})
	controller.Accept()
	deadline := time.After(2 * time.Second)
	for ctx.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("registration failure did not cancel app context")
		case <-time.After(time.Millisecond):
		}
	}
	err := controller.Close()
	if err == nil {
		t.Fatal("registration failure was hidden from Close")
	}
	if !strings.Contains(err.Error(), "create agent runtime directory") {
		t.Fatalf("registration error=%v", err)
	}
}
