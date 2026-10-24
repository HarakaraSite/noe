package jsexec

import (
	"crypto/tls"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	"forge.harakara.site/littleisland/noe/v2/internal/noestdlib"
	lower "forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func lowerConfig(profile Profile, env Environment, backend *filesystem.Backend, loop *lower.Loop) (lower.Config, noestdlib.NativeModules, error) {
	modules := profile.Modules
	cfg := lower.Config{
		Stdout: env.Stdout, Stderr: env.Stderr, Loop: loop,
		Random: modules.Random, WindowsPaths: modules.WindowsPaths,
	}
	lower.SetExecutionEnvironment(&cfg, env.Cwd, env.Args, env.Env)
	if modules.FS != nil {
		cfg.FS = &lower.FS{Cwd: env.Cwd, Backend: backend, Loop: loop}
	}
	if modules.ProcessInfo {
		cfg.Process = &lower.Process{
			Args: cloneArgs(env.Args), Env: cloneMap(env.Env), Cwd: env.Cwd,
			Stdin: env.Stdin, Stdout: env.Stdout, Stderr: env.Stderr,
		}
	}
	if modules.OS != nil {
		cfg.OS = &lower.OSInfo{Hostname: modules.OS.Hostname, Homedir: modules.OS.Homedir, Tmpdir: modules.OS.Tmpdir, Real: modules.OS.Real}
	}
	if modules.Fetch != nil {
		cfg.Fetch = &lower.Fetch{Loop: loop, Client: modules.Fetch.Client, Allow: modules.Fetch.Allow, MaxBodyBytes: modules.Fetch.MaxBodyBytes}
	}
	if modules.DNS != nil {
		cfg.DNS = &lower.DNS{Loop: loop, Resolver: modules.DNS.Resolver, Allow: modules.DNS.Allow, Servers: append([]string(nil), modules.DNS.Servers...), Timeout: modules.DNS.Timeout}
	}
	if modules.Commands != nil {
		cfg.Run = &lower.Run{Supervisor: modules.Commands.Supervisor, Loop: loop, Allow: modules.Commands.Allow, Dir: backend.PhysicalCwd(), Env: cloneMap(env.Env), Timeout: modules.Commands.Timeout, MaxOutputBytes: modules.Commands.MaxOutputBytes}
		cfg.Run.ResolveDirectory = func(dir string) (string, error) { return backend.Resolve(dir, true) }
	}
	var serve *lower.Serve
	if modules.Serve != nil {
		serve = &lower.Serve{Loop: loop, Allow: modules.Serve.Allow, MaxBodyBytes: modules.Serve.MaxBodyBytes, ReadTimeout: modules.Serve.ReadTimeout, IdleTimeout: modules.Serve.IdleTimeout}
		cfg.Serve = serve
	}
	if modules.WebSockets != nil {
		var socketServe *lower.Serve
		if serve != nil {
			socketServe = serve
		}
		var tlsConfig *tls.Config
		if modules.WebSockets.TLS != nil {
			tlsConfig = modules.WebSockets.TLS.Clone()
		}
		cfg.Sockets = &lower.WebSockets{Loop: loop, Allow: modules.WebSockets.Allow, Serve: socketServe, MaxMessageBytes: modules.WebSockets.MaxMessageBytes, HandshakeTimeout: modules.WebSockets.HandshakeTimeout, TLS: tlsConfig}
	}
	if modules.Workers != nil {
		cfg.Workers = &lower.Workers{Max: modules.Workers.Max}
	}
	selection := noestdlib.NativeModules{
		Stdio: modules.Stdio, Commands: modules.Commands != nil, FS: modules.FS != nil,
		RawSockets: modules.RawSockets, Signals: modules.Signals,
	}
	return cfg, selection, nil
}
