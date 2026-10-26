package jsexec

import (
	"context"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// ModuleLoaderProvider supplies a runtime-local loader and its URL capability.
// Providers are borrowed by Profile snapshots and must support concurrent
// NewLoader calls when a host creates multiple runtimes.
type ModuleLoaderProvider interface {
	NewLoader(context.Context, Origin) (quickjs.ModuleLoader, error)
	URLImports() URLImportSupport
}

// URLImportSupport describes configuration, not permission for every URL or
// compatibility with arbitrary packages.
type URLImportSupport string

const (
	URLImportsEnabled  URLImportSupport = "enabled"
	URLImportsDisabled URLImportSupport = "disabled"
	URLImportsUnknown  URLImportSupport = "unknown"
)

// URLImportCapability reads explicit provider metadata. Legacy factories are
// opaque; their identity is never used to infer a capability.
func URLImportCapability(profile Profile) URLImportSupport {
	if profile.NewLoader != nil {
		return URLImportsUnknown
	}
	if profile.Loader == nil {
		return URLImportsDisabled
	}
	switch support := profile.Loader.URLImports(); support {
	case URLImportsEnabled, URLImportsDisabled:
		return support
	default:
		return URLImportsUnknown
	}
}
