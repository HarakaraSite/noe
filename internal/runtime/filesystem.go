package runtime

import (
	"context"
	"os"
	"sort"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func (h *host) installFilesystem(noe quickjs.Value) error {
	if err := noe.Set("readDir", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return readDirContext(ctx, path)
		})
	}); err != nil {
		return err
	}
	return noe.Set("stat", func(path string) quickjs.Value {
		path = h.filePath(path)
		return h.startAsyncWork(func(ctx context.Context) (any, error) {
			return statContext(ctx, path)
		})
	})
}

func readDirContext(ctx context.Context, path string) ([]map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	result := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		mode := entry.Type()
		result = append(result, map[string]any{
			"name":        entry.Name(),
			"isFile":      mode.IsRegular(),
			"isDirectory": entry.IsDir(),
			"isSymlink":   mode&os.ModeSymlink != 0,
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func statContext(ctx context.Context, path string) (map[string]any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	mode := info.Mode()
	return map[string]any{
		"size":        info.Size(),
		"modifiedAt":  info.ModTime().UTC().Format(time.RFC3339Nano),
		"isFile":      mode.IsRegular(),
		"isDirectory": mode.IsDir(),
	}, nil
}
