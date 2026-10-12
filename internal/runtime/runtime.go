// Package runtime hosts go-quickjs for NOE scripts.
package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	Stdout io.Writer
	Stderr io.Writer
	Args   []string
	Cwd    string
}

// Execute creates a fresh runtime and runs source as an ES module.
func Execute(ctx context.Context, source Source, opts Options) (int, error) {
	h, err := newHost(opts)
	if err != nil {
		return 1, err
	}
	defer h.rt.Close()
	return h.run(ctx, source)
}

type host struct {
	rt            *quickjs.Runtime
	unhandled     []error
	cwd           string
	exitRequested bool
	exitCode      int
	inspect       quickjs.Value
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
	h := &host{rt: quickjs.New(), cwd: cwd}
	if err := h.installConsole(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.installNoe(opts); err != nil {
		h.rt.Close()
		return nil, err
	}
	if err := h.installFetch(); err != nil {
		h.rt.Close()
		return nil, err
	}
	h.rt.OnUnhandledRejection(func(reason quickjs.Value) {
		message := reason.String()
		if reason.IsObject() {
			if stack, err := reason.Get("stack"); err == nil && stack.IsString() && stack.String() != "" {
				message = stack.String()
			}
		}
		h.unhandled = append(h.unhandled, errors.New(message))
	})
	return h, nil
}

func (h *host) run(ctx context.Context, source Source) (code int, err error) {
	defer func() {
		if h.exitRequested && errors.Is(err, quickjs.ErrClosed) {
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
		_, err = p.Await(ctx)
	}
	if err != nil {
		return 1, err
	}
	if err := h.drain(ctx); err != nil {
		return 1, err
	}
	if err := h.takeUnhandled(); err != nil {
		return 1, err
	}
	return 0, nil
}

func (h *host) installLoader(entry *Source) {
	h.rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
			return "", "", fmt.Errorf("only relative local imports are supported: %q", specifier)
		}
		resolved := filepath.Clean(filepath.Join(filepath.Dir(referrer), filepath.FromSlash(specifier)))
		if ext := filepath.Ext(resolved); ext != ".js" && ext != ".mjs" {
			return "", "", fmt.Errorf("import must name a .js or .mjs file: %q", specifier)
		}
		if entry != nil && resolved == entry.Name {
			return entry.Code, resolved, nil
		}
		data, err := os.ReadFile(resolved)
		return string(data), resolved, err
	})
}

func (h *host) drain(ctx context.Context) error {
	for h.rt.Busy() {
		select {
		case <-h.rt.Wake():
			if err := h.rt.RunJobsContext(ctx); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

func (h *host) takeUnhandled() error {
	err := errors.Join(h.unhandled...)
	h.unhandled = nil
	return err
}

func (h *host) installConsole(opts Options) error {
	rt := h.rt
	if err := stdlib.Console(rt, opts.Stdout, opts.Stderr); err != nil {
		return err
	}
	inspect, err := rt.EvalFile("<noe-inspect>", `console[Symbol.for("quickjs.inspect")]`)
	if err != nil {
		return err
	}
	h.inspect = inspect
	full, err := rt.Get("console")
	if err != nil {
		return err
	}
	console := rt.NewObject()
	for _, name := range []string{"log", "warn", "error"} {
		method, err := full.Get(name)
		if err != nil {
			return err
		}
		if err := console.Set(name, method); err != nil {
			return err
		}
	}
	return rt.Set("console", console)
}

// Diagnostic includes a JavaScript stack where the engine supplies one.
func Diagnostic(err error) string {
	if jsErr, ok := err.(*quickjs.Error); ok {
		if stack := jsErr.Stack(); stack != "" {
			return stack
		}
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var parts []string
		for _, child := range joined.Unwrap() {
			parts = append(parts, Diagnostic(child))
		}
		return strings.Join(parts, "\n")
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if child := wrapped.Unwrap(); child != nil {
			return strings.Replace(err.Error(), child.Error(), Diagnostic(child), 1)
		}
	}
	return err.Error()
}
