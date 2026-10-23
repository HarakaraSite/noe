package agent_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	agentlog "forge.harakara.site/littleisland/noe/v2/agent/log"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Verify the same independent initializer in root, task hooks, and tools.
func TestInitializerSuppliesAuthAndLogToRootTaskAndTool(t *testing.T) {
	ctx := testContext(t)
	var mu sync.Mutex
	initialized, cleaned := 0, 0
	seen := map[string]int{}
	check := `import {analyze_log} from "noe:agent-log";
import {save_chatgpt_credentials} from "noe:auth";
function check(where){
 if(typeof save_chatgpt_credentials!=="function") throw Error("auth unavailable");
 const s=analyze_log('{"executionId":"fixture","seq":1,"type":"agent.started","runId":"r"}\n',{runId:"r"});
 if(s.status!=="running"||s.runId!=="r") throw Error("analysis unavailable");
 initializerSeen(where);
}
`
	profile := stdlib.Profile{
		Initialize: func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
			if view.Worker() {
				return nil, nil
			}
			a, err := auth.Bind(ctx, view, auth.Options{})
			if err != nil {
				return nil, err
			}
			l, err := agentlog.Bind(ctx, view)
			if err != nil {
				a.Close()
				return nil, err
			}
			if err := view.Runtime().Set("initializerSeen", func(where string) { mu.Lock(); seen[where]++; mu.Unlock() }); err != nil {
				l.Close()
				a.Close()
				return nil, err
			}
			mu.Lock()
			initialized++
			mu.Unlock()
			return func() { l.Close(); a.Close(); mu.Lock(); cleaned++; mu.Unlock() }, nil
		},
		NewLoader: func(context.Context, stdlib.Origin) (quickjs.ModuleLoader, error) {
			return func(specifier, referrer string) (string, string, error) {
				if specifier == "fixture:extras-hook" {
					return check + `export function beforeModelRequest(){check("task");}`, "memory:/extras-hook.mjs", nil
				}
				return "", "", fmt.Errorf("unexpected import %q from %q", specifier, referrer)
			}, nil
		},
	}
	models := &fixtureModels{generate: func(_ context.Context, r model.Request) (model.Response, error) {
		if !runtimeHasToolReply(r, "extras") {
			return runtimeToolResponse("extras", check+`check("tool");`), nil
		}
		return runtimeFinalResponse("complete"), nil
	}}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_, err = stdlib.Execute(ctx, stdlib.Source{Name: "[extras root]", Virtual: true, Module: true, Code: check + `check("root");`}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
	hooks := agent.HookModules{"fixture:extras-hook"}
	config := instructionConfig("extras", "", hooks)
	result, err := client.Run(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("extras")})
	if err != nil || result.LastResult == nil || result.LastResult.State != "completed" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	client.Close()
	mu.Lock()
	defer mu.Unlock()
	if initialized != 3 || cleaned != 3 || seen["root"] != 1 || seen["tool"] != 1 || seen["task"] != 2 {
		t.Fatalf("init/cleanup=%d/%d seen=%v", initialized, cleaned, seen)
	}
}
