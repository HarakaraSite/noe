package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestControlServerUsesSeparateCapabilityAndCleansRegistration(t *testing.T) {
	dir := t.TempDir()
	h := NewHost(Options{Provider: &fixtureProvider{}, ControlDir: dir})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	id := spawnLifecycle(t, op, nil, true)
	hosts, err := DiscoverControls(context.Background(), dir)
	if err != nil || len(hosts) != 1 || len(hosts[0].Listing.Agents) != 1 || hosts[0].Listing.Agents[0].AgentID != id {
		t.Fatalf("discovery: %+v %v", hosts, err)
	}
	b := h.broker
	if b.control.connection.Token == b.token {
		t.Fatal("human and child capability are identical")
	}
	for _, request := range []struct{ path, token, scope string }{
		{"/control/agents", b.token, op.scope},
		{"/rpc/observe_agent", b.control.connection.Token, op.scope},
	} {
		req, _ := http.NewRequest(http.MethodPost, b.url+request.path, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+request.token)
		req.Header.Set("X-Noe-Scope", request.scope)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s accepted wrong capability: %d", request.path, resp.StatusCode)
		}
	}
	result, err := hosts[0].Connection.Call(context.Background(), "interrupt", map[string]string{"agentId": id})
	if err != nil {
		t.Fatal(err)
	}
	var ack map[string]string
	if json.Unmarshal(result, &ack) != nil || ack["status"] != "accepted" {
		t.Fatalf("ack: %s", result)
	}
	snapshot := lifecycleSnapshot(t, op, id, true)
	if snapshot.State != "stopped" || snapshot.Reason != "interrupted" {
		t.Fatalf("snapshot: %+v", snapshot)
	}
	op.Close()
	h.Close()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("registration not cleaned: %v %v", entries, err)
	}
}

func TestControlAcknowledgementSurvivesHostClose(t *testing.T) {
	h := NewHost(Options{Provider: &fixtureProvider{}, ControlDir: t.TempDir()})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	id := spawnLifecycle(t, op, nil, true)
	h.mu.Lock()
	instance := h.instances[id]
	connection := h.broker.control.connection
	h.mu.Unlock()
	closed := make(chan struct{})
	go func() {
		<-instance.done
		op.Close()
		h.Close()
		close(closed)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := connection.Call(ctx, "interrupt", map[string]string{"agentId": id})
	if err != nil || !strings.Contains(string(result), `"status":"accepted"`) {
		t.Fatalf("accepted interrupt lost during shutdown: %s %v", result, err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("host close did not finish")
	}
}
