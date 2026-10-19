package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/decision"
)

type decisionProviderFunc func(context.Context, decision.Request) (decision.Response, error)

func (f decisionProviderFunc) Decide(ctx context.Context, r decision.Request) (decision.Response, error) {
	return f(ctx, r)
}

const validDecisionRequest = `{"provider":"openrouter","model":"m","state":"x","questions":{"q":{}}}`

func decisionConnection(t *testing.T, h *Host, op *Operation) *Connection {
	t.Helper()
	b, err := h.ensureBroker()
	if err != nil {
		t.Fatal(err)
	}
	return &Connection{URL: b.url, Token: b.token, Scope: op.scope}
}

func TestDecisionBrokerUsesScopedParentProvider(t *testing.T) {
	var calls atomic.Int32
	h := NewHost(Options{DecisionProvider: decisionProviderFunc(func(ctx context.Context, r decision.Request) (decision.Response, error) {
		calls.Add(1)
		return decision.Response{JSON: json.RawMessage(`{"model":"m","answers":{"q":{"future":9007199254740993}},"extension":true}`)}, nil
	})})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	connection := decisionConnection(t, h, op)
	remote := NewHost(Options{Connection: connection})
	defer remote.Close()
	child := remote.NewOperation(context.Background())
	defer child.Close()
	response, err := child.Call("decide", validDecisionRequest)
	if err != nil || !json.Valid([]byte(response)) || calls.Load() != 1 {
		t.Fatalf("response=%s err=%v calls=%d", response, err, calls.Load())
	}
	if _, err := child.Call("decide", `{"provider":"openrouter","model":"m","state":"x","questions":{"q":{}},"extra":true}`); err == nil || calls.Load() != 1 {
		t.Fatalf("invalid request reached provider: %v", err)
	}
}

func TestDecisionOperationCloseCancelsRemoteRequest(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	h := NewHost(Options{DecisionProvider: decisionProviderFunc(func(ctx context.Context, _ decision.Request) (decision.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return decision.Response{}, ctx.Err()
	})})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	connection := decisionConnection(t, h, op)
	remote := NewHost(Options{Connection: connection})
	defer remote.Close()
	child := remote.NewOperation(context.Background())
	defer child.Close()
	done := make(chan error, 1)
	go func() { _, err := child.Call("decide", validDecisionRequest); done <- err }()
	<-started
	op.Close()
	select {
	case <-stopped:
	default:
		t.Fatal("close returned before provider stopped")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("remote request did not return")
	}
}

func TestDecisionDoesNotFallBackFromFixtureToRealProvider(t *testing.T) {
	h := NewHost(Options{Provider: &fixtureProvider{}})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	_, err := op.Call("decide", validDecisionRequest)
	var rpc *RPCError
	if !errors.As(err, &rpc) || rpc.Code != "provider" {
		t.Fatalf("error=%v", err)
	}
}

func TestDecisionOwnerMetadataSurvivesRunRemoval(t *testing.T) {
	h := NewHost(Options{Provider: &fixtureProvider{}})
	defer h.Close()
	op := h.NewOperation(context.Background())
	defer op.Close()
	runID := startFixtureRun(t, op)
	child := h.NewOperation(context.Background())
	defer child.Close()
	h.mu.Lock()
	child.parentRunID = runID
	child.decisionOwner = decisionOwnerFields(runID, h.runs[runID], "tool")
	h.mu.Unlock()
	if err := finishFixtureRun(op, runID); err != nil {
		t.Fatal(err)
	}
	fields := h.decisionFields(child, "request", decision.Request{Provider: "openrouter", Model: "decision/model"})
	if fields["runId"] != runID || fields["toolCallId"] != "tool" || fields["model"] != "fixture/model" || fields["decisionModel"] != "decision/model" {
		t.Fatalf("ownership lost during parent cleanup: %+v", fields)
	}
}
