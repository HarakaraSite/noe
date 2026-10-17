// Package runtime hosts go-quickjs for NOE scripts.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"noe/internal/agent"
	"noe/internal/credentials"
	"noe/internal/decision"
	"noe/internal/hostio"
	"noe/internal/modulemap"
	"noe/internal/nativeio"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

// Source keeps loading separate from execution.
type Source struct {
	Name string
	Code string
}

// Options describes the host environment for one execution.
type Options struct {
	// Stdin is borrowed and never closed by the runtime. Execute treats nil as
	// empty input; REPL sessions reserve stdin for source.
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
	inspect        quickjs.Value
	cancelProbe    quickjs.Value
	completion     quickjs.Value
	agents         *agent.Host
	agentOperation *agent.Operation
	ioHost         *nativeio.Host
}

func newHost(opts Options) (*host, error) {
	// One writer boundary is shared by console, native streams and Workers.
	// The pinned stdlib's own wrappers still receive these same writers.
	stdout := hostio.NewSharedWriter(opts.Stdout)
	// Preserve aliases before replacing the destinations with wrappers. Stdlib
	// and Workers must see the same lock when both channels share one sink.
	sameOutput := opts.Stdout == nil && opts.Stderr == nil || (opts.Stdout != nil && reflect.TypeOf(opts.Stdout).Comparable() && opts.Stdout == opts.Stderr)
	stderr := stdout
	if !sameOutput {
		stderr = hostio.NewSharedWriter(opts.Stderr)
	}
	opts.Stdout, opts.Stderr = stdout, stderr
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
	h.rt.OnClose(func() { _ = stdout.Close(); _ = stderr.Close() })
	h.loop, err = newEventLoop(h)
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	probe, err := h.rt.EvalFile("<noe-cancel-probe>", `() => { while (true) {} }`)
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	h.cancelProbe = probe
	completion, err := h.rt.EvalFile("<noe-completion>", completionHelperSource)
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	h.completion = completion
	if err := h.installStdlib(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	h.ioHost, err = nativeio.Install(h.rt, nativeio.Options{Stdin: opts.Stdin, Stdout: opts.Stdout, Stderr: opts.Stderr, Cwd: cwd})
	if err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.installAgent(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.installAuth(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.loop.trackTimers(); err != nil {
		h.rt.Close()
		return nil, err
	}

	if err := h.loop.trackResources(); err != nil {
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
	h.installLoader(&Source{Name: name, Code: source.Code})
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

func (h *host) installLoader(entry *Source) {
	var name, code string
	if entry != nil {
		name, code = entry.Name, entry.Code
	}
	local := modulemap.NewLoader(h.importMap, name, code)
	h.rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		switch specifier {
		case "noe:agent":
			return agent.ModuleSource, specifier, nil
		case "noe:decision":
			return decision.ModuleSource, specifier, nil
		case "noe:auth":
			return credentials.ModuleSource, specifier, nil
		case "noe:agent/task":
			return agent.TaskModuleSource, specifier, nil
		}
		return local.Load(specifier, referrer)
	})
}

func (h *host) drain(ctx context.Context) error {
	return h.loop.drain(ctx)
}

// drainCancelled discards jobs from an interrupted REPL operation. Active
// stdlib I/O is handled by terminating the session before reaching this path.
func (h *host) drainCancelled(ctx context.Context) error {
	return h.loop.drainCancelled(ctx)
}

// discardCancelledJobs makes the runtime observe an abort in a pre-created
// function call. The function call begins before AbortOn's closed signal is
// polled, and the interrupted turn drops its queued Promise reactions.
func (h *host) discardCancelledJobs() error {
	interrupted := make(chan struct{})
	close(interrupted)
	h.rt.AbortOn(interrupted)
	_, err := h.cancelProbe.CallContext(context.Background())
	h.rt.AbortOn(nil)
	if errors.Is(err, context.Canceled) {
		return nil
	}
	if err == nil {
		return errors.New("cancellation cleanup probe returned without interruption")
	}
	return err
}

func (h *host) takeUnhandled() error {
	err := errors.Join(h.unhandled...)
	h.unhandled = nil
	return err
}
