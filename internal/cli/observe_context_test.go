package cli

import (
	"bytes"
	"strings"
	"testing"

	"noe/internal/events"
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
