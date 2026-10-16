package agent

import (
	"context"
	"strings"
	"testing"
)

func TestChildEnvironmentFiltersSecretsAndReplacesConnection(t *testing.T) {
	parent := []string{
		"PATH=/fixture/bin",
		"OPENROUTER_API_KEY=fake-openrouter-value",
		"OPENAI_API_KEY=fake-openai-value",
		"ANTHROPIC_API_KEY=fake-anthropic-value",
		"NOE_OPENROUTER_API_KEY=fake-noe-value",
		"NOE_AGENT_CONNECTION=stale-capability",
		"NOE_OPENROUTER_API_KEY_FILE=/fixture/key-file",
	}

	withoutConnection, err := childEnvironment(parent, nil)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(withoutConnection, "\n")
	if !strings.Contains(joined, "PATH=/fixture/bin") || !strings.Contains(joined, "NOE_OPENROUTER_API_KEY_FILE=/fixture/key-file") {
		t.Fatalf("ordinary environment entries were not retained: %q", joined)
	}
	for _, forbidden := range []string{
		"OPENROUTER_API_KEY=", "OPENAI_API_KEY=", "ANTHROPIC_API_KEY=",
		"NOE_OPENROUTER_API_KEY=", "NOE_AGENT_CONNECTION=",
	} {
		if strings.Contains(joined, forbidden) {
			t.Errorf("child environment retained %q: %q", forbidden, joined)
		}
	}

	connection := &Connection{
		URL: "http://127.0.0.1:12345", Token: "fixture-capability", Scope: "fixture-scope",
		Defaults: Config{Provider: "openrouter", Model: "fixture/model"},
	}
	withConnection, err := childEnvironment(parent, connection)
	if err != nil {
		t.Fatal(err)
	}
	joined = strings.Join(withConnection, "\n")
	if strings.Count(joined, "NOE_AGENT_CONNECTION=") != 1 || !strings.Contains(joined, `NOE_AGENT_CONNECTION={"url":"http://127.0.0.1:12345","token":"fixture-capability","scope":"fixture-scope","defaults":{"provider":"openrouter","model":"fixture/model"}}`) {
		t.Fatalf("child connection was not replaced with the scoped capability: %q", joined)
	}
}

func TestRunnerDoesNotLaunchWhenContextIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (Runner{}).Run(ctx, "throw new Error('unreachable');")
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != "cancelled" || result.ExitCode != -1 {
		t.Fatalf("pre-cancel result = %#v, want cancelled with no process exit code", result)
	}
}
