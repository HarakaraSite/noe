package runtime

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

// installStdlib gives scripts the full set of capabilities provided by the
// pinned go-quickjs standard library. NOE is a local command-line runtime, so
// these capabilities have the same authority as the user running it.
func (h *host) installStdlib(opts Options) error {
	if err := stdlib.Install(h.rt, h.stdlibConfig(opts)); err != nil {
		return err
	}
	inspect, err := h.rt.EvalFile("<noe-inspect>", `console[Symbol.for("quickjs.inspect")]`)
	if err != nil {
		return err
	}
	h.inspect = inspect
	return nil
}

func (h *host) stdlibConfig(opts Options) stdlib.Config {
	env := currentEnvironment()
	workers := &stdlib.Workers{
		New:  h.newWorkerRuntime,
		Load: h.loadWorker,
	}

	return stdlib.Config{
		Stdout: opts.Stdout,
		Stderr: opts.Stderr,
		Loop:   h.loop.std,
		FS:     &stdlib.FS{},
		Process: &stdlib.Process{
			Args:      append([]string{os.Args[0], opts.SourceName}, opts.Args...),
			Env:       env,
			Cwd:       h.cwd,
			Stdin:     opts.Stdin,
			Stdout:    opts.Stdout,
			Stderr:    opts.Stderr,
			Unhandled: h.recordUnhandled,
			Exit: func(code int) {
				h.exitRequested, h.exitCode = true, code
				_ = h.rt.Close()
			},
		},
		OS:      &stdlib.OSInfo{Real: true},
		Fetch:   &stdlib.Fetch{},
		DNS:     &stdlib.DNS{},
		Run:     &stdlib.Run{Dir: h.cwd, Env: env, Allow: func(string, []string) error { return nil }},
		Serve:   &stdlib.Serve{Allow: func(string) error { return nil }},
		Sockets: &stdlib.WebSockets{Allow: func(*url.URL) error { return nil }},
		Workers: workers,
	}
}

func currentEnvironment() map[string]string {
	env := make(map[string]string)
	for _, pair := range os.Environ() {
		name, value, ok := strings.Cut(pair, "=")
		if ok {
			env[name] = value
		}
	}
	return env
}

func (h *host) newWorkerRuntime() (*quickjs.Runtime, error) {
	rt := quickjs.New()
	rt.SetModuleLoader(func(specifier, referrer string) (string, string, error) {
		return loadLocalModule(specifier, referrer)
	})
	return rt, nil
}

func (h *host) loadWorker(specifier string) (source, name string, module bool, err error) {
	path, err := h.workerPath(specifier)
	if err != nil {
		return "", "", false, err
	}
	if ext := filepath.Ext(path); ext != ".js" && ext != ".mjs" {
		return "", "", false, fmt.Errorf("worker must name a .js or .mjs file: %q", specifier)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", false, err
	}
	// NOE treats both supported extensions as ES modules. The worker API's
	// explicit type option can still request classic-script execution.
	return string(data), path, true, nil
}

func (h *host) workerPath(specifier string) (string, error) {
	var path string
	if strings.HasPrefix(specifier, "file:") {
		u, err := url.Parse(specifier)
		if err != nil {
			return "", fmt.Errorf("invalid worker file URL %q: %w", specifier, err)
		}
		if u.Scheme != "file" || u.Opaque != "" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.Fragment != "" {
			return "", fmt.Errorf("worker URL must be a local file URL: %q", specifier)
		}
		path = u.Path
		if path == "" {
			return "", fmt.Errorf("worker URL has no file path: %q", specifier)
		}
		if len(path) >= 3 && path[0] == '/' && isDriveLetter(path[1]) && path[2] == ':' {
			path = path[1:]
		}
	} else if filepath.IsAbs(specifier) {
		path = specifier
	} else if u, err := url.Parse(specifier); err == nil && u.Scheme == "" {
		path = filepath.Join(h.cwd, filepath.FromSlash(strings.ReplaceAll(specifier, "\\", "/")))
	} else {
		return "", fmt.Errorf("worker must name a relative or absolute local file: %q", specifier)
	}

	path, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return path, nil
}

func isDriveLetter(c byte) bool {
	return c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func loadLocalModule(specifier, referrer string) (string, string, error) {
	if specifier == "noe:agent" {
		return "", "", fmt.Errorf("noe:agent is not supported in Workers")
	}
	if !strings.HasPrefix(specifier, "./") && !strings.HasPrefix(specifier, "../") {
		return "", "", fmt.Errorf("only relative local imports are supported: %q", specifier)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(referrer), filepath.FromSlash(specifier)))
	if ext := filepath.Ext(resolved); ext != ".js" && ext != ".mjs" {
		return "", "", fmt.Errorf("import must name a .js or .mjs file: %q", specifier)
	}
	data, err := os.ReadFile(resolved)
	return string(data), resolved, err
}
