package runtime

import (
	"context"
	"os"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installFileMutations(noe quickjs.Value) error {
	operations := h.rt.NewObject()
	if err := operations.Set("mkdir", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return nil, mkdirContext(ctx, path)
		})
	}); err != nil {
		return err
	}
	if err := operations.Set("rename", func(from, to string) quickjs.Value {
		from, to = h.filePath(from), h.filePath(to)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return nil, renameContext(ctx, from, to)
		})
	}); err != nil {
		return err
	}
	if err := operations.Set("remove", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return nil, removeContext(ctx, path)
		})
	}); err != nil {
		return err
	}

	factory, err := h.rt.EvalFile("<noe-file-mutations>", `(operations => ({
		mkdir: async function mkdir(path) {
			await operations.mkdir(path);
		},
		rename: async function rename(from, to) {
			await operations.rename(from, to);
		},
		remove: async function remove(path) {
			await operations.remove(path);
		},
	}))`)
	if err != nil {
		return err
	}
	api, err := factory.Call(operations)
	if err != nil {
		return err
	}
	for _, name := range []string{"mkdir", "rename", "remove"} {
		method, err := api.Get(name)
		if err != nil {
			return err
		}
		if err := noe.Set(name, method); err != nil {
			return err
		}
	}
	return nil
}

func mkdirContext(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0755); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	return ctx.Err()
}

func renameContext(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Rename(from, to); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	return ctx.Err()
}

func removeContext(ctx context.Context, path string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	return ctx.Err()
}
