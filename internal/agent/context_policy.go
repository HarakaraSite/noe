package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

const (
	defaultContextRetainRatio = 0.1
	defaultContextInputTokens = int64(1_000_000)
)

// ContextPolicy controls automatic trimming of completed execution history.
// A nil policy is interpreted by the caller as automatic policy unless the
// corresponding config presence bit says that it was explicitly disabled.
type ContextPolicy struct {
	RetainRatio           *float64 `json:"retainRatio,omitempty"`
	ResponseReserveTokens *int64   `json:"responseReserveTokens,omitempty"`
}

func (p *ContextPolicy) UnmarshalJSON(data []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
		return errors.New("contextPolicy must be an object")
	}
	for key := range fields {
		if key != "retainRatio" && key != "responseReserveTokens" {
			return errors.New("unknown contextPolicy option: " + key)
		}
	}
	var policy ContextPolicy
	if raw, exists := fields["retainRatio"]; exists {
		value, err := decodePolicyFloat(raw)
		if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value >= 1 {
			return errors.New("contextPolicy.retainRatio must be finite and between 0 and 1")
		}
		policy.RetainRatio = &value
	}
	if raw, exists := fields["responseReserveTokens"]; exists {
		value, err := decodePolicyInteger(raw)
		if err != nil || value <= 0 || value > maxSafeContextBudget {
			return errors.New("contextPolicy.responseReserveTokens must be a positive safe integer")
		}
		policy.ResponseReserveTokens = &value
	}
	*p = policy
	return nil
}

func decodePolicyFloat(raw json.RawMessage) (float64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, err
	}
	value, ok := decoded.(json.Number)
	if !ok {
		return 0, errors.New("expected a JSON number")
	}
	return value.Float64()
}

func decodePolicyInteger(raw json.RawMessage) (int64, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return 0, err
	}
	value, ok := decoded.(json.Number)
	if !ok {
		return 0, errors.New("expected a JSON integer")
	}
	return value.Int64()
}

func parseContextPolicy(raw json.RawMessage) (*ContextPolicy, bool, error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, true, nil
	}
	var policy ContextPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return nil, true, err
	}
	return &policy, true, nil
}

func cloneContextPolicy(policy *ContextPolicy) *ContextPolicy {
	if policy == nil {
		return nil
	}
	copy := *policy
	if policy.RetainRatio != nil {
		value := *policy.RetainRatio
		copy.RetainRatio = &value
	}
	copy.ResponseReserveTokens = cloneInt64(policy.ResponseReserveTokens)
	return &copy
}

func effectiveContextRetainRatio(policy *ContextPolicy) float64 {
	if policy == nil || policy.RetainRatio == nil {
		return defaultContextRetainRatio
	}
	return *policy.RetainRatio
}

// executionBudgetSnapshot keeps the input-total budget distinct from the
// execution-history allowance used by trimming.
type executionBudgetSnapshot struct {
	BudgetSource           string  `json:"budgetSource"`
	TotalInputTokens       int64   `json:"totalInputTokens"`
	ExecutionTokens        int64   `json:"executionTokens"`
	FixedTokens            int64   `json:"fixedTokens"`
	TransientTokens        int64   `json:"transientTokens"`
	ResponseReserveTokens  *int64  `json:"responseReserveTokens"`
	ResponseReserveUnknown bool    `json:"responseReserveUnknown"`
	WindowTokens           *int64  `json:"windowTokens"`
	RetainRatio            float64 `json:"retainRatio"`
}

// resolveExecutionBudget combines model and user limits without changing the
// existing contextBudget input-total scale. All estimates are nonnegative and
// clamped to the largest exactly representable JSON integer.
func resolveExecutionBudget(budget *ContextBudget, window *int64, maxTokens int, catalogMaxOutput *int64, policy *ContextPolicy, fixedTokens, transientTokens int64) executionBudgetSnapshot {
	fixed := safeTokenCount(fixedTokens)
	transient := safeTokenCount(transientTokens)
	protected := safeTokenAdd(fixed, transient)
	reserve := resolveResponseReserve(policy, maxTokens, catalogMaxOutput)
	windowValue := cloneInt64(window)
	if windowValue != nil {
		if *windowValue <= 0 {
			windowValue = nil
		} else {
			*windowValue = safeTokenCount(*windowValue)
		}
	}
	result := executionBudgetSnapshot{
		FixedTokens: fixed, TransientTokens: transient,
		ResponseReserveTokens: reserve, ResponseReserveUnknown: reserve == nil,
		WindowTokens: windowValue, RetainRatio: effectiveContextRetainRatio(policy),
	}

	var explicit *int64
	if budget != nil && budget.InputTokens > 0 {
		value := safeTokenCount(budget.InputTokens)
		explicit = &value
	}
	var total int64
	switch {
	case windowValue != nil && reserve != nil:
		remaining := int64(0)
		if *windowValue > protected && *windowValue-protected > *reserve {
			remaining = *windowValue - protected - *reserve
		}
		windowTotal := safeTokenAdd(protected, floorNinetyPercent(remaining))
		if explicit != nil && *explicit < windowTotal {
			total = *explicit
			result.BudgetSource = "combined"
		} else {
			total = windowTotal
			if explicit != nil {
				result.BudgetSource = "combined"
			} else {
				result.BudgetSource = "model_window"
			}
		}
	case windowValue != nil:
		base := defaultContextInputTokens
		result.BudgetSource = "fallback_window_cap"
		if explicit != nil {
			base = *explicit
			result.BudgetSource = "combined"
		}
		windowCap := floorNinetyPercent(*windowValue)
		total = base
		if windowCap < total {
			total = windowCap
		}
	case explicit != nil:
		total = *explicit
		result.BudgetSource = "input_budget"
	default:
		total = defaultContextInputTokens
		result.BudgetSource = "fallback"
	}
	result.TotalInputTokens = safeTokenCount(total)
	if result.TotalInputTokens > protected {
		result.ExecutionTokens = result.TotalInputTokens - protected
	}
	return result
}

func resolveResponseReserve(policy *ContextPolicy, maxTokens int, catalogMaxOutput *int64) *int64 {
	var reserve int64
	if policy != nil && policy.ResponseReserveTokens != nil && *policy.ResponseReserveTokens > reserve {
		reserve = *policy.ResponseReserveTokens
	}
	if maxTokens > 0 {
		value := int64(maxTokens)
		if value > maxSafeContextBudget {
			value = maxSafeContextBudget
		}
		if value > reserve {
			reserve = value
		}
	}
	if reserve == 0 && catalogMaxOutput != nil && *catalogMaxOutput > 0 {
		reserve = safeTokenCount(*catalogMaxOutput)
	}
	if reserve == 0 {
		return nil
	}
	return &reserve
}

func safeTokenCount(tokens int64) int64 {
	if tokens <= 0 {
		return 0
	}
	if tokens > maxSafeContextBudget {
		return maxSafeContextBudget
	}
	return tokens
}

func safeTokenAdd(left, right int64) int64 {
	if right > maxSafeContextBudget-left {
		return maxSafeContextBudget
	}
	return left + right
}

func floorNinetyPercent(tokens int64) int64 {
	if tokens <= 0 {
		return 0
	}
	return (tokens/10)*9 + (tokens%10)*9/10
}

func contextMaxOutputTokensForModel(catalog interface {
	ListModels(provider.ModelQuery) ([]provider.Model, error)
}, providerName, modelName string) *int64 {
	if catalog == nil {
		return nil
	}
	models, err := catalog.ListModels(provider.ModelQuery{Provider: providerName, Kind: "agent"})
	if err != nil {
		return nil
	}
	for _, model := range models {
		if model.Model == modelName && model.MaxOutputTokens > 0 {
			value := model.MaxOutputTokens
			return &value
		}
	}
	return nil
}

func (h *Host) contextModelLimits(providerName, modelName string) (*int64, *int64) {
	if h.opts.Connection != nil {
		defaults := h.opts.Connection.Defaults
		if defaults.Provider == providerName && defaults.Model == modelName {
			return cloneInt64(h.opts.Connection.ModelWindow), cloneInt64(h.opts.Connection.ModelMaxOutputTokens)
		}
		return nil, nil
	}
	return contextWindowForModel(h.catalog, providerName, modelName), contextMaxOutputTokensForModel(h.catalog, providerName, modelName)
}
