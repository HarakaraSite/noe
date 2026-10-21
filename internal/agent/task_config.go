package agent

import "encoding/json"

// TaskConfig is frozen at registration and copied into each fresh task
// execution.
type TaskConfig struct {
	Config
	MaxSteps int     `json:"maxSteps"`
	CacheKey *string `json:"cacheKey,omitempty"`
}

// Keep the package-internal spelling used by core dispatch and tests.
type taskConfig = TaskConfig

// MarshalJSON preserves the complete registration input. Execution-specific
// projections are made by the executor's caller, never by this public type.
func (c TaskConfig) MarshalJSON() ([]byte, error) {
	data, err := json.Marshal(c.Config)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	fields["maxSteps"], err = json.Marshal(c.MaxSteps)
	if err != nil {
		return nil, err
	}
	if c.CacheKey != nil {
		fields["cacheKey"], err = json.Marshal(c.CacheKey)
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

func (c *TaskConfig) UnmarshalJSON(data []byte) error {
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
