package runtime

import (
	"context"
	"errors"

	"noe/internal/agent"

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
	return h.rt.SetModule("noe:agent/internal", map[string]any{
		"request":  h.agentRequest,
		"defaults": h.agents.Defaults,
	})
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
