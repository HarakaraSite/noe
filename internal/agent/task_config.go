package agent

import "encoding/json"

// taskConfig is frozen at registration and copied into each fresh task runner.
type taskConfig struct {
	Config
	MaxSteps int     `json:"maxSteps"`
	CacheKey *string `json:"cacheKey,omitempty"`
}

func (c taskConfig) MarshalJSON() ([]byte, error) {
	var rawBudget json.RawMessage
	if c.Config.ContextBudget != nil {
		encoded, err := json.Marshal(c.Config.ContextBudget)
		if err != nil {
			return nil, err
		}
		rawBudget = encoded
	} else if c.Config.contextBudgetPresent {
		rawBudget = json.RawMessage("null")
	}
	var rawContextPolicy json.RawMessage
	if c.Config.ContextPolicy != nil {
		encoded, err := json.Marshal(c.Config.ContextPolicy)
		if err != nil {
			return nil, err
		}
		rawContextPolicy = encoded
	} else if c.Config.contextPolicyPresent {
		rawContextPolicy = json.RawMessage("null")
	}
	return json.Marshal(struct {
		Provider      string          `json:"provider,omitempty"`
		Model         string          `json:"model,omitempty"`
		Name          string          `json:"name,omitempty"`
		System        string          `json:"system,omitempty"`
		MaxTokens     int             `json:"maxTokens,omitempty"`
		Effort        string          `json:"effort,omitempty"`
		Cwd           string          `json:"cwd,omitempty"`
		Hooks         *HookModules    `json:"hooks,omitempty"`
		ContextBudget json.RawMessage `json:"contextBudget,omitempty"`
		ContextPolicy json.RawMessage `json:"contextPolicy,omitempty"`
		MaxSteps      int             `json:"maxSteps"`
		CacheKey      *string         `json:"cacheKey,omitempty"`
	}{c.Config.Provider, c.Config.Model, c.Config.Name, c.Config.System, c.Config.MaxTokens, c.Config.Effort, c.Config.Cwd, c.Config.Hooks, rawBudget, rawContextPolicy, c.MaxSteps, c.CacheKey})
}

func (c *taskConfig) UnmarshalJSON(data []byte) error {
	if err := json.Unmarshal(data, &c.Config); err != nil {
		return err
	}
	var extra struct {
		MaxSteps int     `json:"maxSteps"`
		CacheKey *string `json:"cacheKey"`
	}
	if err := json.Unmarshal(data, &extra); err != nil {
		return err
	}
	c.MaxSteps, c.CacheKey = extra.MaxSteps, extra.CacheKey
	return nil
}
