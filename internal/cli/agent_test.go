package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// Managed tasks re-execute NOE. Dispatch those child invocations through the
// same CLI entry point instead of accidentally running the test suite again.
func TestMain(m *testing.M) {
	if len(os.Args) >= 3 && os.Args[1] == "run" && os.Getenv("NOE_AGENT_CONNECTION") != "" {
		os.Exit(Run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}

func agentFixtureSettings(t *testing.T, url string) {
	t.Helper()
	keyFile := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(keyFile, []byte("fixture-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_AGENT_RUNTIME_DIR", t.TempDir())
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", keyFile)
	t.Setenv("NOE_OPENROUTER_BASE_URL", url)
	t.Setenv("NOE_AGENT_TRACE", "")
}

func TestCLIAgentRunAndEval(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture-key" || r.URL.Path != "/chat/completions" {
			t.Errorf("invalid provider request: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"x\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"answer\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	agentFixtureSettings(t, server.URL)
	source := `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"hello");console.log(result.lastResult.answer);`
	file := filepath.Join(t.TempDir(), "agent.js")
	if err := os.WriteFile(file, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"run", file}, {"eval", source}} {
		var out, diagnostic bytes.Buffer
		code := Run(context.Background(), args, strings.NewReader(""), &out, &diagnostic)
		if code != 0 || out.String() != "answer\n" || diagnostic.Len() != 0 {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", args, code, &out, &diagnostic)
		}
	}
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"eval", `import {run_agent} from "noe:agent";await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"hello");`}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.Len() != 0 || diagnostic.Len() != 0 || calls.Load() != 3 {
		t.Fatalf("run printed automatically: code=%d out=%q err=%q calls=%d", code, &out, &diagnostic, calls.Load())
	}
}

func TestCLIAgentTimeoutPreservesExit124(t *testing.T) {
	var started atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		io.WriteString(w, ": waiting\n\n")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	agentFixtureSettings(t, server.URL)
	t.Setenv("NOE_AGENT_TRACE", "1")
	var out, diagnostic bytes.Buffer
	// Allow managed-process startup under race instrumentation before testing
	// cancellation of the deliberately blocked provider request.
	code := Run(context.Background(), []string{"eval", "--timeout", "2s", `import {run_agent} from "noe:agent";await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"wait");`}, strings.NewReader(""), &out, &diagnostic)
	if code != 124 || !started.Load() || !strings.Contains(diagnostic.String(), "execution timed out") {
		t.Fatalf("code=%d started=%v stderr=%q", code, started.Load(), &diagnostic)
	}
}
