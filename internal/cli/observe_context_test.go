package cli

import (
	"bytes"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
)

func TestObserveContextAndHookProgress(t *testing.T) {
	input := `{"type":"context.updated","name":"parent","runId":"123456789","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":null},"previous":null,"estimate":{"tokens":125,"stage":"preparing"},"remainingTokens":875,"usedPercent":12.5}}
{"type":"hook.started","name":"parent","runId":"123456789","phase":"beforeModelRequest","module":"/tmp/hook.mjs"}
{"type":"context.updated","name":"parent","runId":"123456789","context":{"budget":{"inputTokens":1000},"modelWindow":{"tokens":10000},"previous":{"requestId":"request-123","status":"completed","promptTokens":850,"cachedTokens":800},"estimate":{"tokens":150,"stage":"sent"},"remainingTokens":850,"usedPercent":15}}
{"type":"stdout","name":"parent","runId":"123456789","text":"hidden command output"}
{"type":"hook.failed","name":"parent","runId":"123456789","phase":"afterTool","module":"/tmp/hook.mjs","error":"fixture error"}
{"type":"execution.limit_reached","name":"parent","runId":"123456789","reason":"execution_limit","limitName":"providerCalls","used":5,"limit":5}
`
	var out, stderr bytes.Buffer
	if code := Run(t.Context(), []string{"observe"}, strings.NewReader(input), &out, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, &stderr)
	}
	for _, want := range []string{"[parent run=12345678]\ncontext:12.5%", "hook started: beforeModelRequest / /tmp/hook.mjs", "context:15.0%", "hook failed: afterTool / /tmp/hook.mjs / fixture error", "execution limit reached: providerCalls / used=5 / limit=5"} {
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
		"context:87.5%",
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

func TestObserveContextPercentOnly(t *testing.T) {
	for _, test := range []struct {
		name, context, want string
	}{
		{"execution", `{"budget":{"inputTokens":943718},"modelWindow":{"tokens":1048576},"estimate":{"tokens":6332,"stage":"sent"},"remainingTokens":937386,"previous":{"promptTokens":4376,"cachedTokens":0,"status":"completed"},"usedPercent":0.7,"executionContext":{"estimatedTokens":1612,"budgetTokens":938998,"usedPercent":0.17167,"budgetSource":"fallback_window_cap"}}`, "0.2%"},
		{"legacy", `{"budget":{"inputTokens":1000},"usedPercent":12.5}`, "12.5%"},
		{"zero", `{"executionContext":{"usedPercent":0}}`, "0.0%"},
		{"over budget", `{"executionContext":{"usedPercent":125}}`, "125.0%"},
		{"unknown execution", `{"usedPercent":12.5,"executionContext":{"usedPercent":null}}`, "unknown"},
		{"missing", `{}`, "unknown"},
		{"null", `null`, "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := `{"type":"context.updated","name":"worker","runId":"12345678-full","context":` + test.context + "}\n"
			var out, stderr bytes.Buffer
			if code := Run(t.Context(), []string{"observe"}, strings.NewReader(input), &out, &stderr); code != 0 {
				t.Fatalf("exit %d: %s", code, &stderr)
			}
			if got, want := out.String(), "[worker run=12345678]\ncontext:"+test.want+"\n"; got != want {
				t.Fatalf("context display = %q, want %q", got, want)
			}
		})
	}
}
