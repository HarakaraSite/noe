// Package agentbridge connects the managed-agent host to a QuickJS runtime.
// It is private so public consumers can depend on the stable agent facade
// without inheriting NOE's runtime implementation.
package agentbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"forge.harakara.site/littleisland/noe/v2/internal/observation"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Config is the private adapter between the public agent facade and the
// existing Go agent host. Resource and callback fields are borrowed.
type Config struct {
	Loop  *stdlib.Loop
	Agent core.Options
	Auth  *credentials.Options
	Load  quickjs.ModuleLoader
}

// Binding owns the Go-side lifetime for one runtime agent installation. It
// never owns the QuickJS runtime or its stdlib loop.
type Binding struct {
	rt   *quickjs.Runtime
	loop *stdlib.Loop
	cwd  string
	host *core.Host
	op   *core.Operation

	mu        sync.Mutex
	closed    bool
	closeDone chan struct{}
	workers   sync.WaitGroup
	stopCtx   func() bool

	hookMu     sync.Mutex
	hookScopes map[string]hookScope
}

type hookScope struct {
	ctx    context.Context
	cancel context.CancelFunc
}

var installations = struct {
	sync.Mutex
	runtimes map[*quickjs.Runtime]struct{}
}{runtimes: make(map[*quickjs.Runtime]struct{})}

// Install connects the agent modules to rt and starts one operation under
// ctx. A runtime can receive only one bridge installation. The caller remains
// responsible for closing rt and cfg.Loop.
func Install(ctx context.Context, rt *quickjs.Runtime, cfg Config) (*Binding, error) {
	if ctx == nil {
		return nil, errors.New("agent: context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if rt == nil {
		return nil, errors.New("agent: runtime is required")
	}
	runtimeCtx := rt.Context()
	if runtimeCtx == nil {
		return nil, errors.New("agent: live runtime is required")
	}
	if err := runtimeCtx.Err(); err != nil {
		return nil, err
	}
	if cfg.Loop == nil {
		return nil, errors.New("agent: Loop is required")
	}
	loopCtx := cfg.Loop.Context()
	if loopCtx == nil {
		return nil, errors.New("agent: live Loop is required")
	}
	if err := loopCtx.Err(); err != nil {
		return nil, err
	}

	installations.Lock()
	if _, exists := installations.runtimes[rt]; exists {
		installations.Unlock()
		return nil, errors.New("agent: runtime already has an installation")
	}
	installations.runtimes[rt] = struct{}{}
	installations.Unlock()
	// Register reservation release before the binding close callback. QuickJS
	// invokes OnClose callbacks in LIFO order, so the Go lifetime is joined
	// before this runtime can be considered available for pointer reuse.
	rt.OnClose(func() {
		installations.Lock()
		delete(installations.runtimes, rt)
		installations.Unlock()
	})

	settings, err := cloneAgentOptions(cfg.Agent)
	if err != nil {
		return nil, err
	}
	if settings.Cwd == "" {
		settings.Cwd, err = os.Getwd()
		if err != nil {
			return nil, err
		}
	}
	if cfg.Auth != nil {
		auth := *cfg.Auth
		cfg.Auth = &auth
	}
	settings.Cwd, err = filepath.Abs(settings.Cwd)
	if err != nil {
		return nil, err
	}

	host := core.NewHost(settings)
	op := host.NewOperation(ctx)
	b := &Binding{
		rt: rt, loop: cfg.Loop, cwd: settings.Cwd, host: host, op: op,
		closeDone: make(chan struct{}), hookScopes: make(map[string]hookScope),
	}
	stopCtx := context.AfterFunc(ctx, b.Close)
	b.mu.Lock()
	b.stopCtx = stopCtx
	closedDuringInstall := b.closed
	b.mu.Unlock()
	if closedDuringInstall {
		stopCtx()
	}
	rt.OnClose(b.Close)

	local := cfg.Load
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		if source, ok := moduleSource(specifier, cfg.Auth != nil); ok {
			return source, specifier, nil
		}
		if local == nil {
			return "", "", fmt.Errorf("module %q has no configured loader", specifier)
		}
		return local(specifier, referrer)
	})
	if err := b.installModules(cfg.Auth); err != nil {
		b.Close()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		b.Close()
		return nil, err
	}
	if b.isClosed() {
		b.Close()
		return nil, context.Canceled
	}
	return b, nil
}

func cloneAgentOptions(options core.Options) (core.Options, error) {
	if options.Connection != nil {
		data, err := json.Marshal(options.Connection)
		if err != nil {
			return core.Options{}, fmt.Errorf("copy agent connection: %w", err)
		}
		var connection core.Connection
		if err := json.Unmarshal(data, &connection); err != nil {
			return core.Options{}, fmt.Errorf("copy agent connection: %w", err)
		}
		options.Connection = &connection
	}
	if profile := options.ProviderOptions.Profile; profile != nil {
		data, err := json.Marshal(profile)
		if err != nil {
			return core.Options{}, fmt.Errorf("copy provider profile: %w", err)
		}
		var copied provider.Definition
		if err := json.Unmarshal(data, &copied); err != nil {
			return core.Options{}, fmt.Errorf("copy provider profile: %w", err)
		}
		options.ProviderOptions.Profile = &copied
	}
	return options, nil
}

func moduleSource(specifier string, authEnabled bool) (string, bool) {
	switch specifier {
	case "noe:agent":
		return core.ModuleSource, true
	case "noe:agent/api":
		return core.APIModuleSource, true
	case "noe:agent/hooks":
		return core.HooksModuleSource, true
	case "noe:image":
		return imageapi.ModuleSource, true
	case "noe:decision":
		return decision.ModuleSource, true
	case "noe:auth":
		if authEnabled {
			return credentials.ModuleSource, true
		}
	case "noe:agent/task":
		return core.TaskModuleSource, true
	case "noe:agent/history":
		return core.HistoryModuleSource, true
	}
	return "", false
}

func (b *Binding) installModules(auth *credentials.Options) error {
	if err := b.rt.SetModule("noe:image/internal", map[string]any{"request": b.agentRequest}); err != nil {
		return err
	}
	if err := b.rt.SetModule("noe:decision/internal", map[string]any{"request": b.agentRequest}); err != nil {
		return err
	}
	if err := b.rt.SetModule("noe:agent/internal", map[string]any{
		"request":        b.agentRequest,
		"defaults":       b.host.Defaults,
		"instructions":   core.Instructions,
		"observe":        b.agentObserve,
		"beginHookScope": b.agentBeginHookScope,
		"requestHook":    b.agentRequestHook,
		"endHookScope":   b.agentEndHookScope,
		"observeHook":    b.agentObserveHook,
	}); err != nil {
		return err
	}
	if auth == nil {
		return nil
	}
	settings := *auth
	return b.rt.SetModule("noe:auth/internal", map[string]any{
		"save": func(payload string) *quickjs.Promise {
			promise := b.rt.NewPromise()
			var request credentials.SaveRequest
			if err := json.Unmarshal([]byte(payload), &request); err != nil || request.File == "" {
				promise.RejectError(errors.New("invalid ChatGPT sign-in result"))
				return promise
			}
			if !filepath.IsAbs(request.File) {
				request.File = filepath.Join(b.cwd, request.File)
			}
			b.start(promise, func() (string, error) {
				account, err := credentials.SaveChatGPT(b.Context(), request, settings)
				data, _ := json.Marshal(account)
				return string(data), err
			})
			return promise
		},
	})
}

// Context returns this binding's operation context.
func (b *Binding) Context() context.Context {
	if b == nil || b.op == nil {
		return context.Background()
	}
	return b.op.Context()
}

// Close cancels and joins Go work without closing the runtime, loop, or
// borrowed resources. It is safe to call concurrently and more than once.
func (b *Binding) Close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if b.closed {
		done := b.closeDone
		b.mu.Unlock()
		<-done
		return
	}
	b.closed = true
	stopCtx := b.stopCtx
	b.mu.Unlock()
	if stopCtx != nil {
		stopCtx()
	}

	// Operation.Close cancels provider requests and owned runs before joining
	// their internal request gates. The bridge's own producers are joined after
	// it; they only post owner work and never wait for that work to run.
	b.op.Close()
	b.host.Close()
	b.workers.Wait()
	b.hookMu.Lock()
	for id, scope := range b.hookScopes {
		delete(b.hookScopes, id)
		scope.cancel()
	}
	b.hookMu.Unlock()
	close(b.closeDone)
}

func (b *Binding) isClosed() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closed
}

func (b *Binding) beginWork() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return false
	}
	b.workers.Add(1)
	b.loop.Begin()
	return true
}

func (b *Binding) start(promise *quickjs.Promise, work func() (string, error)) {
	if !b.beginWork() {
		promise.RejectError(errors.New("agent binding is closed"))
		return
	}
	go func() {
		defer b.workers.Done()
		defer b.loop.Done()
		response, err := work()
		b.loop.Post(func() { b.settle(promise, response, err) })
	}()
}

func (b *Binding) settle(promise *quickjs.Promise, response string, err error) {
	// Hold the same gate Close uses until JS settlement finishes. This prevents
	// a posted callback that passed its closed check from touching QuickJS after
	// a concurrent Go Close has begun.
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	if err != nil {
		promise.RejectError(err)
	} else {
		promise.Resolve(response)
	}
}

func (b *Binding) watchOwned(done <-chan struct{}) {
	if done == nil || !b.beginWork() {
		return
	}
	go func() {
		defer b.workers.Done()
		defer b.loop.Done()
		select {
		case <-done:
		case <-b.Context().Done():
		}
	}()
}

func (b *Binding) agentRequest(method, payload string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	if b.op == nil {
		promise.RejectError(errors.New("agent request outside an evaluation"))
		return promise
	}
	b.start(promise, func() (string, error) {
		response, err := b.op.Call(method, payload)
		if err == nil && method == "spawn_agent" {
			var spawned struct {
				AgentID string `json:"agentId"`
			}
			if json.Unmarshal([]byte(response), &spawned) == nil {
				b.watchOwned(b.op.OwnedLifetime(spawned.AgentID))
			}
		}
		return response, err
	})
	return promise
}

func (b *Binding) agentObserve(payload string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	if b.op == nil {
		promise.RejectError(errors.New("agent observation outside an evaluation"))
		return promise
	}
	var query observation.Query
	if err := json.Unmarshal([]byte(payload), &query); err != nil {
		promise.RejectError(err)
		return promise
	}
	if query.LogPath != "" && !filepath.IsAbs(query.LogPath) {
		query.LogPath = filepath.Join(b.cwd, query.LogPath)
	}
	b.start(promise, func() (string, error) {
		snapshot, err := b.op.ObserveLog(query)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(snapshot)
		return string(encoded), err
	})
	return promise
}

func (b *Binding) agentBeginHookScope(payload string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	if !json.Valid([]byte(payload)) {
		promise.RejectError(errors.New("invalid hook scope request"))
		return promise
	}
	b.start(promise, func() (string, error) {
		response, err := b.op.Call("begin_hook_scope", payload)
		if err != nil {
			return "", err
		}
		var scope struct {
			ScopeID string `json:"scopeId"`
		}
		if json.Unmarshal([]byte(response), &scope) != nil || scope.ScopeID == "" {
			return "", errors.New("agent broker returned an invalid hook scope")
		}
		if err := b.Context().Err(); err != nil {
			cleanup, _ := json.Marshal(map[string]string{"scopeId": scope.ScopeID, "status": "cancelled"})
			_, _ = b.op.Call("end_hook_scope", string(cleanup))
			return "", err
		}
		hookCtx, cancel := context.WithCancel(b.Context())
		b.hookMu.Lock()
		if _, exists := b.hookScopes[scope.ScopeID]; exists {
			b.hookMu.Unlock()
			cancel()
			err = errors.New("agent broker reused a hook scope")
		} else {
			b.hookScopes[scope.ScopeID] = hookScope{ctx: hookCtx, cancel: cancel}
			b.hookMu.Unlock()
		}
		if err != nil {
			cleanup, _ := json.Marshal(map[string]string{"scopeId": scope.ScopeID, "status": "cancelled"})
			_, _ = b.op.Call("end_hook_scope", string(cleanup))
			return "", err
		}
		return response, nil
	})
	return promise
}

func (b *Binding) agentRequestHook(scopeID, method, payload string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	b.hookMu.Lock()
	scope, ok := b.hookScopes[scopeID]
	b.hookMu.Unlock()
	if !ok || scope.ctx.Err() != nil {
		promise.RejectError(errors.New("hook API scope has ended"))
		return promise
	}
	if !hookAPIMethod(method) {
		promise.RejectError(errors.New("unsupported hook agent request"))
		return promise
	}
	if !json.Valid([]byte(payload)) {
		promise.RejectError(errors.New("invalid agent request JSON"))
		return promise
	}
	b.start(promise, func() (string, error) {
		return b.op.CallWithScope(scope.ctx, scopeID, method, payload)
	})
	return promise
}

func hookAPIMethod(method string) bool {
	switch method {
	case "list_models", "spawn_agent", "observe_agent", "wait_agent", "queue_agent", "wait_task", "steer_agent", "interrupt_agent", "execution_get", "context_get", "context_set_runtime_info":
		return true
	}
	return false
}

func (b *Binding) agentEndHookScope(scopeID, status, errorText string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	b.hookMu.Lock()
	scope, ok := b.hookScopes[scopeID]
	if ok {
		delete(b.hookScopes, scopeID)
		scope.cancel()
	}
	b.hookMu.Unlock()
	if !ok {
		promise.Resolve(`{"closed":false}`)
		return promise
	}
	encoded, _ := json.Marshal(map[string]string{"scopeId": scopeID, "status": status, "error": errorText})
	b.start(promise, func() (string, error) {
		return b.op.Call("end_hook_scope", string(encoded))
	})
	return promise
}

func (b *Binding) agentObserveHook(scopeID, payload string) *quickjs.Promise {
	promise := b.rt.NewPromise()
	b.hookMu.Lock()
	scope, ok := b.hookScopes[scopeID]
	b.hookMu.Unlock()
	if !ok || scope.ctx.Err() != nil {
		promise.RejectError(errors.New("hook API scope has ended"))
		return promise
	}
	var query observation.Query
	if err := json.Unmarshal([]byte(payload), &query); err != nil {
		promise.RejectError(err)
		return promise
	}
	if query.LogPath != "" && !filepath.IsAbs(query.LogPath) {
		query.LogPath = filepath.Join(b.cwd, query.LogPath)
	}
	b.start(promise, func() (string, error) {
		snapshot, err := observation.Observe(scope.ctx, query)
		if err != nil {
			return "", err
		}
		encoded, err := json.Marshal(snapshot)
		return string(encoded), err
	})
	return promise
}
