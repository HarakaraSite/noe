package cli

import (
	"bytes"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestObserveContextAndHookProgress(t *testing.T) {
	input := `{"type":"context.updated","name":"parent","runId":"123456789","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":null},"previous":null,"estimate":{"tokens":125,"stage":"preparing"},"remainingTokens":875}}
{"type":"hook.started","name":"parent","runId":"123456789","phase":"beforeModelRequest","module":"/tmp/hook.mjs"}
{"type":"context.updated","name":"parent","runId":"123456789","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":10000},"previous":{"requestId":"request-123","status":"completed","promptTokens":850,"cachedTokens":800},"estimate":{"tokens":150,"stage":"sent"},"remainingTokens":850}}
{"type":"stdout","name":"parent","runId":"123456789","text":"hidden command output"}
{"type":"hook.failed","name":"parent","runId":"123456789","phase":"afterTool","module":"/tmp/hook.mjs","error":"fixture error"}
{"type":"execution.limit_reached","name":"parent","runId":"123456789","reason":"execution_limit","limitName":"providerCalls","used":5,"limit":5}
`
	var out, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"observe"}, strings.NewReader(input), &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	for _, want := range []string{"[parent/12345678] context: budget=1000 / window=unknown / estimated=125 (preparing) / remaining=875 / previous=unknown", "hook started: beforeModelRequest / /tmp/hook.mjs", "previous=850 measured [request-] / cached=800 / completed", "hook failed: afterTool / /tmp/hook.mjs / fixture error", "execution limit reached: providerCalls / used=5 / limit=5"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "hidden command output") {
		t.Fatal("command output became visible")
	}
	// Typed live snapshots follow the same renderer as JSONL events.
	value := struct {
		Budget struct {
			InputTokens *int `json:"inputTokens"`
		} `json:"budget"`
	}{}
	if err := newAgentRenderer(&out).render(events.Event{"type": "context.updated", "context": value}); err != nil {
		t.Fatal(err)
	}
}

func TestObserveContextTrimming(t *testing.T) {
	input := `{"type":"context.updated","name":"worker","runId":"run-12345678","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":null},"previous":null,"estimate":{"tokens":850,"stage":"preparing"},"remainingTokens":150,"executionContext":{"policy":"auto","budgetSource":"fallback","estimatedTokens":700,"budgetTokens":800,"usedPercent":87.5,"pendingTrim":{"id":"activity-1","beforeTokens":1200,"afterTokens":700,"removedBlocks":6}}}}
{"type":"stdout","name":"worker","runId":"run-12345678","text":"hidden tool stdout"}
{"type":"context.trimmed","name":"worker","runId":"run-12345678","activityId":"activity-1","beforeTokens":1200,"afterTokens":700,"removedBlocks":6,"reason":"input_budget","budgetSource":"fallback","text":"must not be shown"}
`
	var out, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"observe"}, strings.NewReader(input), &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	for _, want := range []string{
		"context: budget=1000 / window=unknown / estimated=850 (preparing) / remaining=150 / previous=unknown / execution=~700/800 tokens (87.5%, source=fallback)",
		"context trimmed: ~1200 -> ~700 tokens / removed blocks=6 / reason=input_budget / source=fallback",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "hidden tool stdout") || strings.Contains(out.String(), "must not be shown") {
		t.Fatalf("raw output was rendered: %s", &out)
	}
	if strings.Count(out.String(), "context trimmed:") != 1 {
		t.Fatalf("trim notification should be rendered only from context.trimmed: %s", &out)
	}
}

func TestObserveComputedBudgetWithoutExplicitBudgetOrWindow(t *testing.T) {
	input := `{"type":"context.updated","context":{"budget":{"inputTokens":null},"modelWindow":{"tokens":null},"estimate":{"tokens":6000,"stage":"sent"},"remainingTokens":null,"executionContext":{"budgetSource":"fallback","estimatedTokens":1000,"budgetTokens":995000,"totalInputBudgetTokens":1000000}}}
{"type":"context.updated","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":2000},"estimate":{"tokens":850,"stage":"sent"},"remainingTokens":150,"executionContext":{"budgetSource":"combined","estimatedTokens":700,"budgetTokens":800,"totalInputBudgetTokens":900}}}
`
	var out, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"observe"}, strings.NewReader(input), &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	for _, want := range []string{
		"budget=1000000 / window=unknown / estimated=6000 (sent) / remaining=994000",
		"budget=900 / window=2000 / estimated=850 (sent) / remaining=50",
		"explicitBudget=1000",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing %q in %s", want, &out)
		}
	}
	if strings.Contains(out.String(), "budget=unknown") {
		t.Fatalf("computed budget was hidden: %s", &out)
	}
}
