//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecisionCLIEntrypoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/alpha/decisions" || r.Header.Get("Authorization") != "Bearer agent-fixture-secret" {
			t.Errorf("request %s with missing auth", r.URL.Path)
			http.Error(w, "bad", 400)
			return
		}
		var body struct {
			Model     string                     `json:"model"`
			Questions map[string]json.RawMessage `json:"questions"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "decision/model" || len(body.Questions) != 1 {
			t.Error("bad decision body")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"model":"decision/revision","answers":{"q":{"type":"choice","choice":"yes"}},"usage":{"input_tokens":1,"output_tokens":0},"unknown":true}`)
	}))
	defer server.Close()
	work := t.TempDir()
	env := agentCLIEnv(t, server.URL+"/api/v1")
	env["NOE_AGENT_TRACE"] = ""
	source := `import {decide} from "noe:decision";const r=await decide({provider:"openrouter",model:"decision/model",state:"x",questions:{q:{}}});console.log(r.answers.q.choice,r.unknown);`
	writeFixture(t, filepath.Join(work, "decision.js"), source)
	for _, args := range [][]string{{"run", "--events=-", "decision.js"}, {"eval", "--events=-", source}, {"eval", "--observe", source}} {
		result := runCLI(t, work, env, args...)
		requireExit(t, result, 0)
		if result.stderr != "" {
			t.Fatalf("stderr=%s", result.stderr)
		}
		if args[1] == "--observe" {
			if !strings.Contains(result.stdout, "decision completed") || strings.Contains(result.stdout, "yes true") {
				t.Fatalf("display=%s", result.stdout)
			}
			continue
		}
		var start, end bool
		for _, line := range strings.Split(strings.TrimSpace(result.stdout), "\n") {
			var event map[string]any
			if json.Unmarshal([]byte(line), &event) != nil {
				t.Fatalf("invalid JSONL %s", line)
			}
			if strings.HasPrefix(event["type"].(string), "decision.") && (event["model"] != nil || event["runId"] != nil || event["decisionModel"] != "decision/model") {
				t.Fatalf("invented agent metadata: %+v", event)
			}
			start = start || event["type"] == "decision.started"
			end = end || event["type"] == "decision.completed"
		}
		if !start || !end || !strings.Contains(result.stdout, "yes true") {
			t.Fatalf("missing results/events: %s", result.stdout)
		}
	}
}
