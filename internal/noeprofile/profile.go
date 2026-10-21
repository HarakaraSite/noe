package noeprofile

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	agentlog "forge.harakara.site/littleisland/noe/v2/agent/log"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

// ProfileOptions selects application-owned CLI defaults, independently of agent execution.
type ProfileOptions struct {
	Cwd, Source, Supervisor string
	Args                    []string
	Stdin                   io.Reader
	Stdout, Stderr          io.Writer
	ImportMap               *modulemap.Snapshot
	Auth                    auth.Options
}

func Environment() map[string]string {
	values := make(map[string]string)
	for _, pair := range os.Environ() {
		key, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		sensitive := false
		for _, hidden := range []string{"OPENROUTER_API_KEY", "OPENAI_API_KEY", "ANTHROPIC_API_KEY", "NOE_OPENROUTER_API_KEY", "NOE_AGENT_CONNECTION"} {
			if key == hidden || runtime.GOOS == "windows" && strings.EqualFold(key, hidden) {
				sensitive = true
				break
			}
		}
		if !sensitive {
			values[key] = value
		}
	}
	return values
}

// Profile assembles NOE's local CLI authority. Every execution receives a fresh
// loader and independent auth/log bindings; Worker runtimes omit these extras.
func Profile(options ProfileOptions) (stdlib.Profile, error) {
	cwd := options.Cwd
	var err error
	if cwd == "" {
		cwd, err = os.Getwd()
		if err != nil {
			return stdlib.Profile{}, err
		}
	}
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return stdlib.Profile{}, err
	}
	supervisor := options.Supervisor
	if supervisor == "" {
		supervisor, err = os.Executable()
		if err != nil {
			return stdlib.Profile{}, fmt.Errorf("locate NOE process helper: %w", err)
		}
	}
	profile := stdlib.Profile{
		Environment: stdlib.Environment{Cwd: cwd, Args: append([]string{os.Args[0], options.Source}, options.Args...), Env: Environment(), Stdin: options.Stdin, Stdout: options.Stdout, Stderr: options.Stderr},
		Modules: stdlib.Modules{
			ProcessInfo: true, Stdio: true, FS: &stdlib.FileSystemOptions{},
			Commands: &stdlib.CommandOptions{Supervisor: supervisor, Allow: func(string, []string) error { return nil }},
			DNS:      &stdlib.DNSOptions{}, Fetch: &stdlib.FetchOptions{}, Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }},
			WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }}, RawSockets: true, Signals: true,
			Workers: &stdlib.WorkerOptions{Max: -1}, OS: &stdlib.OSOptions{Real: true}, Random: rand.Reader, WindowsPaths: runtime.GOOS == "windows",
		},
	}
	profile.NewLoader = stdlib.FileLoader(options.ImportMap.Imports())
	profile.Initialize = func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
		if view.Worker() {
			return nil, nil
		}
		a, err := auth.Bind(ctx, view, options.Auth)
		if err != nil {
			return nil, err
		}
		l, err := agentlog.Bind(ctx, view)
		if err != nil {
			a.Close()
			return nil, err
		}
		return func() { l.Close(); a.Close() }, nil
	}
	return profile, nil
}
