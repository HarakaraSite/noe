package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"
)

func TestCLITestMainIsolatesInheritedAgentConnection(t *testing.T) {
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
			// The fixtures assert local provider calls, then delegate through a
			// freshly generated connection to a real child process. Selecting
			// only these tests also prevents this regression from recursing.
			cmd := exec.CommandContext(ctx, executable, "-test.run=^(TestCLIAgentRunAndEval|TestCLIConfiguredProviderParentChildRunIDs)$", "-test.count=1")
			cmd.Env = append(os.Environ(), "NOE_AGENT_CONNECTION="+tc.value)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("isolated fixture suite: %v\n%s", err, output)
			}
			if got := calls.Load(); got != 0 {
				t.Fatalf("dummy outer agent received %d requests", got)
			}
		})
	}
}
