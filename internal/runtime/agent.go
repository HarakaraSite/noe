package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/observation"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installAgent(opts Options) error {
	settings := agent.Options{}
	if opts.Agent != nil {
		settings = *opts.Agent
	}
	settings.Cwd = h.cwd
	settings.ImportMap = opts.ImportMap
	h.agents = agent.NewHost(settings)
	// OnClose runs after QuickJS shutdown: this callback only touches Go state.
	h.rt.OnClose(h.agents.Close)
	if err := h.rt.SetModule("noe:image/internal", map[string]any{"request": h.agentRequest}); err != nil {
		return err
	}
	if err := h.rt.SetModule("noe:decision/internal", map[string]any{"request": h.agentRequest}); err != nil {
		return err
	}
	return h.rt.SetModule("noe:agent/internal", map[string]any{
		"request":        h.agentRequest,
		"defaults":       h.agents.Defaults,
		"instructions":   agent.Instructions,
		"observe":        h.agentObserve,
		"beginHookScope": h.agentBeginHookScope,
		"requestHook":    h.agentRequestHook,
		"endHookScope":   h.agentEndHookScope,
		"observeHook":    h.agentObserveHook,
	})
}

// agentObserve assembles a log snapshot in Go; QuickJS Promise settlement stays
// on the runtime owner just like provider requests. No broker or credentials are
// needed to read a saved or currently appended events file.
func (h *host) agentObserve(payload string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent observation outside an evaluation"))
		return promise
	}
	var query observation.Query
	if err := json.Unmarshal([]byte(payload), &query); err != nil {
		promise.RejectError(err)
		return promise
	}
	if query.LogPath != "" && !filepath.IsAbs(query.LogPath) {
		query.LogPath = filepath.Join(h.cwd, query.LogPath)
	}
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		snapshot, err := op.ObserveLog(query)
		var encoded []byte
		if err == nil {
			encoded, err = json.Marshal(snapshot)
		}
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(string(encoded))
			}
		})
	}()
	return promise
}

// beginAgentOperation is called on the owner for each Execute/Eval/Complete.
// Imported functions consult this current operation at invocation time.
func (h *host) beginAgentOperation(ctx context.Context) func() {
	previous := h.agentOperation
	op := h.agents.NewOperation(ctx)
	h.agentOperation = op
	return func() {
		h.closeAgentHookScopes(op)
		op.Close()
		h.agentOperation = previous
	}
}

func (h *host) agentRequest(method, payload string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent request outside an evaluation"))
		return promise
	}
	// Plain strings and Go state cross the goroutine boundary. Only the loop
	// owner may settle the QuickJS Promise, and Post drops work after close.
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		response, err := op.Call(method, payload)
		if err == nil && method == "spawn_agent" {
			var spawned struct {
				AgentID string `json:"agentId"`
			}
			if json.Unmarshal([]byte(response), &spawned) == nil {
				if done := op.OwnedLifetime(spawned.AgentID); done != nil {
					h.loop.std.Begin()
					go func() { defer h.loop.std.Done(); <-done }()
				}
			}
		}
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(response)
			}
		})
	}()
	return promise
}

func (h *host) agentBeginHookScope(payload string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent request outside an evaluation"))
		return promise
	}
	if !json.Valid([]byte(payload)) {
		promise.RejectError(errors.New("invalid hook scope request"))
		return promise
	}
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		response, err := op.Call("begin_hook_scope", payload)
		if err == nil {
			var scope struct {
				ScopeID string `json:"scopeId"`
			}
			if json.Unmarshal([]byte(response), &scope) != nil || scope.ScopeID == "" {
				err = errors.New("agent broker returned an invalid hook scope")
			} else if op.Context().Err() != nil {
				err = op.Context().Err()
				cleanup, _ := json.Marshal(map[string]string{"scopeId": scope.ScopeID, "status": "cancelled"})
				_, _ = op.Call("end_hook_scope", string(cleanup))
			} else {
				ctx, cancel := context.WithCancel(op.Context())
				h.hookMu.Lock()
				if h.hookScopes == nil {
					h.hookScopes = make(map[string]runtimeHookScope)
				}
				if _, exists := h.hookScopes[scope.ScopeID]; exists {
					cancel()
					err = errors.New("agent broker reused a hook scope")
				} else {
					h.hookScopes[scope.ScopeID] = runtimeHookScope{owner: op, ctx: ctx, cancel: cancel}
				}
				h.hookMu.Unlock()
				if err != nil {
					cleanup, _ := json.Marshal(map[string]string{"scopeId": scope.ScopeID, "status": "cancelled"})
					_, _ = op.Call("end_hook_scope", string(cleanup))
				}
			}
		}
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(response)
			}
		})
	}()
	return promise
}

func (h *host) agentRequestHook(scopeID, method, payload string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent request outside an evaluation"))
		return promise
	}
	h.hookMu.Lock()
	scope, ok := h.hookScopes[scopeID]
	h.hookMu.Unlock()
	if !ok || scope.owner != op || scope.ctx.Err() != nil {
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
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		response, err := op.CallWithScope(scope.ctx, scopeID, method, payload)
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(response)
			}
		})
	}()
	return promise
}

func hookAPIMethod(method string) bool {
	switch method {
	case "list_models", "spawn_agent", "observe_agent", "wait_agent", "queue_agent", "steer_agent", "interrupt_agent", "execution_get", "context_get", "context_set_runtime_info":
		return true
	}
	return false
}

func (h *host) agentEndHookScope(scopeID, status, errorText string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent request outside an evaluation"))
		return promise
	}
	h.hookMu.Lock()
	scope, ok := h.hookScopes[scopeID]
	if ok && scope.owner == op {
		delete(h.hookScopes, scopeID)
		scope.cancel()
	} else {
		ok = false
	}
	h.hookMu.Unlock()
	if !ok {
		promise.Resolve(`{"closed":false}`)
		return promise
	}
	encoded, _ := json.Marshal(map[string]string{"scopeId": scopeID, "status": status, "error": errorText})
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		response, err := op.Call("end_hook_scope", string(encoded))
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(response)
			}
		})
	}()
	return promise
}

func (h *host) agentObserveHook(scopeID, payload string) *quickjs.Promise {
	promise := h.rt.NewPromise()
	op := h.agentOperation
	if op == nil {
		promise.RejectError(errors.New("agent observation outside an evaluation"))
		return promise
	}
	h.hookMu.Lock()
	scope, ok := h.hookScopes[scopeID]
	h.hookMu.Unlock()
	if !ok || scope.owner != op || scope.ctx.Err() != nil {
		promise.RejectError(errors.New("hook API scope has ended"))
		return promise
	}
	var query observation.Query
	if err := json.Unmarshal([]byte(payload), &query); err != nil {
		promise.RejectError(err)
		return promise
	}
	if query.LogPath != "" && !filepath.IsAbs(query.LogPath) {
		query.LogPath = filepath.Join(h.cwd, query.LogPath)
	}
	h.loop.std.Begin()
	go func() {
		defer h.loop.std.Done()
		snapshot, err := observation.Observe(scope.ctx, query)
		var encoded []byte
		if err == nil {
			encoded, err = json.Marshal(snapshot)
		}
		h.loop.std.Post(func() {
			if err != nil {
				promise.RejectError(err)
			} else {
				promise.Resolve(string(encoded))
			}
		})
	}()
	return promise
}

func (h *host) closeAgentHookScopes(owner *agent.Operation) {
	h.hookMu.Lock()
	for id, scope := range h.hookScopes {
		if scope.owner == owner {
			delete(h.hookScopes, id)
			scope.cancel()
		}
	}
	h.hookMu.Unlock()
}
