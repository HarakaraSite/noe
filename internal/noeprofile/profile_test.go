package noeprofile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func TestProfileSharesFilteredEnvironmentAndExecutionFilesystem(t *testing.T) {
	for _, key := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "NOE_OPENROUTER_API_KEY", "NOE_AGENT_CONNECTION"} {
		t.Setenv(key, "hidden")
	}
	t.Setenv("NOE_PROFILE_VISIBLE", "visible")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "value.mjs"), []byte(`export const value=42;`), 0600); err != nil {
		t.Fatal(err)
	}
	profile, err := Profile(ProfileOptions{Cwd: dir, Source: "[root]"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = stdlib.Execute(context.Background(), stdlib.Source{Name: "[profile fixture]", Virtual: true, Module: true, Code: `
import {value} from "./value.mjs";
import {analyze_log} from "noe:agent-log";
import {save_chatgpt_credentials} from "noe:auth";
import {docs} from "noe:docs";
if(value!==42||typeof analyze_log!=="function"||typeof save_chatgpt_credentials!=="function"||typeof docs!=="function")throw Error("initializer or loader");
for(const key of ["OPENROUTER_API_KEY","OPENAI_API_KEY","ANTHROPIC_API_KEY","NOE_OPENROUTER_API_KEY","NOE_AGENT_CONNECTION"]){if(key in process.env)throw Error("secret environment exposed");}
if(process.env.NOE_PROFILE_VISIBLE!=="visible")throw Error("environment missing");
`}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}

func TestModelsDiscoverDefaultConfigLazilyWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("NOE_CONFIG_FILE", "")
	service := Models(model.Options{BaseDir: dir})
	configDir := filepath.Join(dir, "noe")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	config := `{"providers":{"openrouter":{"models":{"fixture/model":{"catalog":{"kind":"agent","description":"workspace catalog"}}}}}}`
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	entries, err := service.ListModels(context.Background(), model.ModelQuery{Provider: "openrouter"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range entries {
		if entry.Model == "fixture/model" {
			found = true
		}
	}
	if !found {
		t.Fatalf("default config not read: %+v", entries)
	}
	// A second operation uses the selected service and its immutable registry.
	if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ListModels(context.Background(), model.ModelQuery{}); err != nil {
		t.Fatal(err)
	}
}
func TestModelsExplicitMissingConfigFailsButMissingDefaultUsesCatalog(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("NOE_CONFIG_FILE", "")
	entries, err := Models(model.Options{BaseDir: dir}).ListModels(context.Background(), model.ModelQuery{})
	if err != nil || len(entries) == 0 {
		t.Fatalf("embedded catalog=%v err=%v", entries, err)
	}
	t.Setenv("NOE_CONFIG_FILE", filepath.Join(dir, "missing.json"))
	_, err = Models(model.Options{BaseDir: dir}).ListModels(context.Background(), model.ModelQuery{})
	if err == nil || !strings.Contains(err.Error(), "configuration") {
		t.Fatalf("explicit config=%v", err)
	}
}
