package stdlib

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"runtime"

	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// LoaderOptions selects exact import mappings and optional HTTP loading.
// Imports and HTTP option values are copied by ModuleLoader.
type LoaderOptions struct {
	Imports map[string]string
	HTTP    *ModuleHTTPOptions
}

// ModuleHTTPOptions enables HTTP/HTTPS imports independently of FS and Fetch.
// Client and Allow are borrowed. MaxBodyBytes is 0 for the default 32 MiB,
// negative for no limit, and otherwise limits the decompressed source body.
type ModuleHTTPOptions struct {
	Client       *http.Client
	Allow        func(*http.Request) error
	MaxBodyBytes int64
}

type moduleLoaderProvider struct {
	data []byte
	http *ModuleHTTPOptions
}

// ModuleLoader returns an immutable provider. Each NewLoader call creates
// independent runtime-local sources; no download cache is shared across runs.
func ModuleLoader(options LoaderOptions) ModuleLoaderProvider {
	p := &moduleLoaderProvider{data: encodeImports(options.Imports)}
	if options.HTTP != nil {
		v := *options.HTTP
		p.http = &v
	}
	return p
}

func (p *moduleLoaderProvider) URLImports() URLImportSupport {
	if p.http == nil {
		return URLImportsDisabled
	}
	return URLImportsEnabled
}

func (p *moduleLoaderProvider) NewLoader(ctx context.Context, origin Origin) (quickjs.ModuleLoader, error) {
	return newModuleLoader(ctx, origin, p.data, p.http)
}

// FileLoader snapshots exact bare-specifier mappings and creates a fresh local
// file loader for each execution. Relative map targets use that execution's
// cwd. Imports read only its selected filesystem; the helper grants no access
// of its own. A nil map enables ordinary relative/absolute local file imports.
// HTTP/HTTPS targets are rejected; enabling Fetch does not change this.
func FileLoader(imports map[string]string) LoaderFactory {
	data := encodeImports(imports)
	return func(ctx context.Context, origin Origin) (quickjs.ModuleLoader, error) {
		return newModuleLoader(ctx, origin, data, nil)
	}
}

func encodeImports(imports map[string]string) []byte {
	entries := make(map[string]string, len(imports))
	for name, target := range imports {
		entries[name] = target
	}
	// JSON encoding also isolates concurrent factories from caller mutations.
	data, _ := json.Marshal(struct {
		Imports map[string]string `json:"imports"`
	}{entries})
	return data
}

func newModuleLoader(ctx context.Context, origin Origin, data []byte, options *ModuleHTTPOptions) (quickjs.ModuleLoader, error) {
	parse := modulemap.Parse
	// A Windows rooted filesystem uses slash paths without a host volume.
	if runtime.GOOS == "windows" && filepath.VolumeName(origin.Cwd) == "" {
		parse = modulemap.ParseVirtual
	}
	snapshot, err := parse(data, origin.Cwd)
	if err != nil {
		return nil, err
	}
	if options == nil {
		for key, target := range snapshot.Imports() {
			if modulemap.IsHTTPURL(target) {
				return nil, fmt.Errorf("HTTP/HTTPS target for import map key %q is unsupported by the local module loader", key)
			}
		}
	}
	entry, code := "", ""
	if origin.Source != nil && !origin.Virtual {
		entry, code = origin.Entry, origin.Source.Code
	}
	var remote modulemap.RemoteReader
	if options != nil {
		remote = httpModuleReader(ctx, *options)
	}
	loader := modulemap.NewRemoteLoader(snapshot, entry, code, func(name string) ([]byte, error) {
		if origin.FileSystem == nil {
			return nil, errors.New("module filesystem is unavailable")
		}
		return origin.FileSystem.ReadFile(ctx, name)
	}, remote)
	entryPending := origin.Worker && origin.Source == nil
	return func(specifier, referrer string) (string, string, error) {
		if entryPending {
			entryPending = false
			if specifier == origin.Entry {
				return loader.LoadLocal(specifier, referrer)
			}
		}
		return loader.Load(specifier, referrer)
	}, nil
}
