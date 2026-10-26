package jsexec

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"

	quickjs "github.com/go-quickjs/go-quickjs"
)

// installImportMeta supplies metadata before the host initializer, which can
// replace the hook. Workers pass through this same Attach boundary.
func (b *Bindings) installImportMeta() {
	b.rt.OnImportMeta(func(id string, meta quickjs.Value) {
		value := moduleIDURL(id)
		if b.loader.virtualEntry != "" && id == b.loader.virtualEntry {
			value = fileModuleURL(virtualEntryPath(b.loader.cwd))
		}
		if err := meta.Set("url", value); err != nil {
			b.fail(fmt.Errorf("stdlib: import.meta.url for %q: %w", id, err))
		}
	})
}

// Keep virtual metadata and virtual-entry import resolution on the same base.
// This path is a synthetic module identifier, not a file that must exist.
func virtualEntryPath(cwd string) string {
	return filepath.Join(cwd, "<virtual-entry>.mjs")
}

func moduleIDURL(id string) string {
	// Slash-rooted paths also cover volume-free rooted filesystems on Windows.
	if filepath.IsAbs(id) || strings.HasPrefix(id, "/") {
		return fileModuleURL(id)
	}
	if parsed, err := url.Parse(id); err == nil && parsed.IsAbs() {
		return id
	}
	// Bare host-defined IDs are not filesystem paths. Give them an opaque URL
	// without suggesting that their relative imports use the host filesystem.
	return "noe:module/" + url.PathEscape(id)
}

func fileModuleURL(name string) string {
	slashed := filepath.ToSlash(name)
	u := url.URL{Scheme: "file"}
	if strings.HasPrefix(slashed, "//") {
		// A native Windows UNC path becomes file://server/share/path.
		u.Host, slashed, _ = strings.Cut(strings.TrimPrefix(slashed, "//"), "/")
		slashed = "/" + slashed
	} else if !strings.HasPrefix(slashed, "/") {
		// A native Windows drive path needs the URL's leading slash.
		slashed = "/" + slashed
	}
	u.Path = slashed
	return u.String()
}
