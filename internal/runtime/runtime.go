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
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/credentials"
	"forge.harakara.site/littleisland/noe/v2/internal/decision"
	imageapi "forge.harakara.site/littleisland/noe/v2/internal/image"
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
	Agent *agent.Options
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
	h, err := newHost(opts)
	if err != nil {
		return 1, err
	}
	defer h.rt.Close()
	return h.run(ctx, source)
}

type host struct {
	rt             *quickjs.Runtime
	loop           *eventLoop
	unhandled      []error
	cwd            string
	importMap      *modulemap.Snapshot
	exitRequested  bool
	exitCode       int
	agents         *agent.Host
	agentOperation *agent.Operation
	hookMu         sync.Mutex
	hookScopes     map[string]runtimeHookScope
}

type runtimeHookScope struct {
	owner  *agent.Operation
	ctx    context.Context
	cancel context.CancelFunc
}

func newHost(opts Options) (*host, error) {
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
	h := &host{rt: quickjs.New(), cwd: cwd, importMap: opts.ImportMap}
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
	if err := h.installAgent(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.installAuth(opts); err != nil {
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

func (h *host) run(ctx context.Context, source Source) (code int, err error) {
	endOperation := h.beginAgentOperation(ctx)
	defer endOperation()
	stopStdlib := context.AfterFunc(ctx, h.loop.std.Close)
	defer stopStdlib()
	defer func() {
		if h.exitRequested {
			code, err = h.exitCode, nil
		}
	}()
	name, err := filepath.Abs(source.Name)
	if err != nil {
		return 1, err
	}
	h.installLoader(Source{Name: name, Code: source.Code})
	specifier, err := json.Marshal("./" + filepath.Base(name))
	if err != nil {
		return 1, err
	}
	bootstrap := `(() => { const p = import(` + string(specifier) + `); p.catch(() => {}); return p; })()`
	p, err := h.rt.EvalFileContext(ctx, filepath.Join(filepath.Dir(name), "<noe-bootstrap>.js"), bootstrap)
	if err == nil {
		_, err = h.loop.await(ctx, p)
	}
	if err != nil {
		return 1, retainException(err)
	}
	if err := h.drain(ctx); err != nil {
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

func (h *host) installLoader(entry Source) {
	local := modulemap.NewLoader(h.importMap, entry.Name, entry.Code)
	h.rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		switch specifier {
		case "noe:agent":
			return agent.ModuleSource, specifier, nil
		case "noe:agent/api":
			return agent.APIModuleSource, specifier, nil
		case "noe:agent/hooks":
			return agent.HooksModuleSource, specifier, nil
		case "noe:image":
			return imageapi.ModuleSource, specifier, nil
		case "noe:decision":
			return decision.ModuleSource, specifier, nil
		case "noe:auth":
			return credentials.ModuleSource, specifier, nil
		case "noe:agent/task":
			return agent.TaskModuleSource, specifier, nil
		case "noe:agent/history":
			return agent.HistoryModuleSource, specifier, nil
		}
		return local.Load(specifier, referrer)
	})
}

func (h *host) drain(ctx context.Context) error {
	return h.loop.drain(ctx)
}

func (h *host) takeUnhandled() error {
	err := errors.Join(h.unhandled...)
	h.unhandled = nil
	return err
}
