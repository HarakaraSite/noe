package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func runPortable() error {
	if err := constructPublicTypes(); err != nil {
		return err
	}
	return runPortableBinding()
}

func constructPublicTypes() error {
	var provider publicagent.Provider = portableProvider{}
	var decision publicagent.DecisionProvider = portableDecisionProvider{}
	var image publicagent.ImageProvider = portableImageProvider{}
	var client publicagent.HTTPClient = http.DefaultClient
	var _ io.Writer = (*borrowedSink)(nil)
	var _ interface {
		Do(*http.Request) (*http.Response, error)
	} = client
	var _ publicagent.Event = publicagent.Event{"type": "fixture"}
	var _ publicagent.Request = publicagent.Request{Provider: "openrouter", Model: "model", Messages: []json.RawMessage{json.RawMessage(`{"role":"user"}`)}}
	var _ publicagent.Response = publicagent.Response{Message: json.RawMessage(`{"role":"assistant"}`), FinishReason: "stop"}
	var _ publicagent.StreamEvent = publicagent.StreamEvent{Type: "message_delta", Text: "x"}
	var _ publicagent.DecisionRequest = publicagent.DecisionRequest{Provider: "openrouter", Model: "model", State: json.RawMessage(`"state"`), Questions: map[string]json.RawMessage{"q": json.RawMessage(`{"type":"choice"}`)}}
	var _ publicagent.DecisionResponse = publicagent.DecisionResponse{JSON: json.RawMessage(`{"answers":{}}`), StatusCode: http.StatusOK}
	var _ publicagent.ImageRequest = publicagent.ImageRequest{Provider: "fixture", Model: "image", Prompt: "p", Output: "out.png"}
	var _ publicagent.GeneratedImage = publicagent.GeneratedImage{Content: []byte{1}, ResponseModel: "image"}
	var _ publicagent.ImageError = publicagent.ImageError{Code: "response", Message: "fixture"}
	var _ publicagent.ProviderOptions = publicagent.ProviderOptions{
		ConfigFile: "config.json", UserAgent: "consumer-fixture", KeyFile: "key",
		BaseURL: "http://127.0.0.1", HTTPClient: client, DecisionServerURL: "http://127.0.0.1/decision",
	}
	var _ publicagent.AuthOptions = publicagent.AuthOptions{HTTPClient: client, DiscoveryURL: "http://127.0.0.1/discovery"}
	_ = provider
	_ = decision
	_ = image
	return nil
}

func runPortableBinding() error {
	workDir, err := os.MkdirTemp("", "noe-agent-portable-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	configPath := filepath.Join(workDir, "deliberately-invalid-config.json")
	if err := os.WriteFile(configPath, []byte("not parsed during import"), 0600); err != nil {
		return err
	}
	sink := &borrowedSink{}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := stdlib.Install(rt, stdlib.Config{Core: stdlib.CoreConfig{NoWebAPIs: true, Loop: loop, Stdout: sink, Stderr: sink}, Docs: true}); err != nil {
		_ = rt.Close()
		return fmt.Errorf("install partial stdlib: %w", err)
	}
	var loads int
	binding, err := publicagent.Install(ctx, rt, publicagent.Config{
		Loop: loop, Cwd: workDir, Executable: filepath.Join(os.TempDir(), "noe-consumer-executable-must-never-run"),
		Providers: publicagent.ProviderOptions{ConfigFile: configPath, KeyFile: filepath.Join(workDir, "missing-key")},
		Load: func(specifier, referrer string) (string, string, error) {
			if specifier != "fixture:portable" {
				return "", "", fmt.Errorf("unexpected portable module %q from %q", specifier, referrer)
			}
			loads++
			return `export const loaded = "external-loader";`, "fixture:portable", nil
		},
	})
	if err != nil {
		_ = rt.Close()
		return fmt.Errorf("install public agent: %w", err)
	}
	if binding.Context() == nil || binding.Context().Err() != nil {
		binding.Close()
		_ = rt.Close()
		return errors.New("binding did not expose a live operation context")
	}
	_, err = rt.EvalModuleContext(ctx, "portable-check.mjs", `
		import * as api from "noe:agent";
		import {loaded} from "fixture:portable";
		import {docs} from "noe:docs";
		globalThis.apiOK = typeof api.run_agent === "function" && typeof api.list_models === "function";
		globalThis.loaderOK = loaded === "external-loader";
		globalThis.docsOK = docs().includes("noe:agent");
	`)
	if err != nil {
		binding.Close()
		_ = rt.Close()
		return fmt.Errorf("import public agent and external module: %w", err)
	}
	for _, expression := range []string{"apiOK", "loaderOK", "docsOK"} {
		value, err := rt.EvalContext(ctx, expression)
		if err != nil || !value.Bool() {
			binding.Close()
			_ = rt.Close()
			return fmt.Errorf("portable assertion %s failed: %v", expression, err)
		}
	}
	if loads == 0 {
		binding.Close()
		_ = rt.Close()
		return errors.New("agent install did not preserve the caller's module loader")
	}
	if _, err := rt.EvalModuleContext(ctx, "auth-disabled.mjs", `import "noe:auth"`); err == nil {
		binding.Close()
		_ = rt.Close()
		return errors.New("noe:auth was installed without Auth options")
	}
	if loop.Pending() {
		binding.Close()
		_ = rt.Close()
		return errors.New("portable imports unexpectedly left loop work pending")
	}
	if _, err := rt.EvalContext(ctx, `console.log("portable-consumer-ready")`); err != nil {
		binding.Close()
		_ = rt.Close()
		return fmt.Errorf("write to borrowed stdlib sink: %w", err)
	}
	binding.Close()
	binding.Close()
	if binding.Context().Err() == nil {
		_ = rt.Close()
		return errors.New("Binding.Close did not cancel its operation context")
	}
	if err := rt.Close(); err != nil {
		return fmt.Errorf("close portable runtime: %w", err)
	}
	if sink.wasClosed() {
		return errors.New("runtime closed the caller-owned output sink")
	}
	if _, err := sink.Write([]byte("still-open")); err != nil {
		return fmt.Errorf("caller sink is unusable after runtime close: %w", err)
	}
	if !strings.Contains(sink.String(), "portable-consumer-ready") {
		return fmt.Errorf("borrowed sink did not receive console output: %q", sink.String())
	}
	return nil
}

type portableProvider struct{}

func (portableProvider) Generate(context.Context, publicagent.Request) (publicagent.Response, error) {
	return publicagent.Response{Message: json.RawMessage(`{"role":"assistant","content":"unused"}`), FinishReason: "stop"}, nil
}

type portableDecisionProvider struct{}

func (portableDecisionProvider) Decide(context.Context, publicagent.DecisionRequest) (publicagent.DecisionResponse, error) {
	return publicagent.DecisionResponse{JSON: json.RawMessage(`{"answers":{"q":{"type":"choice","choice":"yes"}}}`), StatusCode: http.StatusOK}, nil
}

type portableImageProvider struct{}

func (portableImageProvider) GenerateImage(context.Context, publicagent.ImageRequest) (publicagent.GeneratedImage, error) {
	return publicagent.GeneratedImage{Content: []byte("fixture")}, nil
}

type borrowedSink struct {
	mu     sync.Mutex
	data   strings.Builder
	closed bool
}

func (s *borrowedSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, errors.New("caller sink is closed")
	}
	return s.data.Write(p)
}

func (s *borrowedSink) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	return nil
}

func (s *borrowedSink) wasClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

func (s *borrowedSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data.String()
}
