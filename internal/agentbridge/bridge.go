// Package agentbridge connects the managed-agent host to a QuickJS runtime.
// It is private so public consumers can depend on the stable agent facade
// without inheriting NOE's runtime implementation.
package agentbridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
	"forge.harakara.site/littleisland/noe/v2/internal/modelbinding"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Binding owns the Go-side lifetime for one runtime agent installation. It
// never owns the QuickJS runtime or its stdlib loop.
type Binding struct {
	rt            *quickjs.Runtime
	loop          *stdlib.Loop
	joinHostClose bool
	view          *stdlib.Bindings
	op            *core.Operation
	models        *modelbinding.Binding

	mu        sync.Mutex
	closed    bool
	closeDone chan struct{}
	workers   sync.WaitGroup
	stopCtx   func() bool
	stopView  func() bool

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

func moduleSource(specifier string) (string, bool) {
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
	case "noe:agent/task":
		return core.TaskModuleSource, true
	case "noe:agent/task-protocol":
		return core.TaskProtocolModuleSource, true
	case "noe:agent/run-js":
		return core.RunJSModuleSource, true
	case "noe:agent/tool-pipeline":
		return core.ToolPipelineModuleSource, true
	case "noe:agent/history":
		return core.HistoryModuleSource, true
	}
	return "", false
}

func (b *Binding) installModules() error {
	if err := b.rt.SetModule("noe:agent/internal", map[string]any{
		"request":           b.agentRequest,
		"defaults":          b.defaults,
		"instructions":      b.instructions,
		"resolveHookModule": b.resolveHookModule,
		"beginHookScope":    b.agentBeginHookScope,
		"requestHook":       b.agentRequestHook,
		"endHookScope":      b.agentEndHookScope,
	}); err != nil {
		return err
	}
	return nil
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
	stopView := b.stopView
	models := b.models
	b.mu.Unlock()
	if stopCtx != nil {
		stopCtx()
	}
	if stopView != nil {
		stopView()
	}

	// Operation.Close cancels provider requests and owned runs before joining
	// their internal request gates. The bridge's own producers are joined after
	// it; they only post owner work and never wait for that work to run.
	models.Close()
	b.hookMu.Lock()
	for id, scope := range b.hookScopes {
		delete(b.hookScopes, id)
		scope.cancel()
	}
	b.hookMu.Unlock()

	if b.joinHostClose {
		b.op.CloseClient()
	} else {
		b.op.Close()
	}
	b.workers.Wait()
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
	case "list_models", "spawn_agent", "observe_agent", "wait_agent", "queue_agent", "wait_task", "wait_tasks", "read_task_result", "steer_agent", "cancel_task", "stop_agent", "execution_get", "context_get", "context_set_runtime_info":
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

func reserveRuntime(rt *quickjs.Runtime) error {
	installations.Lock()
	if _, exists := installations.runtimes[rt]; exists {
		installations.Unlock()
		return errors.New("agent: runtime already has an installation")
	}
	installations.runtimes[rt] = struct{}{}
	installations.Unlock()
	rt.OnClose(func() { installations.Lock(); delete(installations.runtimes, rt); installations.Unlock() })
	return nil
}
