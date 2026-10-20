package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCLITestMainIsolatesInheritedAgentConnection(t *testing.T) {
	if os.Getenv("NOE_TEST_ISOLATION_PROBE") == "1" {
		runCLIIsolationProbe(t)
		return
	}
	var calls atomic.Int32
	outer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "test suite reached the dummy outer agent", http.StatusForbidden)
	}))
	defer outer.Close()
	connection, err := json.Marshal(map[string]string{
		"url": outer.URL, "token": "dummy-outer-token", "scope": "dummy-outer-scope",
	})
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, value string }{
		{"empty", ""},
		{"invalid", "not-a-connection"},
		{"local-outer", string(connection)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			// Exercise TestMain in suite mode, not its managed "run" dispatch.
			// Use a focused root/child probe instead of rerunning unrelated suites.
			cmd := exec.CommandContext(ctx, executable, "-test.run=^TestCLITestMainIsolatesInheritedAgentConnection$", "-test.count=1")
			cmd.Env = append(os.Environ(), "NOE_AGENT_CONNECTION="+tc.value, "NOE_TEST_ISOLATION_PROBE=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated fixture suite: %v\n%s", err, output)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("dummy outer agent received %d requests", got)
			}
		})
	}
}

func runCLIIsolationProbe(t *testing.T) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		step := calls.Add(1)
		delta := map[string]any{"role": "assistant", "content": "done"}
		finish := "stop"
		if body.Model == "parent" && step == 1 {
			args, _ := json.Marshal(map[string]string{"code": "// Verify the fixture child uses its new broker connection.\nconst child=await run_agent({model:'child'},'child');if(child.lastResult?.answer!=='done')throw Error(JSON.stringify(child));"})
			delta = map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"index": 0, "id": "child", "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(args)}}}}
			finish = "tool_calls"
		}
		chunk, _ := json.Marshal(map[string]any{"id": "probe", "object": "chat.completion.chunk", "created": 1, "model": body.Model, "choices": []any{map[string]any{"index": 0, "delta": delta, "finish_reason": finish}}})
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", chunk)
	}))
	defer server.Close()
	agentFixtureSettings(t, server.URL)
	source := `import {run_agent} from "noe:agent";const root=await run_agent({provider:"openrouter",model:"parent",effort:"auto"},"root");if(root.lastResult?.answer!=="done")throw Error(JSON.stringify(root));console.log("isolated");`
	var out, diagnostic bytes.Buffer
	code := Run(t.Context(), []string{"eval", source}, strings.NewReader(""), &out, &diagnostic)
	if code != 0 || out.String() != "isolated\n" || diagnostic.Len() != 0 || calls.Load() != 3 {
		t.Fatalf("isolation probe code=%d stdout=%q stderr=%q local calls=%d", code, &out, &diagnostic, calls.Load())
	}
}
