package provider

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

type scopeResource struct{ close func() }

func (r scopeResource) Close() { r.close() }

func TestRunScopeOwnsResourcesAndJoinsConcurrentClose(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	scope := NewRunScope(ctx)
	var count atomic.Int32
	key := &OpenAI{}
	first, err := scope.resource(key, func() managedResource {
		return scopeResource{close: func() {
			count.Add(1)
			// Close callbacks can inspect the manager: it must release its mutex
			// before invoking resource code, including while the owner is ending.
			if _, err := scope.resource(key, func() managedResource { t.Error("resource recreated during close"); return nil }); err == nil {
				t.Error("closing scope admitted a resource")
			}
		}}
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := scope.resource(key, func() managedResource { t.Error("duplicate resource"); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if first.(scopeResource).close == nil || second.(scopeResource).close == nil {
		t.Fatal("resource missing")
	}
	if managedScope(WithRunScope(ctx, scope)) != scope || managedScope(ctx) != nil {
		t.Fatal("managed scope context identity")
	}
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() { defer group.Done(); scope.Close() }()
	}
	cancel()
	group.Wait()
	scope.Close()
	if count.Load() != 1 {
		t.Fatalf("resource closed %d times", count.Load())
	}
	if _, err := scope.resource(key, func() managedResource { return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("closed resource admission: %v", err)
	}
}

func TestRunScopeOwnerCancellationAndIsolation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first, second := NewRunScope(ctx), NewRunScope(t.Context())
	defer second.Close()
	closed := make(chan struct{})
	otherClosed := false
	key := &OpenAI{}
	if _, err := first.resource(key, func() managedResource { return scopeResource{close: func() { close(closed) }} }); err != nil {
		t.Fatal(err)
	}
	if _, err := second.resource(key, func() managedResource { return scopeResource{close: func() { otherClosed = true }} }); err != nil {
		t.Fatal(err)
	}
	cancel()
	first.Close()
	<-closed
	if otherClosed {
		t.Fatal("ending one run closed another run's resource")
	}
	if _, err := first.resource(&OpenAI{}, func() managedResource { t.Error("cancelled owner created a resource"); return nil }); err == nil {
		t.Fatal("cancelled run admitted a resource")
	}
	second.Close()
	if !otherClosed {
		t.Fatal("resource not released")
	}
}

func TestProviderTransportSelection(t *testing.T) {
	for _, test := range []struct {
		sdk, api, transport string
		valid               bool
	}{
		{"openai", "responses", "", true}, {"openai", "responses", "sse", true}, {"openai", "responses", "websocket", true},
		{"openai", "responses", "auto", true}, {"openai", "chat-completions", "auto", true},
		{"openai", "responses", "tcp", false}, {"openai", "chat-completions", "websocket", false}, {"openrouter", "responses", "websocket", false},
	} {
		definition := `{"sdk":"` + test.sdk + `","auth":"api-key","api":"` + test.api + `","baseURL":"https://fixture.invalid/v1","path":"/responses","transport":"` + test.transport + `"}`
		value, err := decodeDefinition([]byte(definition))
		if (err == nil) != test.valid {
			t.Fatalf("%s: %v", definition, err)
		}
		if err == nil && test.transport == "" && value.Transport != "sse" {
			t.Fatal("custom providers must keep SSE by default")
		}
		if err == nil && test.transport == "auto" && ((test.api == "responses" && value.Transport != "websocket") || (test.api != "responses" && value.Transport != "sse")) {
			t.Fatal("automatic transport did not follow selected API")
		}
	}
	base := t.TempDir()
	registry := NewExplicitRegistry(Options{BaseDir: base, ConfigDir: base})
	for _, name := range []string{"openai-api", "openai-chatgpt"} {
		value, err := registry.resolveDefinition(name, "gpt-6.1-sol")
		if err != nil {
			t.Fatal(err)
		}
		if value.Transport != "websocket" {
			t.Fatalf("%s default transport: %s", name, value.Transport)
		}
	}
}
