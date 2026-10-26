package agent_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestGoHostURLImportFactsMatchSnapshottedProfile(t *testing.T) {
	for _, tc := range []struct {
		name    string
		profile stdlib.Profile
		support string
	}{
		{"enabled", stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
			Imports: map[string]string{"hidden": "https://example.invalid/library?secret-fixture"},
			HTTP:    &stdlib.ModuleHTTPOptions{},
		})}, "enabled"},
		{"disabled-even-with-fetch", stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{}),
			Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, "disabled"},
		{"legacy-opaque", stdlib.Profile{NewLoader: stdlib.FileLoader(nil)}, "unknown"},
		{"no-loader", stdlib.Profile{}, "disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testContext(t)
			models := new(fixtureModels)
			profile := tc.profile
			_, client := instructionHost(t, ctx, models, nil, "", &profile)
			// The Host's template and facts must not follow later caller edits.
			profile.Loader, profile.NewLoader = nil, nil
			result, err := client.Run(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("inspect capabilities")})
			if err != nil || result.LastResult == nil || result.LastResult.State != "completed" {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			models.mu.Lock()
			requests := append([]model.Request(nil), models.requests...)
			models.mu.Unlock()
			if len(requests) != 1 {
				t.Fatalf("model calls=%d", len(requests))
			}
			system := instructionSystem(requests[0])
			if !strings.Contains(system, core.URLImportFacts(tc.support)) || strings.Contains(system, "secret-fixture") {
				t.Fatalf("capability facts mismatch or leaked configuration: %s", system)
			}
		})
	}
}

func TestGoHostTaskCancellationStopsURLBodyRead(t *testing.T) {
	entered, disconnected := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/javascript")
		w.Write([]byte("export const value = "))
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(disconnected)
	}))
	defer server.Close()
	ctx := testContext(t)
	models := &fixtureModels{generate: func(context.Context, model.Request) (model.Response, error) {
		return runtimeToolResponse("url-body", `await import("remote/hanging");`), nil
	}}
	profile := stdlib.Profile{Loader: stdlib.ModuleLoader(stdlib.LoaderOptions{
		Imports: map[string]string{"remote/hanging": server.URL + "/hanging"},
		HTTP:    &stdlib.ModuleHTTPOptions{Client: server.Client()},
	})}
	_, client := instructionHost(t, ctx, models, nil, "", &profile)
	started, err := client.Spawn(ctx, agent.SpawnRequest{Config: taskConfig(), Instruction: textPointer("cancel URL load")})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := client.CancelTask(ctx, agent.CancelTaskRequest(started)); err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: started.AgentID, CommandID: started.CommandID})
	if err != nil || outcome.State != "cancelled" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	select {
	case <-disconnected:
	case <-ctx.Done():
		t.Fatal("task cancellation did not close URL response body")
	}
}
