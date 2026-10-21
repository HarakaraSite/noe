package auth

import (
	"context"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/internal/authbridge"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// Binding owns one runtime's authentication work. It borrows its execution
// view and HTTP client. Close is safe to call concurrently and repeatedly.
type Binding struct {
	native *authbridge.Binding
	cancel context.CancelFunc
	stop   func() bool
}

// Bind installs noe:auth independently of an agent Host. Call it on the
// runtime owner before entry evaluation, for example from Profile.Initialize,
// and return its Close method as the initializer cleanup.
func Bind(ctx context.Context, view *stdlib.Bindings, options Options) (*Binding, error) {
	if ctx == nil || view == nil || view.Context() == nil {
		return nil, errors.New("auth: context and initialized execution bindings are required")
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(view.Context(), cancel)
	if err := view.Context().Err(); err != nil {
		stop()
		cancel()
		return nil, err
	}
	cwd := view.Environment().Cwd
	if fs := view.FileSystem(); fs != nil {
		cwd = fs.PhysicalCwd()
	}
	native, err := authbridge.Install(ctx, view.Runtime(), view.Loop(), cwd, options)
	if err != nil {
		stop()
		cancel()
		return nil, err
	}
	b := &Binding{native: native, cancel: cancel, stop: stop}
	if err := view.DefineModule("noe:auth", credentials.ModuleSource); err != nil {
		b.Close()
		return nil, err
	}
	return b, nil
}

func (b *Binding) Close() {
	if b == nil {
		return
	}
	b.stop()
	b.cancel()
	b.native.Close()
}
