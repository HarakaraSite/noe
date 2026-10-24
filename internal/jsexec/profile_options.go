package jsexec

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
)

// Environment is the one snapshot shared by the modules installed on an
// execution. Readers and writers are borrowed; Args and Env are copied.
type Environment struct {
	Cwd    string
	Args   []string
	Env    map[string]string
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

// Profile selects the environment and capabilities for one runtime.
type Profile struct {
	Environment Environment
	Modules     Modules
	NewLoader   LoaderFactory
	Initialize  RuntimeInitializer
}

// Modules selects capabilities independently. A nil option pointer disables
// that capability; option pointers are copied when the profile is attached.
type Modules struct {
	ProcessInfo  bool
	Stdio        bool
	FS           *FileSystemOptions
	Commands     *CommandOptions
	DNS          *DNSOptions
	Fetch        *FetchOptions
	Serve        *ServeOptions
	WebSockets   *WebSocketsOptions
	RawSockets   bool
	Signals      bool
	Workers      *WorkerOptions
	OS           *OSOptions
	Random       io.Reader
	WindowsPaths bool
}

type FileSystemOptions struct {
	Root     string
	ReadOnly bool
}

type CommandOptions struct {
	// Supervisor is an explicit process-helper executable; empty starts directly.
	Supervisor     string
	Allow          func(name string, args []string) error
	Timeout        time.Duration
	MaxOutputBytes int
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
	LookupAddr(context.Context, string) ([]string, error)
	LookupCNAME(context.Context, string) (string, error)
	LookupMX(context.Context, string) ([]*net.MX, error)
	LookupNS(context.Context, string) ([]*net.NS, error)
	LookupSRV(context.Context, string, string, string) (string, []*net.SRV, error)
	LookupTXT(context.Context, string) ([]string, error)
}

type DNSOptions struct {
	Resolver Resolver
	Allow    func(name string) error
	Servers  []string
	Timeout  time.Duration
}

type FetchOptions struct {
	Client       *http.Client
	Allow        func(*http.Request) error
	MaxBodyBytes int64
}

type ServeOptions struct {
	Allow        func(address string) error
	MaxBodyBytes int64
	ReadTimeout  time.Duration
	IdleTimeout  time.Duration
}

type WebSocketsOptions struct {
	Allow            func(target *url.URL) error
	MaxMessageBytes  int
	HandshakeTimeout time.Duration
	TLS              *tls.Config
}

type WorkerOptions struct {
	Max int
}

type OSOptions struct {
	Hostname string
	Homedir  string
	Tmpdir   string
	Real     bool
}

func normalize(profile Profile) (Profile, Environment, *filesystem.Backend, error) {
	profile = cloneProfile(profile)
	env := profile.Environment
	if env.Env == nil {
		env.Env = map[string]string{}
	}
	if env.Args == nil {
		env.Args = []string{}
	}
	if env.Stdin == nil {
		env.Stdin = strings.NewReader("")
	}
	if env.Stdout == nil {
		env.Stdout = io.Discard
	}
	if env.Stderr == nil {
		env.Stderr = io.Discard
	}
	root, readOnly := "", false
	if profile.Modules.FS != nil {
		root, readOnly = profile.Modules.FS.Root, profile.Modules.FS.ReadOnly
	}
	backend, err := filesystem.New(filesystem.Config{Root: root, Cwd: env.Cwd, ReadOnly: readOnly})
	if err != nil {
		return Profile{}, Environment{}, nil, fmt.Errorf("stdlib: configure execution cwd/filesystem: %w", err)
	}
	env.Cwd = backend.Cwd()
	if profile.Modules.FS != nil {
		// Carry the creation-time absolute root into worker profiles so later
		// attaches never reinterpret a relative Root against ambient cwd.
		profile.Modules.FS.Root = backend.Root()
	}
	if commands := profile.Modules.Commands; commands != nil && commands.Supervisor != "" {
		helper, helperErr := filepath.Abs(commands.Supervisor)
		if helperErr != nil {
			return Profile{}, Environment{}, nil, fmt.Errorf("stdlib: process helper: %w", helperErr)
		}
		commands.Supervisor = helper
	}
	// cloneProfile already gave this environment independent ownership. Keep
	// a separate binding copy, while retaining the owned profile copy for Workers.
	profile.Environment = env
	return profile, cloneEnvironment(env), backend, nil
}

func cloneProfile(profile Profile) Profile {
	profile.Environment = cloneEnvironment(profile.Environment)
	if profile.Modules.FS != nil {
		v := *profile.Modules.FS
		profile.Modules.FS = &v
	}
	if profile.Modules.Commands != nil {
		v := *profile.Modules.Commands
		profile.Modules.Commands = &v
	}
	if profile.Modules.DNS != nil {
		v := *profile.Modules.DNS
		v.Servers = append([]string(nil), v.Servers...)
		profile.Modules.DNS = &v
	}
	if profile.Modules.Fetch != nil {
		v := *profile.Modules.Fetch
		profile.Modules.Fetch = &v
	}
	if profile.Modules.Serve != nil {
		v := *profile.Modules.Serve
		profile.Modules.Serve = &v
	}
	if profile.Modules.WebSockets != nil {
		v := *profile.Modules.WebSockets
		if v.TLS != nil {
			v.TLS = v.TLS.Clone()
		}
		profile.Modules.WebSockets = &v
	}
	if profile.Modules.Workers != nil {
		v := *profile.Modules.Workers
		profile.Modules.Workers = &v
	}
	if profile.Modules.OS != nil {
		v := *profile.Modules.OS
		profile.Modules.OS = &v
	}
	return profile
}

func cloneEnvironment(env Environment) Environment {
	env.Args = cloneArgs(env.Args)
	env.Env = cloneMap(env.Env)
	return env
}

func cloneArgs(src []string) []string {
	dst := make([]string, len(src))
	copy(dst, src)
	return dst
}

func cloneMap(src map[string]string) map[string]string {
	if src == nil {
		return nil
	}
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// Snapshot freezes mutable profile settings and creation-time filesystem paths
// without creating a VM or invoking user initialization. Streams stay borrowed.
func Snapshot(profile Profile) (Profile, error) {
	input, output, diagnostics := profile.Environment.Stdin, profile.Environment.Stdout, profile.Environment.Stderr
	frozen, _, _, err := normalize(profile)
	if err != nil {
		return Profile{}, err
	}
	frozen.Environment.Stdin, frozen.Environment.Stdout, frozen.Environment.Stderr = input, output, diagnostics
	return frozen, nil
}
