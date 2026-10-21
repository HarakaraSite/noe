package runtime

import (
	"bytes"
	"context"
	"forge.harakara.site/littleisland/noe/v2/internal/noeprofile"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestManagedNOEDoesNotResolveControlUntilAgentAcceptance(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(dir, "missing-config.json"))
	options := Options{Agent: &noeprofile.AgentOptions{EnableControl: true, ControlDir: filepath.Join(blocked, "control")}}
	var out bytes.Buffer
	options.Stdout = &out
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "arithmetic.js"), `console.log(20+22)`}, options)
	if code != 0 || err != nil || out.String() != "42\n" {
		t.Fatalf("arithmetic code=%d err=%v out=%q", code, err, &out)
	}
	// Catalog reads configuration but neither credentials nor the control directory.
	t.Setenv("NOE_CONFIG_FILE", "")
	t.Setenv("XDG_CONFIG_HOME", dir)
	code, err = Execute(context.Background(), Source{filepath.Join(dir, "catalog.js"), `import {list_models} from "noe:agent";if((await list_models()).length===0)throw Error("empty catalog");`}, options)
	if code != 0 || err != nil {
		t.Fatalf("catalog code=%d err=%v", code, err)
	}
}

func TestManagedNOEControlActivationFailureCancelsAndJoinsHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	var started atomic.Bool
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		started.Store(true)
		defer close(finished)
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	})
	options := Options{Agent: agentTestOptions(p)}
	options.Agent.EnableControl = true
	options.Agent.ControlDir = filepath.Join(blocked, "control")
	code, err := Execute(ctx, Source{filepath.Join(dir, "agent.js"), `import {run_agent} from "noe:agent";await run_agent({provider:"openrouter",model:"fixture",effort:"none"},"wait");`}, options)
	if code == 0 || err == nil || !strings.Contains(Diagnostic(err), "runtime directory") {
		t.Fatalf("control failure code=%d err=%s", code, Diagnostic(err))
	}
	// Admission may be cancelled before the model begins. If it began, Execute
	// must have joined it; the owned Host has no work after this return.
	if started.Load() {
		select {
		case <-finished:
		default:
			t.Fatal("provider survived Host shutdown")
		}
	}
}
