// Package runtime assembles NOE application services around managed stdlib execution.
package runtime

import (
	"context"
	"forge.harakara.site/littleisland/noe/v2/agent/auth"
	"forge.harakara.site/littleisland/noe/v2/internal/modulemap"
	"forge.harakara.site/littleisland/noe/v2/internal/noeprofile"
	"io"
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
	// InstructionDirs replaces default external sources when non-nil; empty disables them.
	// Relative paths are based on Cwd. The runtime copies and fixes locations at startup.
	InstructionDirs []string
	// Agent supplies NOE model, supervisor, control, and event settings. Nil uses lazy defaults.
	Agent *noeprofile.AgentOptions
	// Auth supplies HTTP/discovery settings for native sign-in verification.
	Auth *auth.Options

	// ImportMap is an immutable map of absolute file or HTTP/HTTPS targets.
	ImportMap *modulemap.Snapshot
}

// Execute creates a fresh runtime and runs source as an ES module.
func Execute(ctx context.Context, source Source, opts Options) (int, error) {
	return executeManaged(ctx, source, opts)
}
