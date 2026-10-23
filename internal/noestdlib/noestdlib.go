// Package noestdlib assembles the NOE-maintained standard library for a
// runtime without depending on the CLI or agent packages.
package noestdlib

import (
	"context"
	"errors"
	"io"
	"reflect"
	"sync"

	"forge.harakara.site/littleisland/noe/v2/internal/apidocs"
	"forge.harakara.site/littleisland/noe/v2/internal/commandenv"
	"forge.harakara.site/littleisland/noe/v2/internal/hostio"
	"forge.harakara.site/littleisland/noe/v2/internal/nativeio"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// IOConfig selects the native I/O modules as a group. Stdin is borrowed and
// is not inherited by worker runtimes. Cwd is used to resolve relative paths
// in the native file API.
type IOConfig struct {
	Stdin io.Reader
	Cwd   string
}

// NativeModules selects NOE-owned modules independently for a Profile.
type NativeModules struct {
	Stdio, Commands, FS, RawSockets, Signals bool
}

// ProfileConfig is the internal normalized installation request used by
// jsexec. It has no application defaults or grouped capability switch.
type ProfileConfig struct {
	Context       context.Context
	Core          stdlib.Config
	IO            IOConfig
	Native        NativeModules
	WorkerInstall stdlib.WorkerInstaller
}

// Bindings are the normalized output writers shared by the runtime's core,
// native I/O, and workers. The caller's writers remain borrowed.
type Bindings struct {
	Stdout *hostio.SharedWriter
	Stderr *hostio.SharedWriter
}

// ProfileBindings owns only adapters created by a Profile installation.
type ProfileBindings struct {
	Bindings
	native *nativeio.Host
	close  func()
}

// Close releases runtime-owned adapters, not the runtime, loop, writers, or
// other resources supplied by the caller.
func (b *ProfileBindings) Close() {
	if b != nil && b.close != nil {
		b.close()
	}
}

// InstallProfile installs explicit capabilities and the complete reference catalog.
func InstallProfile(rt *quickjs.Runtime, cfg ProfileConfig) (_ *ProfileBindings, err error) {
	if err := validate(cfg.Core); err != nil {
		return nil, err
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
	result := &ProfileBindings{Bindings: Bindings{Stdout: stdout, Stderr: stderr}}
	var closeOnce sync.Once
	closeAdapters := func() {
		closeOnce.Do(func() {
			if result.native != nil {
				result.native.Close()
			}
			for _, writer := range outputs.owned {
				_ = writer.Close()
			}
		})
	}
	result.close = closeAdapters
	rt.OnClose(closeAdapters)
	if err := stdlib.InstallProfile(rt, core, cfg.Native.Stdio, cfg.WorkerInstall); err != nil {
		closeAdapters()
		return nil, err
	}
	if err := installDocs(rt, cfg.Context); err != nil {
		closeAdapters()
		return nil, err
	}
	if cfg.Native.Stdio || cfg.Native.Commands || cfg.Native.FS || cfg.Native.RawSockets || cfg.Native.Signals {
		nativeOptions := nativeio.Options{Context: cfg.Context,
			Stdin: cfg.IO.Stdin, Stdout: stdout, Stderr: stderr, Cwd: cfg.IO.Cwd,
		}
		if core.FS != nil {
			nativeOptions.FileSystem = core.FS.Backend
		}
		if core.Run != nil {
			nativeOptions.Command = &commandenv.Config{
				Cwd: core.Run.Dir, Env: core.Run.Env, Allow: core.Run.Allow,
				Supervisor: core.Run.Supervisor, Timeout: core.Run.Timeout, ResolveDirectory: core.Run.ResolveDirectory,
			}
		}
		host, installErr := nativeio.InstallSelected(rt, nativeOptions, nativeio.Modules{
			Stdio: cfg.Native.Stdio, Commands: cfg.Native.Commands, FS: cfg.Native.FS,
			RawSockets: cfg.Native.RawSockets, Signals: cfg.Native.Signals,
		})
		if installErr != nil {
			closeAdapters()
			return nil, installErr
		}
		result.native = host
	}
	return result, nil
}

func validate(cfg stdlib.Config) error {
	if cfg.Workers != nil {
		if cfg.Loop == nil {
			return errors.New("stdlib: Workers needs a Loop")
		}
		if cfg.NoWebAPIs {
			return errors.New("stdlib: Workers needs the web APIs")
		}
	}
	return nil
}

func installDocs(rt *quickjs.Runtime, ctx context.Context) error {
	if ctx == nil {
		ctx = rt.Context()
	}
	return rt.SetModule("noe:docs", map[string]any{
		"describe": func(args ...quickjs.Value) (map[string]any, error) {
			if len(args) > 2 {
				return nil, rt.ThrowTypeError("describe expects at most a topic and member")
			}
			values := [2]string{}
			for i, arg := range args {
				if arg.IsUndefined() {
					continue
				}
				if !arg.IsString() {
					return nil, rt.ThrowTypeError("describe topic and member must be strings")
				}
				values[i] = arg.String()
			}
			return describe(ctx, rt, values[0], values[1])
		},
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
