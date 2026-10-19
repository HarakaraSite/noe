// Package noestdlib assembles the NOE-maintained standard library for a
// runtime without depending on the CLI or agent packages.
package noestdlib

import (
	"errors"
	"io"
	"reflect"

	"noe/internal/apidocs"
	"noe/internal/hostio"
	"noe/internal/nativeio"
	"noe/internal/stdlib"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// IOConfig selects the native I/O modules as a group. Stdin is borrowed and
// is not inherited by worker runtimes. Cwd is used to resolve relative paths
// in the native file API.
type IOConfig struct {
	Stdin io.Reader
	Cwd   string
}

// Config selects the capabilities installed on a runtime.
type Config struct {
	Core stdlib.Config
	IO   *IOConfig
	Docs bool
}

// Bindings are the normalized output writers shared by the runtime's core,
// native I/O, and workers. The caller's writers remain borrowed.
type Bindings struct {
	Stdout *hostio.SharedWriter
	Stderr *hostio.SharedWriter
}

// Install installs the selected APIs on rt. It does not take ownership of rt,
// its loop, or any writer supplied in cfg.
func Install(rt *quickjs.Runtime, cfg Config) (Bindings, error) {
	if err := validate(cfg); err != nil {
		return Bindings{}, err
	}

	core := cloneConfig(cfg.Core)
	outputs := newOutputSet()
	stdout := outputs.normalize(core.Stdout)
	stderr := outputs.normalize(core.Stderr)
	core.Stdout, core.Stderr = stdout, stderr
	if core.Process != nil {
		if core.Process.Stdout == nil {
			core.Process.Stdout = stdout
		} else {
			core.Process.Stdout = outputs.normalize(core.Process.Stdout)
		}
		if core.Process.Stderr == nil {
			core.Process.Stderr = stderr
		} else {
			core.Process.Stderr = outputs.normalize(core.Process.Stderr)
		}
	}

	bindings := Bindings{Stdout: stdout, Stderr: stderr}
	closeOutputs := func() {
		for _, writer := range outputs.owned {
			_ = writer.Close()
		}
	}
	// This hook is deliberately registered before stdlib.Install: OnClose
	// runs in reverse order, so workers and native I/O finish before outputs.
	rt.OnClose(closeOutputs)

	var originalInstalled func(*quickjs.Runtime) error
	if core.Workers != nil {
		workers := *core.Workers
		originalInstalled = workers.Installed
		workers.Installed = func(workerRT *quickjs.Runtime) error {
			if err := installExtras(workerRT, cfg.IO, cfg.Docs, bindings, true); err != nil {
				return err
			}
			if originalInstalled != nil {
				return originalInstalled(workerRT)
			}
			return nil
		}
		core.Workers = &workers
	}

	if err := stdlib.Install(rt, core); err != nil {
		closeOutputs()
		return Bindings{}, err
	}
	if err := installExtras(rt, cfg.IO, cfg.Docs, bindings, false); err != nil {
		closeOutputs()
		return Bindings{}, err
	}
	return bindings, nil
}

func validate(cfg Config) error {
	if cfg.IO != nil {
		if cfg.Core.Process == nil {
			return errors.New("noestdlib: IO needs Core.Process")
		}
		if cfg.Core.NoWebAPIs {
			return errors.New("noestdlib: IO needs the web APIs")
		}
	}
	if cfg.Core.Workers != nil {
		if cfg.Core.Loop == nil {
			return errors.New("stdlib: Workers needs a Loop")
		}
		if cfg.Core.NoWebAPIs {
			return errors.New("stdlib: Workers needs the web APIs")
		}
	}
	return nil
}

func installExtras(rt *quickjs.Runtime, ioConfig *IOConfig, docs bool, bindings Bindings, worker bool) error {
	if docs {
		if err := installDocs(rt); err != nil {
			return err
		}
	}
	if ioConfig != nil {
		stdin := ioConfig.Stdin
		if worker {
			stdin = nil
		}
		_, err := nativeio.Install(rt, nativeio.Options{
			Stdin: stdin, Stdout: bindings.Stdout, Stderr: bindings.Stderr, Cwd: ioConfig.Cwd,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func installDocs(rt *quickjs.Runtime) error {
	return rt.SetModule("noe:docs", map[string]any{
		"docs": func(args ...quickjs.Value) (string, error) {
			if len(args) > 2 {
				return "", rt.ThrowTypeError("docs expects at most a topic and member")
			}
			values := [2]string{}
			for i, arg := range args {
				if arg.IsUndefined() {
					continue
				}
				if !arg.IsString() {
					return "", rt.ThrowTypeError("docs topic and member must be strings")
				}
				values[i] = arg.String()
			}
			text, err := apidocs.Lookup(values[0], values[1])
			if err != nil {
				exception := rt.NewError("Error", err.Error())
				if err := exception.Set("code", "ERR_DOC_NOT_FOUND"); err != nil {
					return "", err
				}
				return "", rt.Throw(exception)
			}
			return text, nil
		},
	})
}

// outputSet creates one owned facade for each comparable sink identity. A
// non-comparable writer is handled without using it as an interface map key.
type outputSet struct {
	entries []outputEntry
	owned   []*hostio.SharedWriter
}

type outputEntry struct {
	sink   io.Writer
	shared *hostio.SharedWriter
}

func newOutputSet() *outputSet { return &outputSet{} }

func (s *outputSet) normalize(sink io.Writer) *hostio.SharedWriter {
	for _, entry := range s.entries {
		if sameWriter(entry.sink, sink) {
			return entry.shared
		}
	}
	shared := hostio.NewSharedWriter(sink)
	s.entries = append(s.entries, outputEntry{sink: sink, shared: shared})
	s.owned = append(s.owned, shared)
	return shared
}

func sameWriter(a, b io.Writer) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	t := reflect.TypeOf(a)
	// A comparable struct type can still contain an interface holding a
	// non-comparable value. Check both actual values before comparing them.
	return t == reflect.TypeOf(b) && reflect.ValueOf(a).Comparable() && reflect.ValueOf(b).Comparable() && a == b
}

func cloneConfig(cfg stdlib.Config) stdlib.Config {
	originalServe := cfg.Serve
	if cfg.FS != nil {
		copy := *cfg.FS
		cfg.FS = &copy
	}
	if cfg.Process != nil {
		copy := *cfg.Process
		copy.Args = append([]string(nil), cfg.Process.Args...)
		copy.Env = cloneMap(cfg.Process.Env)
		cfg.Process = &copy
	}
	if cfg.OS != nil {
		copy := *cfg.OS
		cfg.OS = &copy
	}
	if cfg.Fetch != nil {
		copy := *cfg.Fetch
		cfg.Fetch = &copy
	}
	if cfg.DNS != nil {
		copy := *cfg.DNS
		copy.Servers = append([]string(nil), cfg.DNS.Servers...)
		cfg.DNS = &copy
	}
	if cfg.Run != nil {
		copy := *cfg.Run
		copy.Env = cloneMap(cfg.Run.Env)
		cfg.Run = &copy
	}
	if cfg.Serve != nil {
		copy := *cfg.Serve
		cfg.Serve = &copy
	}
	if cfg.Sockets != nil {
		socketServe := cfg.Sockets.Serve
		copy := *cfg.Sockets
		if socketServe != nil {
			switch {
			case socketServe == originalServe && cfg.Serve != nil:
				copy.Serve = cfg.Serve
			default:
				serveCopy := *socketServe
				copy.Serve = &serveCopy
			}
		}
		cfg.Sockets = &copy
	}
	if cfg.Workers != nil {
		copy := *cfg.Workers
		cfg.Workers = &copy
	}
	return cfg
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	copy := make(map[string]string, len(source))
	for key, value := range source {
		copy[key] = value
	}
	return copy
}
