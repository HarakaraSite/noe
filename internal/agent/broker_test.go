package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"noe/internal/agent/provider"
)

type fixtureProvider struct {
	mu    sync.Mutex
	calls []provider.Request
	gen   func(context.Context, provider.Request) (provider.Response, error)
}

func (p *fixtureProvider) Generate(ctx context.Context, request provider.Request) (provider.Response, error) {
	p.mu.Lock()
	p.calls = append(p.calls, request)
	p.mu.Unlock()
	if p.gen != nil {
		return p.gen(ctx, request)
	}
	return provider.Response{
		Message:      json.RawMessage(`{"role":"assistant","content":"fixture answer"}`),
		FinishReason: "stop",
		Usage:        json.RawMessage(`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`),
	}, nil
}

func (p *fixtureProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

func TestHostAndOperationAreLazyUntilRPC(t *testing.T) {
	p := &fixtureProvider{}
	h := NewHost(Options{Provider: p})
	t.Cleanup(h.Close)
	if h.broker != nil {
		t.Fatal("NewHost opened a listener")
	}
	if got := h.Defaults(); got != "{}" {
		t.Fatalf("root defaults = %q, want {}", got)
	}
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	if h.broker != nil {
		t.Fatal("NewOperation opened a listener")
	}
	if p.callCount() != 0 {
		t.Fatal("NewHost/NewOperation called the provider")
	}
	if _, err := op.Call("invalid", `{}`); err == nil {
		t.Fatal("unknown method was accepted")
	}
	if h.broker != nil {
		t.Fatal("invalid method started the broker")
	}
}

func TestIncompleteConnectionDoesNotFallBackToRootBroker(t *testing.T) {
	p := &fixtureProvider{}
	h := NewHost(Options{
		Provider: p,
		Connection: &Connection{
			Token: "parent-capability",
			Scope: "parent-scope",
		},
	})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	_, err := op.Call("start", `{}`)
	var rpcErr *RPCError
	if !errors.As(err, &rpcErr) || rpcErr.Code != "connection" {
		t.Fatalf("incomplete connection error = %v, want connection error", err)
	}
	if h.broker != nil {
		t.Fatal("incomplete remote connection fell back to a root broker")
	}
	if p.callCount() != 0 {
		t.Fatalf("incomplete remote connection reached the provider %d times", p.callCount())
	}
}

func TestConnectionJSONUsesParentBrokerAndInheritedDefaults(t *testing.T) {
	p := &fixtureProvider{}
	parent := NewHost(Options{Provider: p})
	t.Cleanup(parent.Close)
	parentOp := parent.NewOperation(context.Background())
	t.Cleanup(parentOp.Close)
	setupRun := startFixtureRun(t, parentOp)
	if err := finishFixtureRun(parentOp, setupRun); err != nil {
		t.Fatal(err)
	}
	parent.mu.Lock()
	b := parent.broker
	parent.mu.Unlock()
	if b == nil {
		t.Fatal("parent broker was not started")
	}
	wantDefaults := Config{Provider: "openrouter", Model: "fixture/model", MaxTokens: 128}
	encoded, err := json.Marshal(Connection{URL: b.url, Token: b.token, Scope: parentOp.scope, Defaults: wantDefaults})
	if err != nil {
		t.Fatal(err)
	}
	var connection Connection
	if err := json.Unmarshal(encoded, &connection); err != nil {
		t.Fatal(err)
	}
	child := NewHost(Options{Connection: &connection})
	t.Cleanup(child.Close)
	if got, want := child.Defaults(), `{"provider":"openrouter","model":"fixture/model","maxTokens":128}`; got != want {
		t.Fatalf("inherited defaults = %s, want %s", got, want)
	}
	childOp := child.NewOperation(context.Background())
	t.Cleanup(childOp.Close)
	runID := startFixtureRun(t, childOp)
	result := callFixtureModel(childOp, runID, "remote request")
	if result.err != nil || result.answer != "fixture answer" {
		t.Fatalf("parent broker model call = %#v", result)
	}
	if err := finishFixtureRun(childOp, runID); err != nil {
		t.Fatal(err)
	}
	if child.broker != nil {
		t.Fatal("connected host opened a local broker")
	}
	if p.callCount() != 1 {
		t.Fatalf("parent provider call count = %d, want 1", p.callCount())
	}
}

func TestBrokerSeparatesParallelOperationScopesAndTraceMetadata(t *testing.T) {
	var trace bytes.Buffer
	entered := make(chan string, 2)
	release := make(chan struct{})
	p := &fixtureProvider{gen: func(ctx context.Context, request provider.Request) (provider.Response, error) {
		var last struct {
			Content string `json:"content"`
		}
		if err := json.Unmarshal(request.Messages[len(request.Messages)-1], &last); err != nil {
			return provider.Response{}, err
		}
		entered <- last.Content
		select {
		case <-release:
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
		message, _ := json.Marshal(map[string]string{"role": "assistant", "content": "answer:" + last.Content})
		return provider.Response{
			Message: message, FinishReason: "stop",
			Usage: json.RawMessage(`{"prompt_tokens":7,"completion_tokens":4,"total_tokens":11}`),
		}, nil
	}}
	h := NewHost(Options{Provider: p, Trace: &trace})
	t.Cleanup(h.Close)
	opA := h.NewOperation(context.Background())
	opB := h.NewOperation(context.Background())
	t.Cleanup(opA.Close)
	t.Cleanup(opB.Close)
	if opA.scope == opB.scope {
		t.Fatal("operations share a scope")
	}

	runA := startFixtureRun(t, opA)
	runB := startFixtureRun(t, opB)
	results := make(chan modelCallResult, 2)
	go func() { results <- callFixtureModel(opA, runA, "history-A") }()
	go func() { results <- callFixtureModel(opB, runB, "history-B") }()
	seen := map[string]bool{}
	for range 2 {
		select {
		case instruction := <-entered:
			seen[instruction] = true
		case <-time.After(2 * time.Second):
			t.Fatal("parallel model calls did not both reach the provider")
		}
	}
	if !seen["history-A"] || !seen["history-B"] {
		t.Fatalf("provider saw histories %v", seen)
	}
	close(release)
	got := map[string]string{}
	for range 2 {
		select {
		case result := <-results:
			if result.err != nil {
				t.Fatalf("model call failed: %v", result.err)
			}
			got[result.instruction] = result.answer
		case <-time.After(2 * time.Second):
			t.Fatal("model call did not finish")
		}
	}
	if got["history-A"] != "answer:history-A" || got["history-B"] != "answer:history-B" {
		t.Fatalf("parallel results crossed: %v", got)
	}
	if err := finishFixtureRun(opA, runA); err != nil {
		t.Fatal(err)
	}
	if err := finishFixtureRun(opB, runB); err != nil {
		t.Fatal(err)
	}
	if p.callCount() != 2 {
		t.Fatalf("provider call count = %d, want 2", p.callCount())
	}
	traceText := trace.String()
	for _, private := range []string{"history-A", "history-B", "answer:"} {
		if strings.Contains(traceText, private) {
			t.Fatalf("trace leaked request/response data %q: %s", private, traceText)
		}
	}
	if !strings.Contains(traceText, `"promptTokens":7`) || !strings.Contains(traceText, `"totalTokens":11`) {
		t.Fatalf("trace omitted token usage metadata: %s", traceText)
	}
}

func TestOperationCloseCancelsOnlyItsProviderWork(t *testing.T) {
	started := make(chan struct{}, 2)
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		started <- struct{}{}
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p})
	t.Cleanup(h.Close)
	opA := h.NewOperation(context.Background())
	opB := h.NewOperation(context.Background())
	runA := startFixtureRun(t, opA)
	runB := startFixtureRun(t, opB)
	resultA := make(chan error, 1)
	resultB := make(chan error, 1)
	go func() { _, err := opA.Call("model", fixtureModelPayload(runA, "A")); resultA <- err }()
	go func() { _, err := opB.Call("model", fixtureModelPayload(runB, "B")); resultB <- err }()
	for range 2 {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("provider calls did not start")
		}
	}
	opA.Close()
	select {
	case err := <-resultA:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("closed operation error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("operation close did not cancel provider work")
	}
	if _, err := opA.Call("model", fixtureModelPayload(runA, "again")); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed operation accepted another call: %v", err)
	}
	if p.callCount() != 2 {
		t.Fatalf("cancelled operation started another provider call, count=%d", p.callCount())
	}
	// The other scope remains live until independently cancelled.
	opB.Close()
	select {
	case err := <-resultB:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("second closed operation error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second operation did not cancel")
	}
}

func TestHostCloseCancelsProviderAndClosesBroker(t *testing.T) {
	started := make(chan struct{}, 1)
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		started <- struct{}{}
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p})
	op := h.NewOperation(context.Background())
	runID := startFixtureRun(t, op)
	result := make(chan error, 1)
	go func() { _, err := op.Call("model", fixtureModelPayload(runID, "blocked")); result <- err }()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("provider call did not start")
	}
	h.mu.Lock()
	b := h.broker
	h.mu.Unlock()
	if b == nil {
		t.Fatal("broker was not created by RPC")
	}
	h.Close()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("host close error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("host close did not stop provider call")
	}
	if _, err := net.DialTimeout("tcp", strings.TrimPrefix(b.url, "http://"), 100*time.Millisecond); err == nil {
		t.Fatal("host close left the broker listener accepting connections")
	}
	if _, err := op.Call("start", `{}`); !errors.Is(err, context.Canceled) {
		t.Fatalf("operation call after host close = %v, want cancellation", err)
	}
	if p.callCount() != 1 {
		t.Fatalf("provider call count after close = %d, want 1", p.callCount())
	}
}

func TestHostCloseWaitsForCancelledHandlerAndTrace(t *testing.T) {
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	var trace bytes.Buffer
	p := &fixtureProvider{gen: func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		<-release // Control handler completion independently of request cancellation.
		return provider.Response{}, ctx.Err()
	}}
	h := NewHost(Options{Provider: p, Trace: &trace})
	op := h.NewOperation(context.Background())
	id := startFixtureRun(t, op)
	callDone := make(chan struct{})
	go func() { defer close(callDone); op.Call("model", fixtureModelPayload(id, "wait")) }()
	<-started
	closeDone := make(chan struct{})
	go func() { h.Close(); close(closeDone) }()
	<-cancelled
	select {
	case <-closeDone:
		t.Fatal("Close returned before its model handler completed")
	case <-time.After(20 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not wait and finish")
	}
	<-callDone
	if !strings.Contains(trace.String(), `"status":"provider_error"`) {
		t.Fatalf("Close returned before final handler trace: %s", trace.String())
	}
}

func TestProviderErrorDoesNotEchoProviderDiagnostics(t *testing.T) {
	const secret = "fixture-secret-that-must-not-escape"
	p := &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{}, fmt.Errorf("upstream echoed %s", secret)
	}}
	h := NewHost(Options{Provider: p})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	_, err := op.Call("model", fixtureModelPayload(runID, "private prompt"))
	if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "private prompt") {
		t.Fatalf("provider diagnostic escaped through broker error: %v", err)
	}
	if err := finishFixtureRun(op, runID); err != nil {
		t.Fatal(err)
	}
}

type modelCallResult struct {
	instruction string
	answer      string
	err         error
}

func startFixtureRun(t *testing.T, op *Operation) string {
	t.Helper()
	return startFixtureRunWithConfig(t, op, runConfig{
		Provider: "openrouter", Model: "fixture/model", Name: "fixture", MaxTokens: 128,
	})
}

func startFixtureRunWithConfig(t *testing.T, op *Operation, config runConfig) string {
	t.Helper()
	payload, err := json.Marshal(startRequest{
		Provider: config.Provider, Model: config.Model, Name: config.Name, MaxTokens: config.MaxTokens,
	})
	if err != nil {
		t.Fatalf("marshal start request: %v", err)
	}
	response, err := op.Call("start", string(payload))
	if err != nil {
		t.Fatalf("start run: %v", err)
	}
	var result struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal([]byte(response), &result); err != nil || result.RunID == "" {
		t.Fatalf("invalid start result %q: %v", response, err)
	}
	return result.RunID
}

func fixtureModelPayload(runID, instruction string) string {
	message, _ := json.Marshal(map[string]string{"role": "user", "content": instruction})
	payload, _ := json.Marshal(modelRequest{
		RunID: runID, Model: "fixture/model", Messages: []json.RawMessage{message}, MaxTokens: 128,
	})
	return string(payload)
}

func callFixtureModel(op *Operation, runID, instruction string) modelCallResult {
	result, err := op.Call("model", fixtureModelPayload(runID, instruction))
	if err != nil {
		return modelCallResult{instruction: instruction, err: err}
	}
	var response modelResponse
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		return modelCallResult{instruction: instruction, err: err}
	}
	var message struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(response.Message, &message); err != nil {
		return modelCallResult{instruction: instruction, err: err}
	}
	return modelCallResult{instruction: instruction, answer: message.Content}
}

func finishFixtureRun(op *Operation, runID string) error {
	_, err := op.Call("finish", fmt.Sprintf(`{"runId":%q,"status":"completed"}`, runID))
	return err
}
