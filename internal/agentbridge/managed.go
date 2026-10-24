package agentbridge

import (
	"context"
	"errors"

	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/modelbinding"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// AttachOperation binds an actual execution scope. It borrows the Host and
// view, and owns the supplied operation after success.
func AttachOperation(ctx context.Context, view *stdlib.Bindings, host *core.Host, op *core.Operation) (*Binding, error) {
	return attachOperation(ctx, view, host, op, false)
}

// AttachClient owns an embedding client's scope. Unlike an executor binding,
// its Close can join an already-closing Host without a task-owner cycle.
func AttachClient(ctx context.Context, view *stdlib.Bindings, host *core.Host, op *core.Operation) (*Binding, error) {
	return attachOperation(ctx, view, host, op, true)
}

func attachOperation(ctx context.Context, view *stdlib.Bindings, host *core.Host, op *core.Operation, joinHostClose bool) (*Binding, error) {
	if ctx == nil || view == nil || view.Context() == nil || view.Runtime() == nil || view.Loop() == nil || host == nil || op == nil {
		return nil, errors.New("agent: live bindings, context, host and operation are required")
	}
	if view.Worker() {
		return nil, errors.New("agent: Worker bindings are unsupported")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := view.Context().Err(); err != nil {
		return nil, err
	}
	if err := reserveRuntime(view.Runtime()); err != nil {
		return nil, err
	}
	b := &Binding{rt: view.Runtime(), loop: view.Loop(), view: view, op: op, joinHostClose: joinHostClose,
		closeDone: make(chan struct{}), hookScopes: make(map[string]hookScope)}
	for _, id := range []string{
		"noe:agent", "noe:agent/api", "noe:agent/hooks", "noe:agent/task", "noe:agent/history",
		"noe:agent/task-protocol", "noe:agent/run-js", "noe:agent/tool-pipeline", "noe:image", "noe:decision",
	} {
		source, _ := moduleSource(id)
		if err := view.DefineModule(id, source); err != nil {
			b.Close()
			return nil, err
		}
	}
	if err := b.installModules(); err != nil {
		b.Close()
		return nil, err
	}
	models, err := modelbinding.Bind(ctx, view, op.Models(), modelbinding.Options{ReadImage: op.ReadImage, Run: op.WithRequest, AfterSave: op.ObserveImageSave})
	if err != nil {
		b.Close()
		return nil, err
	}
	b.models = models
	if joinHostClose {
		// An embedding client owns this binding and needs context/runtime
		// cancellation to close it automatically. Managed task/tool bindings
		// are closed by their runtime owner; letting process.exit cancellation
		// close the binding here would also close the Operation and cancel the
		// caller context before the executor records the requested exit code.
		stopCtx := context.AfterFunc(ctx, b.Close)
		stopView := context.AfterFunc(view.Context(), b.Close)
		b.mu.Lock()
		b.stopCtx, b.stopView = stopCtx, stopView
		closed := b.closed
		b.mu.Unlock()
		if closed {
			stopCtx()
			stopView()
			return nil, context.Canceled
		}
		view.Runtime().OnClose(b.Close)
	}
	return b, nil
}

func (b *Binding) defaults() string {
	return b.op.Defaults()
}

// resolveHookModule runs on the VM owner after this execution's loader exists.
// The canonical source is registered in the same loader, preserving relative
// imports and avoiding a second interpretation of its bare specifier.
func (b *Binding) resolveHookModule(specifier string, index int) (string, error) {
	if b.isClosed() {
		return "", context.Canceled
	}
	source, canonical, err := b.view.LoadModule(specifier, "[agent task]")
	if err != nil {
		return "", err
	}
	if canonical == "" {
		return "", errors.New("agent: hook loader returned an empty canonical ID")
	}
	if err := b.op.RecordResolvedHook(index, specifier, canonical); err != nil {
		return "", err
	}
	if err := b.view.DefineModule(canonical, source); err != nil {
		return "", err
	}
	return canonical, nil
}

func (b *Binding) instructions() string {
	return core.Facts()
}
