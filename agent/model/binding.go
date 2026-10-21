package model

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/internal/modelbinding"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// Binding owns only its model requests and borrowed execution registration.
// Close joins producers without closing the service, runtime or filesystem.
type Binding struct{ inner *modelbinding.Binding }

func (b *Binding) Close() {
	if b != nil {
		b.inner.Close()
	}
}

// Bind installs noe:model (generate/list_models), noe:decision and the
// file-oriented noe:image helpers. Saving uses exactly this execution's FS.
func Bind(ctx context.Context, view *stdlib.Bindings, service Service) (*Binding, error) {
	binding, err := modelbinding.Bind(ctx, view, service, modelbinding.Options{})
	if err != nil {
		return nil, err
	}
	return &Binding{inner: binding}, nil
}
