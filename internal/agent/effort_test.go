package agent

import (
	"context"
	"encoding/json"
	"testing"
)

func TestModelEffortMustMatchRunConfiguration(t *testing.T) {
	p := &fixtureProvider{}
	h := NewHost(Options{Provider: p})
	t.Cleanup(h.Close)
	op := h.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRunWithConfig(t, op, runConfig{
		Provider: "openrouter", Model: "fixture/model", Name: "high", MaxTokens: 128, Effort: "high",
	})
	for _, effort := range []string{"low", "auto", "", "unknown", "high"} {
		payload, err := json.Marshal(modelRequest{
			RunID: runID, Model: "fixture/model", MaxTokens: 128, Effort: effort,
			Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"task"}`)},
		})
		if err != nil {
			t.Fatal(err)
		}
		_, err = op.Call("model", string(payload))
		if effort == "high" {
			if err != nil {
				t.Fatalf("matching effort rejected: %v", err)
			}
		} else if err == nil || p.callCount() != 0 {
			t.Fatalf("changed/invalid effort reached provider: effort=%q err=%v calls=%d", effort, err, p.callCount())
		}
	}
	if p.callCount() != 1 || p.calls[0].Effort != "high" {
		t.Fatalf("run effort was not forwarded: %+v", p.calls)
	}
}
