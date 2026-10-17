package provider

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCatalogMergeFreezeAndTransportSeparation(t *testing.T) {
	r, path := registryFixture(t, `{"providers":{"openrouter":{"headers":{"X-Private":"not-catalog-data"},"models":{"deepseek/deepseek-v4.1-flash":{"catalog":{"description":"My preferred routine worker","tags":["routine"],"capabilities":{"tools":true,"vision":false},"contextWindow":100000,"efforts":["auto","high"]},"parameters":{"temperature":0.3}},"cloudflare/clef":null}},"opencode-go":{"models":{"gpt-6-luna":{"catalog":{"description":"My alternative worker"}}}}}}`)
	models, err := r.ListModels(ModelQuery{Provider: "openrouter", Kind: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Model != "deepseek/deepseek-v4.1-flash" || models[0].Description != "My preferred routine worker" || strings.Join(models[0].Tags, ",") != "routine" || !models[0].Capabilities["tools"] || models[0].Capabilities["vision"] || models[0].ContextWindow != 100000 {
		t.Fatalf("catalog overlay: %+v", models)
	}
	encoded, _ := json.Marshal(models)
	for _, private := range []string{"not-catalog-data", "keyFile", "headers", "parameters", "baseURL"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("transport data in catalog: %s", encoded)
		}
	}
	// Returned maps/slices are caller-owned, not mutable registry state.
	models[0].Tags[0] = "changed"
	models[0].Capabilities["tools"] = false
	if err := os.WriteFile(path, []byte(`invalid`), 0600); err != nil {
		t.Fatal(err)
	}
	again, err := r.ListModels(ModelQuery{Provider: "openrouter", Kind: "agent"})
	if err != nil || again[0].Tags[0] != "routine" || !again[0].Capabilities["tools"] {
		t.Fatalf("catalog snapshot changed: %+v %v", again, err)
	}
	p, err := r.resolve("openrouter", models[0].Model)
	if err != nil || string(p.(*OpenRouter).profile.Parameters["temperature"]) != "0.3" || p.(*OpenRouter).key != "" {
		t.Fatalf("catalog interfered with transport: %+v %v", p, err)
	}
	// A metadata-only override retains the model-specific Responses transport.
	p, err = r.resolve("opencode-go", "gpt-6-luna")
	if err != nil || p.(*OpenRouter).profile.API != "responses" {
		t.Fatalf("model transport lost: %v", err)
	}
	decisions, err := r.ListModels(ModelQuery{Kind: "decision"})
	if err != nil || len(decisions) != 2 {
		t.Fatalf("removed entry reappeared: %+v %v", decisions, err)
	}
}

func TestCatalogDefaultsCustomProviderAndDeletion(t *testing.T) {
	r, _ := registryFixture(t, `{"providers":{"opencode-go":{"models":{"deepseek-v4.1-flash":{"catalog":null}}},"custom":{"api":"chat-completions","baseURL":"http://localhost:2345/v1","path":"/chat/completions","models":{"custom-model":{"catalog":{"description":"My local worker"}},"route-only":{}}}}}`)
	models, err := r.ListModels(ModelQuery{})
	if err != nil || len(models) != 11 {
		t.Fatalf("merged catalog: %+v %v", models, err)
	}
	for i, model := range models {
		if model.Model == "route-only" || model.Model == "deepseek-v4.1-flash" {
			t.Fatalf("uncatalogued entry returned: %+v", model)
		}
		if i > 0 && models[i-1].Provider+"/"+models[i-1].Model > model.Provider+"/"+model.Model {
			t.Fatal("catalog order is unstable")
		}
	}
	custom, err := r.ListModels(ModelQuery{Provider: "custom", Kind: "agent"})
	if err != nil || len(custom) != 1 || custom[0].Model != "custom-model" || custom[0].Kind != "agent" {
		t.Fatalf("custom provider: %+v %v", custom, err)
	}
	none, err := r.ListModels(ModelQuery{Provider: "custom", Kind: "decision"})
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("empty catalog must encode as []: %+v %v", none, err)
	}
	if _, err := r.ListModels(ModelQuery{Provider: "missing"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	// Catalog membership does not restrict direct calls.
	if _, err := r.resolve("custom", "unlisted-model"); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRejectsInvalidMetadataAndModelTransport(t *testing.T) {
	for _, entry := range []string{
		`{"catalog":true}`,
		`{"catalog":{"unknown":true}}`,
		`{"catalog":{"kind":"messages"}}`,
		`{"catalog":{"contextWindow":-1}}`,
		`{"catalog":{"capabilities":{"vision":"yes"}}}`,
		`{"catalog":{"efforts":["bogus"]}}`,
		`{"catalog":{},"api":"messages"}`,
		`{"catalog":{},"typo":true}`,
	} {
		t.Run(entry, func(t *testing.T) {
			r, _ := registryFixture(t, `{"providers":{"openrouter":{"models":{"invalid-model":`+entry+`}}}}`)
			if _, err := r.ListModels(ModelQuery{}); err == nil {
				t.Fatal("invalid catalog/model transport accepted")
			}
		})
	}
}
