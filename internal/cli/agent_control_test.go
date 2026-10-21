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

	"forge.harakara.site/littleisland/noe/v2/internal/app/control"
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
			json.NewEncoder(w).Encode(control.ControlListing{HostID: "host", PID: 1, Agents: []control.AgentSnapshot{{AgentID: "agent-full", CurrentRunID: "run-full-identifier", CurrentCommandID: "command-full", Name: "worker", State: "running"}, {AgentID: "idle-agent-full", Name: "waiting", State: "idle"}}})
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
			if payload["runId"] != "run-full-identifier" || payload["instruction"] != "use a table" {
				t.Errorf("steer payload=%v", payload)
			}
		case "/control/queue":
			if payload["agentId"] != "agent-full" || payload["instruction"] != "run tests" {
				t.Errorf("queue payload=%v", payload)
			}
		case "/control/interrupt":
			if payload["agentId"] != "agent-full" && payload["runId"] != "run-full-identifier" {
				t.Errorf("interrupt payload=%v", payload)
			}
		default:
			t.Errorf("unexpected control path=%q", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]string{"status": "accepted", "commandId": "command-one"})
	}))
	defer server.Close()
	dir := t.TempDir()
	t.Setenv(control.ControlDirEnv, dir)
	encoded, _ := json.Marshal(control.ControlConnection{Version: 1, HostID: "host", PID: 1, URL: server.URL, Token: capability})
	if err := os.WriteFile(filepath.Join(dir, "host.json"), encoded, 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		args []string
		want string
	}{
		{[]string{"agents"}, "agent-fu"},
		{[]string{"agents", "--full"}, "run-full-identifier"},
		{[]string{"steer", "run-full", "use a table"}, "steer accepted: command-one"},
		{[]string{"queue", "agent-fu", "run tests"}, "queue accepted: command-one"},
		{[]string{"interrupt", "--agent", "agent-fu"}, "interrupt accepted: command-one"},
		{[]string{"interrupt", "--run", "run-full"}, "interrupt accepted: command-one"},
	} {
		var out, diagnostic bytes.Buffer
		code := Run(context.Background(), test.args, strings.NewReader(""), &out, &diagnostic)
		if code != 0 || diagnostic.Len() != 0 || !strings.Contains(out.String(), test.want) {
			t.Fatalf("%v: code=%d stdout=%q stderr=%q", test.args, code, &out, &diagnostic)
		}
		if strings.Contains(out.String(), capability) || strings.Contains(diagnostic.String(), capability) {
			t.Fatal("control capability escaped to CLI output")
		}
		if test.args[0] == "agents" {
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			if len(lines) != 3 {
				t.Fatalf("listing rows: %q", &out)
			}
			wantAgent, wantRun, wantIdle := "agent-fu", "run-full", "idle-age"
			if len(test.args) == 2 {
				wantAgent, wantRun, wantIdle = "agent-full", "run-full-identifier", "idle-agent-full"
			}
			row, idle := strings.Fields(lines[1]), strings.Fields(lines[2])
			if len(row) != 6 || row[1] != wantAgent || row[2] != wantRun || len(idle) != 6 || idle[1] != wantIdle || idle[2] != "-" {
				t.Fatalf("listing IDs: %q", &out)
			}
		}
	}
}

func TestAgentControlCLIEmptyRegistryAndInvalidInputs(t *testing.T) {
	t.Setenv(control.ControlDirEnv, t.TempDir())
	for _, test := range []struct {
		args []string
		code int
		want string
	}{
		{[]string{"agents"}, 0, "No active agents."},
		{[]string{"agents", "--full"}, 0, "No active agents."},
		{[]string{"steer", "missing", "task"}, 1, "no active run"},
		{[]string{"agents", "extra"}, 2, "Usage:"},
		{[]string{"agents", "--full", "extra"}, 2, "Usage:"},
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
