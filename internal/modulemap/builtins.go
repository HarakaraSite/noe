package modulemap

// standardLibraryBareModules is the pinned go-quickjs v0.27.0 stdlib module
// registry used by NOE. Keep this table in one place and compare it against
// that version's public registered modules when updating the dependency.
// node:* names are reserved separately, including names not registered here.
var standardLibraryBareModules = map[string]struct{}{
	"assert":          {},
	"async_hooks":     {},
	"buffer":          {},
	"child_process":   {},
	"crypto":          {},
	"dns":             {},
	"dns/promises":    {},
	"events":          {},
	"fs":              {},
	"fs/promises":     {},
	"http":            {},
	"os":              {},
	"path":            {},
	"path/posix":      {},
	"path/win32":      {},
	"process":         {},
	"querystring":     {},
	"stream/web":      {},
	"string_decoder":  {},
	"timers":          {},
	"timers/promises": {},
	"url":             {},
	"util":            {},
	"vm":              {},
	"worker_threads":  {},
	"zlib":            {},
}
