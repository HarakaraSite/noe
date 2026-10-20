//go:build slow_integration

package runtime

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

// Run with `go test -tags=slow_integration ./internal/runtime -run '^TestAgentInternalRPCLongerThanFetchTimeout$' -count=1`.
func TestAgentInternalRPCLongerThanFetchTimeout(t *testing.T) {
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		timer := time.NewTimer(30*time.Second + 250*time.Millisecond)
		defer timer.Stop()
		select {
		case <-timer.C:
			return agentAnswer("long request completed"), nil
		case <-ctx.Done():
			return provider.Response{}, ctx.Err()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	var out bytes.Buffer
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "main.js"), `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openrouter",model:"x",effort:"auto"},"wait");console.log(result.lastResult.answer);`}, Options{Stdout: &out, Agent: agentTestOptions(p)})
	if code != 0 || err != nil || out.String() != "long request completed\n" {
		t.Fatalf("code=%d error=%v stdout=%q", code, err, &out)
	}
}
