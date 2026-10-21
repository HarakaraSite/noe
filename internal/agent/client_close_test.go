package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
)

func TestClientCloseJoinsHostShutdownWhenHostClosesFirst(t *testing.T) {
	providerEntered := make(chan struct{})
	providerCanceled := make(chan struct{})
	releaseProvider := make(chan struct{})
	var releaseOnce sync.Once
	models := reviewModels{
		list: func(context.Context, model.ModelQuery) ([]model.Model, error) {
			return []model.Model{{Provider: "fixture", Model: "fixture-model"}}, nil
		},
		generate: func(ctx context.Context, _ model.Request) (model.Response, error) {
			close(providerEntered)
			<-ctx.Done()
			close(providerCanceled)
			<-releaseProvider
			return model.Response{}, ctx.Err()
		},
	}
	h := NewHost(Options{Models: models})
	defer func() {
		releaseOnce.Do(func() { close(releaseProvider) })
		h.Close()
	}()
	op := h.NewOperation(context.Background())
	generateDone := make(chan struct{})
	go func() {
		defer close(generateDone)
		_, _ = op.Models().Generate(context.Background(), model.Request{Provider: "fixture", Model: "fixture-model"})
	}()
	select {
	case <-providerEntered:
	case <-time.After(time.Second):
		t.Fatal("provider Generate was not entered")
	}

	hostCloseDone := make(chan struct{})
	go func() {
		h.Close()
		close(hostCloseDone)
	}()
	select {
	case <-providerCanceled:
	case <-time.After(time.Second):
		t.Fatal("Host.Close did not cancel the provider request")
	}

	clientCloseDone := make(chan struct{})
	go func() {
		op.CloseClient()
		close(clientCloseDone)
	}()
	select {
	case <-clientCloseDone:
		t.Fatal("client close returned before Host.Close joined provider cleanup")
	case <-hostCloseDone:
		t.Fatal("Host.Close returned before provider cleanup was released")
	case <-generateDone:
		t.Fatal("provider request returned before cleanup was released")
	case <-time.After(40 * time.Millisecond):
	}

	releaseOnce.Do(func() { close(releaseProvider) })
	for name, done := range map[string]<-chan struct{}{
		"client close": clientCloseDone,
		"host close":   hostCloseDone,
		"provider":     generateDone,
	} {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s did not return after provider cleanup", name)
		}
	}
}
