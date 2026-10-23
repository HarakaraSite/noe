// Package authbridge installs the independent authentication helper. It has
// no agent Host, operation, provider registry, or NOE path dependencies.
package authbridge

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type Binding struct {
	rt      *quickjs.Runtime
	loop    *stdlib.Loop
	ctx     context.Context
	cancel  context.CancelFunc
	cwd     string
	options credentials.Options
	mu      sync.Mutex
	closed  bool
	done    chan struct{}
	workers sync.WaitGroup
}

// Install registers the native endpoint used by credentials.ModuleSource.
// The caller composes that source into its loader and owns rt and loop.
func Install(ctx context.Context, rt *quickjs.Runtime, loop *stdlib.Loop, cwd string, options credentials.Options) (*Binding, error) {
	if ctx == nil || rt == nil || loop == nil {
		return nil, errors.New("auth: context, runtime and loop are required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := rt.Context().Err(); err != nil {
		return nil, err
	}
	if err := loop.Context().Err(); err != nil {
		return nil, err
	}
	if !filepath.IsAbs(cwd) {
		return nil, errors.New("auth: absolute execution cwd is required")
	}
	ctx, cancel := context.WithCancel(ctx)
	b := &Binding{rt: rt, loop: loop, ctx: ctx, cancel: cancel, cwd: cwd, options: options, done: make(chan struct{})}
	if err := rt.SetModule("noe:auth/internal", map[string]any{"save": b.save}); err != nil {
		cancel()
		return nil, err
	}
	rt.OnClose(b.Close)
	return b, nil
}

func (b *Binding) save(payload string) *quickjs.Promise {
	p := b.rt.NewPromise()
	var request credentials.SaveRequest
	if err := json.Unmarshal([]byte(payload), &request); err != nil || request.File == "" {
		p.RejectError(errors.New("invalid ChatGPT sign-in result"))
		return p
	}
	if !filepath.IsAbs(request.File) {
		request.File = filepath.Join(b.cwd, request.File)
	}
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil {
		b.mu.Unlock()
		p.RejectError(context.Canceled)
		return p
	}
	b.workers.Add(1)
	b.loop.Begin()
	b.mu.Unlock()
	go func() {
		defer b.workers.Done()
		defer b.loop.Done()
		account, err := credentials.SaveChatGPT(b.ctx, request, b.options)
		data, _ := json.Marshal(account)
		b.loop.Post(func() {
			b.mu.Lock()
			defer b.mu.Unlock()
			if b.closed {
				return
			}
			if err != nil {
				p.RejectError(err)
			} else {
				_ = p.Resolve(string(data))
			}
		})
	}()
	return p
}

// Close cancels and joins producers without awaiting JS delivery or closing
// the borrowed runtime, loop, or HTTP client. It may be called concurrently.
func (b *Binding) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		<-b.done
		return
	}
	b.closed = true
	b.cancel()
	b.mu.Unlock()
	b.workers.Wait()
	close(b.done)
}
