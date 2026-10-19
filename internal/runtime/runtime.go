// Package runtime hosts go-quickjs for NOE scripts.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agentbridge"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

// Source keeps loading separate from execution.
type Source struct {
	Name string
	Code string
}

// Options describes the host environment for one execution.
type Options struct {
	// Stdin is borrowed and never closed by the runtime. Execute treats nil as
	// empty input.
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Args   []string
	Cwd    string
	// Agent supplies provider/child execution settings. Nil uses lazy defaults.
	Agent *core.Options
	// Auth supplies HTTP/discovery settings for native sign-in verification.
	Auth *credentials.Options

	// ImportMap is an immutable, absolute-target local module map.
	ImportMap *modulemap.Snapshot

	// SourceName is the entry name exposed in process.argv.
	SourceName string
}

// Execute creates a fresh runtime and runs source as an ES module.
func Execute(ctx context.Context, source Source, opts Options) (int, error) {
	opts.SourceName = source.Name
	if opts.Stdin == nil {
		opts.Stdin = strings.NewReader("")
	}
	h, err := newHost(ctx, opts, source)
	if err != nil {
		return 1, err
	}
	defer h.rt.Close()
	return h.run()
}

type host struct {
	rt            *quickjs.Runtime
	loop          *eventLoop
	ctx           context.Context
	source        Source
	unhandled     []error
	cwd           string
	importMap     *modulemap.Snapshot
	exitRequested bool
	exitCode      int
}

func newHost(ctx context.Context, opts Options, source Source) (*host, error) {
	if ctx == nil {
		return nil, errors.New("runtime context is required")
	}
	cwd := opts.Cwd
	var err error
	if cwd == "" {
		cwd, err = os.Getwd()
	}
	if err != nil {
		return nil, err
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return nil, err
	}
	entryName, err := filepath.Abs(source.Name)
	if err != nil {
		return nil, err
	}
	source.Name = entryName
	h := &host{rt: quickjs.New(), ctx: ctx, source: source, cwd: cwd, importMap: opts.ImportMap}
	h.loop, err = newEventLoop(h)
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	bindings, err := stdlib.Install(h.rt, stdlib.Config{
		Core: h.stdlibConfig(opts),
		IO:   &stdlib.IOConfig{Stdin: opts.Stdin, Cwd: cwd},
		Docs: true,
	})
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	// Agent and auth borrow the same output boundary as generic APIs.
	opts.Stdout, opts.Stderr = bindings.Stdout, bindings.Stderr
	loader := modulemap.NewLoader(h.importMap, source.Name, source.Code)
	if err := h.installAgent(ctx, opts, loader.Load); err != nil {
		h.rt.Close()
		return nil, err
	}
	return h, nil
}

func (h *host) recordUnhandled(reason quickjs.Value) {
	message := reason.String()
	var trace string
	if reason.IsObject() {
		if stack, err := reason.Get("stack"); err == nil && stack.IsString() && stack.String() != "" {
			trace = stack.String()
		}
	}
	h.unhandled = append(h.unhandled, snapshotException(reason, message, trace, nil))
	h.loop.interrupt()
}

func (h *host) run() (code int, err error) {
	stopStdlib := context.AfterFunc(h.ctx, h.loop.std.Close)
	defer stopStdlib()
	defer func() {
		if h.exitRequested {
			code, err = h.exitCode, nil
		}
	}()
	specifier, err := json.Marshal("./" + filepath.Base(h.source.Name))
	if err != nil {
		return 1, err
	}
	bootstrap := `(() => { const p = import(` + string(specifier) + `); p.catch(() => {}); return p; })()`
	p, err := h.rt.EvalFileContext(h.ctx, filepath.Join(filepath.Dir(h.source.Name), "<noe-bootstrap>.js"), bootstrap)
	if err == nil {
		_, err = h.loop.await(h.ctx, p)
	}
	if err != nil {
		return 1, retainException(err)
	}
	if err := h.drain(h.ctx); err != nil {
		return 1, err
	}
	if err := h.takeUnhandled(); err != nil {
		return 1, err
	}
	if code, ok := stdlib.ExitCode(h.rt); ok {
		return code, nil
	}
	return 0, nil
}

func (h *host) installAgent(ctx context.Context, opts Options, load quickjs.ModuleLoader) error {
	settings := core.Options{}
	if opts.Agent != nil {
		settings = *opts.Agent
	}
	settings.Cwd = h.cwd
	settings.ImportMap = opts.ImportMap

	// The CLI has historically kept noe:auth available even when no explicit
	// HTTP/discovery overrides are configured. The public embedding facade uses
	// nil Auth as an opt-out, so the private runtime path supplies its zero-value
	// settings explicitly for compatibility.
	auth := opts.Auth
	if auth == nil {
		auth = &credentials.Options{}
	}

	installPrivate := func() error {
		_, err := agentbridge.Install(ctx, h.rt, agentbridge.Config{
			Loop: h.loop.std, Agent: settings, Auth: auth, Load: load,
		})
		return err
	}
	if opts.Agent == nil || settings.Executable == "" || settings.Connection != nil || settings.ProviderOptions.Profile != nil {
		return installPrivate()
	}

	var childMap json.RawMessage
	if opts.ImportMap != nil {
		var err error
		childMap, err = json.Marshal(opts.ImportMap)
		if err != nil {
			return err
		}
	}
	controlDir := settings.ControlDir
	if settings.EnableControl && controlDir == "" {
		var err error
		controlDir, err = core.DefaultControlDir()
		if err != nil {
			// Preserve the old lazy resolution: a host that never starts an
			// agent did not fail just because the CLI default could not be
			// resolved. The private host still resolves it on first broker use.
			return installPrivate()
		}
	}
	binding, err := publicagent.Install(ctx, h.rt, publicagent.Config{
		Loop: h.loop.std, Cwd: h.cwd, Executable: settings.Executable,
		Load: load, ChildImportMap: childMap,
		Providers: publicagent.ProviderOptions{
			ConfigFile: settings.ProviderOptions.ConfigFile, UserAgent: settings.ProviderOptions.UserAgent,
			KeyFile: settings.ProviderOptions.KeyFile, BaseURL: settings.ProviderOptions.BaseURL,
			HTTPClient: settings.ProviderOptions.HTTPClient, DecisionServerURL: settings.ProviderOptions.DecisionServerURL,
		},
		Provider: settings.Provider, DecisionProvider: settings.DecisionProvider, ImageProvider: settings.ImageProvider,
		Auth:  &publicagent.AuthOptions{HTTPClient: auth.HTTPClient, DiscoveryURL: auth.DiscoveryURL},
		Trace: settings.Trace, Observe: settings.Observe, ControlDir: controlDir,
	})
	if err != nil {
		return err
	}
	_ = binding // The runtime close hook owns this binding's Go lifetime.
	return nil
}

func (h *host) drain(ctx context.Context) error {
	return h.loop.drain(ctx)
}

func (h *host) takeUnhandled() error {
	err := errors.Join(h.unhandled...)
	h.unhandled = nil
	return err
}
