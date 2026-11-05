package agent

import (
	"bytes"
	jsonv1 "encoding/json"
	json "encoding/json/v2"
	"fmt"
)

// Config contains inheritable public agent defaults. Empty fields are omitted
// so a root host exposes an empty object.
type Config struct {
	Provider               string           `json:"provider,omitempty"`
	Model                  string           `json:"model,omitempty"`
	Name                   string           `json:"name,omitempty"`
	System                 string           `json:"system,omitempty"`
	MaxTokens              int              `json:"maxTokens,omitempty,omitzero"`
	Effort                 string           `json:"effort,omitempty"`
	Cwd                    string           `json:"cwd,omitempty"`
	Hooks                  *HookModules     `json:"hooks,omitzero"`
	ContextBudget          *ContextBudget   `json:"contextBudget,omitempty"`
	ContextPolicy          *ContextPolicy   `json:"contextPolicy,omitempty"`
	ExecutionLimits        *ExecutionLimits `json:"executionLimits,omitempty"`
	contextBudgetPresent   bool
	contextPolicyPresent   bool
	executionLimitsPresent bool
}

type configAlias Config

func (c *Config) UnmarshalJSON(data []byte) error {
	var decoded configAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*c = Config(decoded)
	var fields map[string]jsonv1.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	_, c.contextBudgetPresent = fields["contextBudget"]
	if raw, exists := fields["contextPolicy"]; exists {
		c.contextPolicyPresent = true
		if !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			var policy ContextPolicy
			if err := json.Unmarshal(raw, &policy); err != nil {
				return err
			}
			c.ContextPolicy = &policy
		}
	}
	if raw, exists := fields["executionLimits"]; exists {
		if string(raw) == "null" {
			return fmt.Errorf("executionLimits must be an object")
		}
		c.executionLimitsPresent = true
	}
	return nil
}

func (c Config) MarshalJSON() ([]byte, error) {
	var rawBudget jsonv1.RawMessage
	if c.ContextBudget != nil {
		encoded, err := json.Marshal(c.ContextBudget)
		if err != nil {
			return nil, err
		}
		rawBudget = encoded
	} else if c.contextBudgetPresent {
		rawBudget = jsonv1.RawMessage("null")
	}
	var rawExecutionLimits jsonv1.RawMessage
	if c.ExecutionLimits != nil {
		encoded, err := json.Marshal(c.ExecutionLimits)
		if err != nil {
			return nil, err
		}
		rawExecutionLimits = encoded
	} else if c.executionLimitsPresent {
		rawExecutionLimits = jsonv1.RawMessage("null")
	}
	var rawContextPolicy jsonv1.RawMessage
	if c.ContextPolicy != nil {
		encoded, err := json.Marshal(c.ContextPolicy)
		if err != nil {
			return nil, err
		}
		rawContextPolicy = encoded
	} else if c.contextPolicyPresent {
		rawContextPolicy = jsonv1.RawMessage("null")
	}
	return json.Marshal(struct {
		Provider        string            `json:"provider,omitempty"`
		Model           string            `json:"model,omitempty"`
		Name            string            `json:"name,omitempty"`
		System          string            `json:"system,omitempty"`
		MaxTokens       int               `json:"maxTokens,omitempty,omitzero"`
		Effort          string            `json:"effort,omitempty"`
		Cwd             string            `json:"cwd,omitempty"`
		Hooks           *HookModules      `json:"hooks,omitzero"`
		ContextBudget   jsonv1.RawMessage `json:"contextBudget,omitzero"`
		ContextPolicy   jsonv1.RawMessage `json:"contextPolicy,omitzero"`
		ExecutionLimits jsonv1.RawMessage `json:"executionLimits,omitzero"`
	}{c.Provider, c.Model, c.Name, c.System, c.MaxTokens, c.Effort, c.Cwd, c.Hooks, rawBudget, rawContextPolicy, rawExecutionLimits})
}

// HookModules keeps Config comparable for callers that use it as a settings
// value while retaining an array in its JSON representation.
type HookModules []string

func cloneHookModules(modules []string) *HookModules {
	if len(modules) == 0 {
		return nil
	}
	copy := HookModules(append([]string(nil), modules...))
	return &copy
}

func (c Config) hookModules() []string {
	if c.Hooks == nil {
		return nil
	}
	return append([]string(nil), (*c.Hooks)...)
}
