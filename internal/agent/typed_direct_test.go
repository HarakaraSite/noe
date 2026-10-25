package agent

import (
	"encoding/json"
	"testing"
)

func TestSnapshotTypedSpawnRequestCopiesInputAndPreservesConfigPresence(t *testing.T) {
	var request SpawnRequest
	if err := json.Unmarshal([]byte(`{"config":{"provider":"fixture","model":"fixture-model","name":"fixture","effort":"none","maxSteps":3,"contextBudget":null,"contextPolicy":null,"executionLimits":{},"hooks":[]},"instruction":"first","allowDelegation":false}`), &request); err != nil {
		t.Fatal(err)
	}
	if !request.Config.contextBudgetPresent || !request.Config.contextPolicyPresent || !request.Config.executionLimitsPresent {
		t.Fatalf("fixture did not set config presence bits: %+v", request.Config)
	}
	cacheKey := "cache"
	request.Config.CacheKey = &cacheKey
	budget := &ContextBudget{InputTokens: 10}
	request.Config.ContextBudget = budget
	ratio, reserve, starts := 0.5, int64(8), int64(2)
	request.Config.ContextPolicy = &ContextPolicy{RetainRatio: &ratio, ResponseReserveTokens: &reserve}
	request.Config.ExecutionLimits = &ExecutionLimits{AgentStarts: &starts}
	hooks := *request.Config.Hooks
	hooks = append(hooks, "before")
	request.Config.Hooks = &hooks
	instruction, allow := "first", false
	request.Instruction, request.AllowDelegation = &instruction, &allow

	snapshotValue, err := snapshotTypedRequest("spawn_agent", request)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := snapshotValue.(SpawnRequest)
	if !snapshot.Config.contextBudgetPresent || !snapshot.Config.contextPolicyPresent || !snapshot.Config.executionLimitsPresent {
		t.Fatalf("snapshot lost config presence bits: %+v", snapshot.Config)
	}
	if snapshot.Config.Hooks == nil || len(*snapshot.Config.Hooks) != 1 || (*snapshot.Config.Hooks)[0] != "before" {
		t.Fatalf("snapshot hooks = %#v", snapshot.Config.Hooks)
	}

	cacheKey = "changed"
	budget.InputTokens = 20
	ratio, reserve, starts = 0.9, 12, 0
	hooks[0] = "changed"
	instruction, allow = "changed", true
	if *snapshot.Config.CacheKey != "cache" || snapshot.Config.ContextBudget.InputTokens != 10 || *snapshot.Config.ContextPolicy.RetainRatio != 0.5 || *snapshot.Config.ContextPolicy.ResponseReserveTokens != 8 || *snapshot.Config.ExecutionLimits.AgentStarts != 2 || (*snapshot.Config.Hooks)[0] != "before" || *snapshot.Instruction != "first" || *snapshot.AllowDelegation {
		t.Fatalf("typed request shares caller-owned input: %+v", snapshot)
	}

	nilHooks := HookModules(nil)
	request.Config.Hooks = &nilHooks
	snapshotValue, err = snapshotTypedRequest("spawn_agent", request)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshotValue.(SpawnRequest).Config.Hooks; got != nil {
		t.Fatalf("JSON null hooks would decode as nil, got %#v", got)
	}
}

func TestValidateTypedTaskConfigMatchesStructuredJSONValidation(t *testing.T) {
	negative, tooLarge, zero, invalidRatio := int64(-1), maxSafeContextBudget+1, int64(0), 1.0
	tests := []struct {
		name   string
		config TaskConfig
	}{
		{name: "context budget", config: TaskConfig{Config: Config{ContextBudget: &ContextBudget{InputTokens: 0}}}},
		{name: "context policy ratio", config: TaskConfig{Config: Config{ContextPolicy: &ContextPolicy{RetainRatio: &invalidRatio}}}},
		{name: "context policy reserve", config: TaskConfig{Config: Config{ContextPolicy: &ContextPolicy{ResponseReserveTokens: &zero}}}},
		{name: "execution limit", config: TaskConfig{Config: Config{ExecutionLimits: &ExecutionLimits{AgentStarts: &negative}}}},
		{name: "execution limit safe integer", config: TaskConfig{Config: Config{ExecutionLimits: &ExecutionLimits{HookCalls: &tooLarge}}}},
		{name: "explicit null execution limits", config: TaskConfig{Config: Config{executionLimitsPresent: true}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateTypedTaskConfig(test.config); err == nil {
				t.Fatal("invalid config was accepted")
			}
		})
	}
	if err := validateTypedTaskConfig(TaskConfig{Config: Config{ExecutionLimits: &ExecutionLimits{AgentStarts: &zero}}}); err != nil {
		t.Fatalf("zero execution limit should be valid: %v", err)
	}
}

func TestTypedResponseSnapshotsCopyStateAndProjectOmittedAnswers(t *testing.T) {
	usageTotal, answerBytes, contextBudget := int64(14), 6, int64(100)
	answer := TaskOutcome{omitAnswer: true, Answer: "secret", AnswerBytes: &answerBytes, HookFailures: []HookFailure{{Error: "kept"}}}
	snapshot := InstanceSnapshot{
		CausedBy:   []string{"activity"},
		Execution:  &executionSnapshot{Usage: &usageTotals{TotalTokens: &usageTotal}},
		LastResult: &answer,
		Context:    &contextSnapshot{Budget: contextBudgetSnapshot{InputTokens: &contextBudget}},
	}
	copy := cloneInstanceSnapshot(snapshot)
	if copy.LastResult == nil || copy.LastResult.Answer != "" || copy.LastResult.AnswerBytes == nil || *copy.LastResult.AnswerBytes != len("secret") || copy.LastResult.omitAnswer {
		t.Fatalf("omitted answer projection = %+v", copy.LastResult)
	}
	*copy.Execution.Usage.TotalTokens = 99
	*copy.Context.Budget.InputTokens = 200
	*copy.LastResult.AnswerBytes = 1
	copy.CausedBy[0] = "changed"
	copy.LastResult.HookFailures[0].Error = "changed"
	if answer.HookFailures[0].Error != "kept" {
		t.Fatal("hook failures alias retained outcome")
	}
	if usageTotal != 14 || contextBudget != 100 || answerBytes != 6 || snapshot.CausedBy[0] != "activity" {
		t.Fatalf("typed result aliases retained state: original=%+v", snapshot)
	}
}
