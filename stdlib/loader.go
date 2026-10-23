package stdlib

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
)

// FileLoader snapshots exact bare-specifier mappings and creates a fresh local
// file loader for each execution. Relative map targets use that execution's
// cwd. Imports read only its selected filesystem; the helper grants no access
// of its own. A nil map enables ordinary relative/absolute local file imports.
func FileLoader(imports map[string]string) LoaderFactory {
	entries := make(map[string]string, len(imports))
	for name, target := range imports {
		entries[name] = target
	}
	// String maps have no unsupported JSON values. Keeping encoded data also
	// isolates factories invoked concurrently from caller mutations.
	data, _ := json.Marshal(struct {
		Imports map[string]string `json:"imports"`
	}{entries})
	return func(ctx context.Context, origin Origin) (quickjs.ModuleLoader, error) {
		parse := modulemap.Parse
		// A Windows rooted filesystem uses slash paths without a host volume.
		if runtime.GOOS == "windows" && filepath.VolumeName(origin.Cwd) == "" {
			parse = modulemap.ParseVirtual
		}
		snapshot, err := parse(data, origin.Cwd)
		if err != nil {
			return nil, err
		}
		entry, code := "", ""
		if origin.Source != nil && !origin.Virtual {
			entry, code = origin.Entry, origin.Source.Code
		}
		loader := modulemap.NewReaderLoader(snapshot, entry, code, func(name string) ([]byte, error) {
			if origin.FileSystem == nil {
				return nil, errors.New("module filesystem is unavailable")
			}
			return origin.FileSystem.ReadFile(ctx, name)
		})
		return loader.Load, nil
	}
}
