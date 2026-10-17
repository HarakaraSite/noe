//go:build e2e && linux

package e2e_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestAgentREPLTerminalReuseAndErrorRecovery(t *testing.T) {
	var calls atomic.Int32
	server := agentAPIFixture(t, func(_ context.Context, req agentChatRequest) agentModelReply {
		calls.Add(1)
		if agentEffort(req) != "high" {
			t.Errorf("REPL effort changed across inputs: %q", agentEffort(req))
		}
		if len(req.Messages) != 2 {
			t.Errorf("REPL history leaked: %+v", req)
		}
		if agentUser(req) == "fail" {
			return agentModelReply{Status: 401}
		}
		return agentModelReply{Content: agentUser(req) + " answer"}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	session := startTerminalREPL(t, t.TempDir(), env)
	session.enter(t, `const am=await import("noe:agent")`)
	if calls.Load() != 0 {
		t.Fatal("REPL import contacted provider")
	}
	// Completion goes through NOE's live Session even with the qjs editor.
	session.writeKeys(t, `await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"high"},"first").then(snapshot=>snapshot.lastResult.answer)`+"\r")
	session.waitForOutputAndPrompt(t, "'first answer'")
	before := session.stdout.snapshot()
	session.writeKeys(t, `await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"high"},"fail").then(snapshot=>snapshot.lastResult.state)`+"\r")
	session.waitForOutputAndPrompt(t, "'failed'")
	session.waitForPromptAfter(t, before)
	session.writeKeys(t, `await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"high"},"third").then(snapshot=>snapshot.lastResult.answer)`+"\r")
	session.waitForOutputAndPrompt(t, "'third answer'")
	session.writeKeys(t, "\x04")
	session.finish(t, 0)
	if calls.Load() != 3 {
		t.Fatalf("model calls=%d", calls.Load())
	}
}

func TestAgentREPLTerminalCtrlCCancelsModelRequest(t *testing.T) {
	started, cancelled := make(chan struct{}), make(chan struct{})
	server := agentAPIFixture(t, func(ctx context.Context, _ agentChatRequest) agentModelReply {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return agentModelReply{Status: 500}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	session := startTerminalREPL(t, t.TempDir(), env)
	session.enter(t, `const am=await import("noe:agent")`)
	session.writeKeys(t, `await am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"wait")`+"\r")
	select {
	case <-started:
	case <-time.After(ptyWaitTimeout):
		t.Fatal("model request did not start")
	}
	session.interruptKey(t)
	result := session.finish(t, 1)
	if !strings.Contains(result.stderr, "context canceled") || !strings.Contains(result.stderr, "runtime is closed") {
		t.Fatalf("REPL interruption diagnostic: %+v", result)
	}
	select {
	case <-cancelled:
	case <-time.After(ptyWaitTimeout):
		t.Fatal("Ctrl+C left model request running")
	}
}

func TestAgentREPLTerminalCompletionRPCClosesSession(t *testing.T) {
	var calls atomic.Int32
	cancelled := make(chan struct{})
	server := agentAPIFixture(t, func(ctx context.Context, _ agentChatRequest) agentModelReply {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			close(cancelled)
		}
		return agentModelReply{Status: 500}
	})
	defer server.Close()
	env := agentCLIEnv(t, server.URL)
	env["NOE_AGENT_TRACE"] = ""
	session := startTerminalREPL(t, t.TempDir(), env)
	session.enter(t, `const am=await import("noe:agent")`)
	session.enter(t, `const ap=new Proxy({}, {ownKeys(){am.run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"from completion").catch(()=>{});return ["value"]}})`)
	session.writeKeys(t, "ap.\t")
	result := session.finish(t, 1)
	if !strings.Contains(result.stderr, "active stdlib work") {
		t.Fatalf("completion did not reclaim pending RPC: calls=%d result=%+v", calls.Load(), result)
	}
	if calls.Load() > 0 {
		select {
		case <-cancelled:
		case <-time.After(ptyWaitTimeout):
			t.Fatal("completion left an arrived provider request running")
		}
	}
}
