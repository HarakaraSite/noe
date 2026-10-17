package provider

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ModelInfo is provider/SDK-independent selection data, never request parameters.
// Missing capabilities and limits mean unknown, rather than unsupported or zero.
type ModelInfo struct {
	Kind            string          `json:"kind"`
	Description     string          `json:"description,omitempty"`
	Tags            []string        `json:"tags,omitempty"`
	Capabilities    map[string]bool `json:"capabilities,omitempty"`
	ContextWindow   int64           `json:"contextWindow,omitempty"`
	MaxOutputTokens int64           `json:"maxOutputTokens,omitempty"`
	Efforts         []string        `json:"efforts,omitempty"`
}

// Model identifies a model within a provider. Only public catalog data is exposed.
type Model struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	ModelInfo
}

type ModelQuery struct {
	Provider string `json:"provider,omitempty"`
	Kind     string `json:"kind,omitempty"`
}

// splitModel separates catalog metadata from a partial transport definition.
// The transport overlay keeps its existing JSON format and validation path.
func splitModel(raw json.RawMessage) (*ModelInfo, map[string]any, error) {
	override, err := decodeObject(raw)
	if err != nil {
		return nil, nil, err
	}
	value, exists := override["catalog"]
	delete(override, "catalog")
	if !exists {
		return nil, override, nil
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, nil, errors.New("catalog must be an object")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, nil, err
	}
	var info ModelInfo
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&info); err != nil {
		return nil, nil, err
	}
	if info.Kind == "" {
		info.Kind = "agent"
	}
	if info.Kind != "agent" && info.Kind != "decision" {
		return nil, nil, errors.New("catalog kind must be agent or decision")
	}
	if info.ContextWindow < 0 || info.MaxOutputTokens < 0 {
		return nil, nil, errors.New("catalog token limits must be nonnegative")
	}
	for _, effort := range info.Efforts {
		if _, valid := NormalizeEffort(effort); !valid || effort == "" {
			return nil, nil, fmt.Errorf("invalid catalog effort %q", effort)
		}
	}
	return &info, override, nil
}

// ListModels reads the frozen merged configuration without constructing clients,
// reading credentials, or making provider requests. Transport-only entries are
// intentionally absent from this curated list; it is not an allowlist for runs.
func (r *Registry) ListModels(query ModelQuery) ([]Model, error) {
	r.once.Do(r.load)
	if r.err != nil {
		return nil, r.err
	}
	if query.Kind != "" && query.Kind != "agent" && query.Kind != "decision" {
		return nil, errors.New("model kind must be agent or decision")
	}
	if query.Provider != "" {
		if _, exists := r.definitions.Providers[query.Provider]; !exists {
			return nil, fmt.Errorf("unknown provider %q", query.Provider)
		}
	}
	models := make([]Model, 0)
	for name, raw := range r.definitions.Providers {
		if query.Provider != "" && name != query.Provider {
			continue
		}
		d, err := decodeDefinition(raw)
		if err != nil {
			return nil, err
		}
		for id, raw := range d.Models {
			info, _, err := splitModel(raw)
			if err != nil {
				return nil, fmt.Errorf("provider %s model %s: %w", name, id, err)
			}
			if info == nil || (query.Kind != "" && info.Kind != query.Kind) {
				continue
			}
			models = append(models, Model{Provider: name, Model: id, ModelInfo: *info})
		}
	}
	sort.Slice(models, func(i, j int) bool {
		if models[i].Provider != models[j].Provider {
			return models[i].Provider < models[j].Provider
		}
		return models[i].Model < models[j].Model
	})
	return models, nil
}

func validateModels(d Definition) error {
	for id, raw := range d.Models {
		if strings.TrimSpace(id) == "" {
			return errors.New("model ID must not be empty")
		}
		_, override, err := splitModel(raw)
		if err == nil {
			// Validate each effective transport, not just the provider default.
			data, _ := json.Marshal(d)
			base, decodeErr := decodeObject(data)
			if decodeErr != nil {
				return decodeErr
			}
			delete(base, "models")
			mergeObject(base, override)
			data, err = json.Marshal(base)
			if err == nil {
				_, err = decodeDefinition(data)
			}
		}
		if err != nil {
			return fmt.Errorf("model %s: %w", id, err)
		}
	}
	return nil
}
