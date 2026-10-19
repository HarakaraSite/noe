package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
)

func TestAgentControlCLIListsAndTargetsAgentsWithoutExposingCapability(t *testing.T) {
	const capability = "private-human-capability"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+capability {
			t.Error("control capability missing")
			http.Error(w, "unauthorized", 401)
			return
		}
		if r.URL.Path == "/control/agents" {
			json.NewEncoder(w).Encode(agent.ControlListing{HostID: "host", PID: 1, Agents: []agent.AgentSnapshot{{AgentID: "agent-full", CurrentRunID: "run-full", Name: "worker", State: "running"}}})
			return
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			http.Error(w, "invalid", 400)
			return
		}
		switch r.URL.Path {
		case "/control/steer":
			if payload["runId"] != "run-full" || payload["instruction"] != "use a table" {
				t.Errorf("steer payload=%v", payload)
			}
		case "/control/queue":
			if payload["agentId"] != "agent-full" || payload["instruction"] != "run tests" {
				t.Errorf("queue payload=%v", payload)
			}
		case "/control/interrupt":
			if payload["agentId"] != "agent-full" && payload["runId"] != "run-full" {
				t.Errorf("interrupt payload=%v", payload)
			}
		default:
			t.Errorf("unexpected control path=%q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "commandId": "command-one"})
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv(agent.ControlDirEnv, dir)
	encoded, _ := json.Marshal(agent.ControlConnection{Version: 1, HostID: "host", PID: 1, URL: server.URL, Token: capability})
	if err := os.WriteFile(filepath.Join(dir, "host.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"agents"}, "agent-full"},
		{[]string{"steer", "run-", "use a table"}, "steer accepted: command-one"},
		{[]string{"queue", "agent-", "run tests"}, "queue accepted: command-one"},
		{[]string{"interrupt", "--agent", "agent-"}, "interrupt accepted: command-one"},
		{[]string{"interrupt", "--run", "run-"}, "interrupt accepted: command-one"},
	} {
		var out, diagnostic bytes.Buffer
		code := Run(context.Background(), test.args, strings.NewReader(""), &out, &diagnostic)
		if code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), test.want) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", test.args, code, &out, &diagnostic)
		}
		if strings.Contains(out.String(), capability) || strings.Contains(diagnostic.String(), capability) {
			t.Fatal("control capability escaped to CLI output")
		}
	}
}

func TestAgentControlCLIEmptyRegistryAndInvalidInputs(t *testing.T) {
	t.Setenv(agent.ControlDirEnv, t.TempDir())
	for _, test := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"agents"}, 0, "No active agents."},
		{[]string{"steer", "missing", "task"}, 1, "no active agent"},
		{[]string{"agents", "extra"}, 2, "Usage:"},
		{[]string{"steer", "run"}, 2, "Usage:"},
		{[]string{"queue", "agent", " "}, 2, "Usage:"},
		{[]string{"interrupt", "agent"}, 2, "Usage:"},
		{[]string{"interrupt", "--run", "run", "--agent", "agent"}, 2, "Usage:"},
	} {
		var out, diagnostic bytes.Buffer
		code := Run(context.Background(), test.args, strings.NewReader(""), &out, &diagnostic)
		if code != test.code || !strings.Contains(out.String()+diagnostic.String(), test.want) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", test.args, code, &out, &diagnostic)
		}
	}
}

func TestOrdinaryEvalDoesNotRequireAgentRuntimeDirectory(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux directory environment regression")
	}
	for _, key := range []string{"HOME", "XDG_CACHE_HOME", "XDG_RUNTIME_DIR", "NOE_AGENT_RUNTIME_DIR"} {
		t.Setenv(key, "")
	}
	var out, diagnostic bytes.Buffer
	code := Run(context.Background(), []string{"eval", `console.log(1)`}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "1\n" || diagnostic.Len() != 0 {
		t.Fatalf("ordinary eval acquired control dependency: code=%d out=%q err=%q", code, &out, &diagnostic)
	}
}
