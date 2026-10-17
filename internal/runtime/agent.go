package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"

	"noe/internal/agent"
	"noe/internal/observation"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installAgent(opts Options) error {
	settings := agent.Options{}
	if opts.Agent != nil {
		settings = *opts.Agent
	}
	settings.Cwd = h.cwd
	h.agents = agent.NewHost(settings)
	// OnClose runs after QuickJS shutdown: this callback only touches Go state.
	h.rt.OnClose(h.agents.Close)
	if err := h.rt.SetModule("noe:decision/internal", map[string]any{"request": h.agentRequest}); err != nil {
		return err
	}
	return h.rt.SetModule("noe:agent/internal", map[string]any{
		"request":      h.agentRequest,
		"defaults":     h.agents.Defaults,
		"instructions": agent.Instructions,
		"observe":      h.agentObserve,
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
