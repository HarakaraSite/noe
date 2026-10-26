package jsexec

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Source is an entry's source text and its identifier. Virtual sources keep
// their supplied name and resolve relative imports from Environment.Cwd.
type Source struct {
	Name    string
	Code    string
	Module  bool
	Virtual bool
}

// Origin identifies the entry and execution view for one new module loader.
type Origin struct {
	// Source is a snapshot of an already supplied entry, or nil while loading
	// a Worker entry for the first time. Factories can seed its module cache.
	Source     *Source
	Entry      string
	Cwd        string
	Virtual    bool
	Worker     bool
	FileSystem FileSystem
}

// LoaderFactory creates a runtime-local module loader on the runtime owner.
type LoaderFactory func(context.Context, Origin) (quickjs.ModuleLoader, error)

// executionLoader owns one execution's loader factory, defined sources and
// preparation state. QuickJS operations, factory calls and loads stay on the
// runtime owner. Module registration and preparation share the binding's
// lifetime mutex with Close; there is no separate loader closed state.
type executionLoader struct {
	state        *bindingsState
	rt           *quickjs.Runtime
	ctx          context.Context
	cwd          string
	backend      *filesystem.Backend
	fs           FileSystem
	worker       bool
	factory      LoaderFactory
	modules      map[string]string
	load         quickjs.ModuleLoader
	prepared     bool
	virtualEntry string
}

// defineModule registers owner-provided JavaScript source without replacing
// the Profile loader. Native modules continue to take normal precedence.
func (l *executionLoader) defineModule(specifier, source string) error {
	if strings.TrimSpace(specifier) == "" {
		return errors.New("stdlib: module specifier is empty")
	}
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	if l.state.closed {
		return errors.New("stdlib: bindings are closed")
	}
	l.modules[specifier] = source
	return nil
}

// installManagedModules registers the private entry helper and wrapper as one
// owner operation. A collision in Profile-defined modules is reported to the
// caller so it can choose another private specifier without replacing it.
func (l *executionLoader) installManagedModules(helperName, wrapperName, wrapperSource string, helperExports map[string]any) (bool, error) {
	l.state.mu.Lock()
	defer l.state.mu.Unlock()
	if l.state.closed {
		return false, errors.New("stdlib: bindings are closed")
	}
	if _, exists := l.modules[helperName]; exists {
		return false, nil
	}
	if _, exists := l.modules[wrapperName]; exists {
		return false, nil
	}
	if err := l.rt.SetModule(helperName, helperExports); err != nil {
		return false, err
	}
	l.modules[wrapperName] = wrapperSource
	return true, nil
}

func (l *executionLoader) prepare(source Source, includeEntry bool) (Source, error) {
	if source.Name == "" {
		return Source{}, errors.New("stdlib: source name is required")
	}
	normalized, err := normalizeSource(source, l.cwd, l.backend)
	if err != nil {
		return Source{}, err
	}
	sourceSnapshot := normalized
	origin := Origin{Source: &sourceSnapshot, Entry: normalized.Name, Cwd: l.cwd, Virtual: source.Virtual, Worker: l.worker, FileSystem: l.fs}
	var entry *Source
	if includeEntry {
		entry = &normalized
	}
	if err := l.installLoader(origin, entry); err != nil {
		return Source{}, err
	}
	return normalized, nil
}

func (l *executionLoader) installLoader(origin Origin, entry *Source) error {
	l.state.mu.Lock()
	closed, prepared, hasModules := l.state.closed, l.prepared, len(l.modules) > 0
	l.state.mu.Unlock()
	if closed {
		return errors.New("stdlib: bindings are closed")
	}
	if prepared {
		return errors.New("stdlib: an entry was already prepared")
	}
	if err := l.ctx.Err(); err != nil {
		return err
	}
	var loader quickjs.ModuleLoader
	var err error
	if l.factory != nil {
		loader, err = l.factory(l.ctx, origin)
		if err != nil {
			return err
		}
		if loader == nil {
			return errors.New("stdlib: loader factory returned nil")
		}
	}
	l.virtualEntry = ""
	if origin.Virtual {
		l.virtualEntry = origin.Entry
	}
	l.load = func(specifier, referrer string) (string, string, error) {
		if err := l.ctx.Err(); err != nil {
			return "", "", err
		}
		if entry != nil && specifier == entry.Name {
			return entry.Code, entry.Name, nil
		}
		l.state.mu.Lock()
		module, ok := l.modules[specifier]
		l.state.mu.Unlock()
		if ok {
			return module, specifier, nil
		}
		if loader == nil {
			return "", "", fmt.Errorf("stdlib: no module loader for %q from %q", specifier, referrer)
		}
		// Virtual entry IDs are kept in stacks; their imports use the fixed
		// environment cwd instead of pretending the ID is a disk file.
		if origin.Virtual && referrer == origin.Entry {
			referrer = virtualEntryPath(origin.Cwd)
		}
		return loader(specifier, referrer)
	}
	// Advanced Attach can preserve a loader installed directly by the caller.
	if l.factory != nil || hasModules || entry != nil {
		l.rt.SetModuleLoader(l.load)
	}
	l.state.mu.Lock()
	l.prepared = true
	l.state.mu.Unlock()
	return nil
}

// loadModule resolves a source through this execution's prepared loader. It
// runs on the runtime owner and returns Go text/IDs, never a shared JS value.
func (l *executionLoader) loadModule(specifier, referrer string) (source, resolved string, err error) {
	if l.load == nil {
		return "", "", errors.New("stdlib: entry is not prepared")
	}
	return l.load(specifier, referrer)
}

func (l *executionLoader) prepareWorker(specifier string, eval bool, requestedType string) (source, name string, module bool, err error) {
	if eval {
		module = requestedType == "module" || requestedType != "classic" && l.rt.CheckSyntax(specifier) != nil && l.rt.CheckModuleSyntax(specifier) == nil
		entry, err := l.prepare(Source{Name: "[worker eval]", Code: specifier, Virtual: true, Module: module}, false)
		return entry.Code, entry.Name, entry.Module, err
	}
	origin := Origin{Entry: specifier, Cwd: l.cwd, Worker: true, FileSystem: l.fs}
	if err := l.installLoader(origin, nil); err != nil {
		return "", "", false, err
	}
	source, name, err = l.loadModule(specifier, filepath.Join(l.cwd, "<worker-entry>.mjs"))
	if err != nil {
		return "", "", false, err
	}
	return source, name, requestedType != "classic", nil
}

func normalizeSource(source Source, cwd string, backend *filesystem.Backend) (Source, error) {
	if source.Virtual {
		return source, nil
	}
	if backend != nil && backend.Root() != "" {
		name := filepath.ToSlash(source.Name)
		if !strings.HasPrefix(name, "/") {
			name = path.Join(cwd, name)
		}
		name = path.Clean("/" + strings.TrimPrefix(name, "/"))
		if _, err := backend.Resolve(name, true); err != nil {
			return Source{}, fmt.Errorf("stdlib: source path: %w", err)
		}
		source.Name = name
		return source, nil
	}
	if !filepath.IsAbs(source.Name) {
		source.Name = filepath.Join(cwd, source.Name)
	}
	name, err := filepath.Abs(filepath.Clean(source.Name))
	if err != nil {
		return Source{}, err
	}
	source.Name = name
	return source, nil
}
