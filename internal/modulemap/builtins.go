package modulemap

// standardLibraryBareModules is the NOE-maintained stdlib module registry,
// based on go-quickjs v0.27.0. Keep this table in one place and compare it
// against internal/stdlib's public registered modules when changing the library.
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
