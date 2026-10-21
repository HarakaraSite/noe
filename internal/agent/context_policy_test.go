package agent

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
)

func TestContextPolicyStrictValidation(t *testing.T) {
	valid := []string{
		`{}`,
		`{"retainRatio":0.1}`,
		`{"retainRatio":0.999999,"responseReserveTokens":9007199254740991}`,
		`{"responseReserveTokens":1}`,
	}
	for _, input := range valid {
		var policy ContextPolicy
		if err := json.Unmarshal([]byte(input), &policy); err != nil {
			t.Errorf("valid policy %s rejected: %v", input, err)
		}
	}
	invalid := []string{
		`null`, `[]`, `"auto"`, `{"unknown":1}`,
		`{"retainRatio":null}`, `{"retainRatio":"0.1"}`, `{"retainRatio":0}`,
		`{"retainRatio":1}`, `{"retainRatio":-0.1}`, `{"retainRatio":1e400}`,
		`{"responseReserveTokens":null}`, `{"responseReserveTokens":"12"}`,
		`{"responseReserveTokens":1.5}`, `{"responseReserveTokens":0}`,
		`{"responseReserveTokens":-1}`, `{"responseReserveTokens":9007199254740992}`,
	}
	for _, input := range invalid {
		var policy ContextPolicy
		if err := json.Unmarshal([]byte(input), &policy); err == nil {
			t.Errorf("invalid policy %s accepted", input)
		}
	}
}

func TestContextPolicyConfigNullAndObjectRoundTrip(t *testing.T) {
	for _, test := range []struct {
		input       string
		wantPresent bool
		wantNull    bool
	}{
		{input: `{}`},
		{input: `{"contextPolicy":null}`, wantPresent: true, wantNull: true},
		{input: `{"contextPolicy":{}}`, wantPresent: true},
		{input: `{"contextPolicy":{"retainRatio":0.2,"responseReserveTokens":4096}}`, wantPresent: true},
	} {
		var config Config
		if err := json.Unmarshal([]byte(test.input), &config); err != nil {
			t.Fatalf("decode %s: %v", test.input, err)
		}
		if config.contextPolicyPresent != test.wantPresent {
			t.Errorf("%s presence = %v, want %v", test.input, config.contextPolicyPresent, test.wantPresent)
		}
		if test.wantNull && config.ContextPolicy != nil {
			t.Errorf("%s policy = %#v, want disabled nil", test.input, config.ContextPolicy)
		}
		encoded, err := json.Marshal(config)
		if err != nil {
			t.Fatalf("marshal %s: %v", test.input, err)
		}
		if test.wantNull && !strings.Contains(string(encoded), `"contextPolicy":null`) {
			t.Errorf("explicit null lost on round trip: %s", encoded)
		}
		if !test.wantPresent && strings.Contains(string(encoded), `"contextPolicy"`) {
			t.Errorf("omitted policy was added on round trip: %s", encoded)
		}
		if test.wantPresent && !test.wantNull && !strings.Contains(string(encoded), `"contextPolicy":{`) {
			t.Errorf("object policy lost on round trip: %s", encoded)
		}
	}

	var config Config
	if err := json.Unmarshal([]byte(`{"contextPolicy":{"retainRatio":0.2,"responseReserveTokens":4096}}`), &config); err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(taskConfig{Config: config, MaxSteps: 3})
	if err != nil {
		t.Fatal(err)
	}
	var decoded taskConfig
	if err := json.Unmarshal(encoded, &decoded); err != nil || !decoded.contextPolicyPresent || decoded.ContextPolicy == nil || decoded.MaxSteps != 3 {
		t.Fatalf("task config round trip = %#v, error %v", decoded, err)
	}

}

func TestResolveExecutionBudgetSourcesAndWindowReserve(t *testing.T) {
	window := int64(1_000_000)
	reserve := int64(8_192)
	policy := &ContextPolicy{ResponseReserveTokens: &reserve}
	got := resolveExecutionBudget(nil, &window, 0, nil, policy, 10_000, 500)
	if got.BudgetSource != "model_window" || got.TotalInputTokens != 893_677 || got.ExecutionTokens != 883_177 {
		t.Fatalf("known model window budget = %#v", got)
	}
	if got.ResponseReserveTokens == nil || *got.ResponseReserveTokens != reserve || got.ResponseReserveUnknown {
		t.Fatalf("known reserve missing: %#v", got)
	}

	explicit := &ContextBudget{InputTokens: 800_000}
	got = resolveExecutionBudget(explicit, &window, 0, nil, policy, 10_000, 500)
	if got.BudgetSource != "combined" || got.TotalInputTokens != explicit.InputTokens || got.ExecutionTokens != 789_500 {
		t.Fatalf("combined explicit/window budget = %#v", got)
	}

	unknownReserveWindow := int64(100_000)
	got = resolveExecutionBudget(&ContextBudget{InputTokens: 95_000}, &unknownReserveWindow, 0, nil, nil, 10_000, 0)
	if got.BudgetSource != "combined" || got.TotalInputTokens != 90_000 || got.ExecutionTokens != 80_000 || !got.ResponseReserveUnknown {
		t.Fatalf("unknown-reserve capped budget = %#v", got)
	}

	got = resolveExecutionBudget(nil, nil, 0, nil, nil, 10_000, 500)
	if got.BudgetSource != "fallback" || got.TotalInputTokens != defaultContextInputTokens || got.ExecutionTokens != 989_500 || !got.ResponseReserveUnknown {
		t.Fatalf("unknown model fallback = %#v", got)
	}
	got = resolveExecutionBudget(&ContextBudget{InputTokens: 500_000}, nil, 0, nil, nil, 10_000, 500)
	if got.BudgetSource != "input_budget" || got.TotalInputTokens != 500_000 || got.ExecutionTokens != 489_500 {
		t.Fatalf("explicit budget without window = %#v", got)
	}
	zeroWindow := int64(0)
	got = resolveExecutionBudget(nil, &zeroWindow, 0, nil, nil, 10_000, 500)
	if got.BudgetSource != "fallback" || got.WindowTokens != nil || got.TotalInputTokens != defaultContextInputTokens {
		t.Fatalf("zero window should be treated as unknown: %#v", got)
	}
}

func TestResolveExecutionBudgetReservePrecedenceAndUnreachableFixedInput(t *testing.T) {
	window := int64(50_000)
	policyReserve := int64(12_000)
	catalogReserve := int64(24_000)
	policy := &ContextPolicy{ResponseReserveTokens: &policyReserve}
	got := resolveExecutionBudget(nil, &window, 16_000, &catalogReserve, policy, 60_000, 2_000)
	if got.ResponseReserveTokens == nil || *got.ResponseReserveTokens != 16_000 || got.TotalInputTokens != 62_000 || got.ExecutionTokens != 0 {
		t.Fatalf("fixed-input-over-window budget = %#v", got)
	}

	got = resolveExecutionBudget(nil, &window, 0, &catalogReserve, policy, 1_000, 0)
	if got.ResponseReserveTokens == nil || *got.ResponseReserveTokens != policyReserve {
		t.Fatalf("policy reserve should override catalog reserve: %#v", got)
	}
	got = resolveExecutionBudget(nil, &window, 0, &catalogReserve, nil, 1_000, 0)
	if got.ResponseReserveTokens == nil || *got.ResponseReserveTokens != catalogReserve {
		t.Fatalf("catalog reserve not used when other reserves are absent: %#v", got)
	}

	max := int64(maxSafeContextBudget)
	if got := floorNinetyPercent(max); got != (max/10)*9+(max%10)*9/10 {
		t.Fatalf("safe-integer floor calculation = %d", got)
	}
	if math.IsNaN(effectiveContextRetainRatio(nil)) || effectiveContextRetainRatio(nil) != 0.1 {
		t.Fatalf("default retain ratio = %v", effectiveContextRetainRatio(nil))
	}
	ratio := 0.2
	if got := effectiveContextRetainRatio(&ContextPolicy{RetainRatio: &ratio}); got != ratio {
		t.Fatalf("configured retain ratio = %v, want %v", got, ratio)
	}
}

func TestContextCatalogOutputReserveIsResolvedPerProviderAndModel(t *testing.T) {
	windowA := int64(128_000)
	windowB := int64(256_000)
	catalog := agentTestModelService{catalog: []model.Model{
		{Provider: "fixture-a", Model: "model-a", Kind: "agent", ContextWindow: windowA, MaxOutputTokens: 12_000},
		{Provider: "fixture-a", Model: "model-b", Kind: "agent", ContextWindow: windowA, MaxOutputTokens: 24_000},
		{Provider: "fixture-b", Model: "model-a", Kind: "agent", ContextWindow: windowB, MaxOutputTokens: 36_000},
	}}
	h := NewHost(Options{Models: catalog})
	defer h.Close()
	for _, test := range []struct {
		provider string
		model    string
		want     int64
	}{
		{provider: "fixture-a", model: "model-a", want: 12_000},
		{provider: "fixture-a", model: "model-b", want: 24_000},
		{provider: "fixture-b", model: "model-a", want: 36_000},
	} {
		gotWindow, gotOutput := h.contextModelLimits(test.provider, test.model)
		if gotOutput == nil || *gotOutput != test.want {
			t.Errorf("catalog reserve %s/%s = %v, want %d", test.provider, test.model, gotOutput, test.want)
		}
		wantWindow := windowA
		if test.provider == "fixture-b" {
			wantWindow = windowB
		}
		if gotWindow == nil || *gotWindow != wantWindow {
			t.Errorf("catalog window %s/%s = %v, want %d", test.provider, test.model, gotWindow, wantWindow)
		}
	}
	if gotWindow, gotOutput := h.contextModelLimits("fixture-a", "unknown"); gotWindow != nil || gotOutput != nil {
		t.Errorf("unknown model metadata = %v/%v, want unknown", gotWindow, gotOutput)
	}
}
