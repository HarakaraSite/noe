package filesystem

import (
	"context"
	"io/fs"
)

// View borrows a backend while binding Go operations to one execution. It has
// no Close method; only the execution owner controls its lifetime.
type View struct {
	ctx     context.Context
	backend *Backend
}

func WithContext(ctx context.Context, backend *Backend) *View {
	return &View{ctx: ctx, backend: backend}
}

func (v *View) operation(ctx context.Context) (context.Context, func()) {
	combined, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(v.ctx, cancel)
	if v.ctx.Err() != nil {
		cancel()
	}
	return combined, func() { stop(); cancel() }
}

func (v *View) result(ctx context.Context, err error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := v.ctx.Err(); err != nil {
		return err
	}
	return err
}

func (v *View) ReadFile(ctx context.Context, p string) ([]byte, error) {
	op, stop := v.operation(ctx)
	defer stop()
	data, err := v.backend.ReadFile(op, p)
	if err = v.result(ctx, err); err != nil {
		return nil, err
	}
	return data, nil
}

func (v *View) Stat(ctx context.Context, p string) (fs.FileInfo, error) {
	op, stop := v.operation(ctx)
	defer stop()
	info, err := v.backend.Stat(op, p)
	if err = v.result(ctx, err); err != nil {
		return nil, err
	}
	return info, nil
}

func (v *View) WriteFileAtomic(ctx context.Context, p string, data []byte) error {
	op, stop := v.operation(ctx)
	defer stop()
	return v.result(ctx, v.backend.WriteFileAtomic(op, p, data))
}

func (v *View) CheckWritable() error {
	if err := v.ctx.Err(); err != nil {
		return err
	}
	return v.backend.CheckWritable()
}

func (v *View) PhysicalCwd() string { return v.backend.PhysicalCwd() }
