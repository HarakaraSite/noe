package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent"
	"noe/internal/agent/provider"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestAgentSessionReusesCachedModuleAndRecoversAfterError(t *testing.T) {
	var calls atomic.Int32
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		calls.Add(1)
		if len(req.Messages) != 2 {
			t.Errorf("history persisted across REPL inputs: %s", req.Messages)
		}
		var user struct {
			Content string `json:"content"`
		}
		json.Unmarshal(req.Messages[1], &user)
		if user.Content == "fail" {
			return provider.Response{}, errors.New("fixture failure")
		}
		return agentAnswer(user.Content), nil
	})
	s, err := NewSession(Options{Agent: &agent.Options{Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	eval := func(input string) Evaluation {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return s.Eval(ctx, input)
	}
	for _, setup := range []string{`const agentModule=await import("noe:agent")`, `const reusedAgent=agentModule.createAgent({provider:"openrouter",model:"fixture/model",effort:"auto"})`} {
		if result := eval(setup); result.Err != nil || result.Exit {
			t.Fatalf("setup: %+v", result)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("REPL import/create called provider")
	}
	if result := eval(`await reusedAgent.run("first")`); result.Err != nil || result.Exit || result.Output != "'first'" {
		t.Fatalf("first: %+v", result)
	}
	if result := eval(`await reusedAgent.run("fail")`); result.Err == nil || result.Exit {
		t.Fatalf("recoverable failure: %+v", result)
	}
	if result := eval(`await reusedAgent.run("third")`); result.Err != nil || result.Exit || result.Output != "'third'" {
		t.Fatalf("after cancelled prior context: %+v", result)
	}
	if calls.Load() != 3 {
		t.Fatalf("model calls=%d", calls.Load())
	}
}

func TestAgentSessionInterruptCancelsPendingRPC(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return provider.Response{}, ctx.Err()
	})
	s, err := NewSession(Options{Agent: &agent.Options{Provider: p}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if result := s.Eval(context.Background(), `const am=await import("noe:agent")`); result.Err != nil || result.Exit {
		t.Fatalf("import: %+v", result)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		select {
		case <-started:
			cancel()
		case <-ctx.Done():
		}
	}()
	result := s.Eval(ctx, `await am.createAgent({provider:"openrouter",model:"fixture/model",effort:"auto"}).run("wait")`)
	if !result.Exit || result.Code != 1 || !errors.Is(result.Err, context.Canceled) || !errors.Is(result.Err, quickjs.ErrClosed) {
		t.Fatalf("pending RPC interruption: %+v", result)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("REPL interruption left provider running")
	}
	if result := s.Eval(context.Background(), `42`); !result.Exit || !errors.Is(result.Err, quickjs.ErrClosed) {
		t.Fatalf("closed session accepted next input: %+v", result)
	}
}

type agentTraceWriterFunc func([]byte) (int, error)

func (f agentTraceWriterFunc) Write(data []byte) (int, error) { return f(data) }

func TestAgentCompletionUsesCurrentContextAndTracksRPC(t *testing.T) {
	var started, cancelled atomic.Bool
	trace := agentTraceWriterFunc(func(data []byte) (int, error) {
		var event struct{ Name, Status string }
		json.Unmarshal(data, &event)
		if event.Name == "completion-agent" {
			if event.Status == "started" {
				started.Store(true)
			}
			if event.Status == "cancelled" {
				cancelled.Store(true)
			}
		}
		return len(data), nil
	})
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		t.Error("completion must not drive model continuation")
		return agentAnswer("unexpected"), nil
	})
	s, err := NewSession(Options{Agent: &agent.Options{Provider: p, Trace: trace}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.h.rt.Set("completionRPCStarted", func() bool { return started.Load() }); err != nil {
		t.Fatal(err)
	}
	for _, setup := range []string{
		`const cm=await import("noe:agent")`,
		`const ca=cm.createAgent({provider:"openrouter",model:"fixture/model",name:"completion-agent",effort:"auto"})`,
		`const agentProxy=new Proxy({}, {ownKeys(){ca.run("pending from trap").catch(()=>{});while(!completionRPCStarted()){};return ["value"]}})`,
	} {
		if result := s.Eval(context.Background(), setup); result.Err != nil || result.Exit {
			t.Fatalf("setup %s: %+v", setup, result)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = s.Complete(ctx, "agentProxy.")
	var terminated *TerminationError
	if !started.Load() || !cancelled.Load() || ctx.Err() != nil || !errors.As(err, &terminated) || !errors.Is(err, quickjs.ErrClosed) {
		t.Fatalf("completion did not start current RPC then reclaim it: started=%v cancelled=%v context=%v error=%v", started.Load(), cancelled.Load(), ctx.Err(), err)
	}
	if result := s.Eval(context.Background(), `42`); !result.Exit || !errors.Is(result.Err, quickjs.ErrClosed) {
		t.Fatalf("completion RPC did not end session: %+v", result)
	}
}
