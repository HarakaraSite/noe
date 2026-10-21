package log

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/internal/logbinding"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type Binding struct{ inner *logbinding.Binding }

func Bind(ctx context.Context, view *stdlib.Bindings) (*Binding, error) {
	binding, err := logbinding.Bind(ctx, view)
	if err != nil {
		return nil, err
	}
	return &Binding{inner: binding}, nil
}
func (b *Binding) Close() {
	if b != nil {
		b.inner.Close()
	}
}
