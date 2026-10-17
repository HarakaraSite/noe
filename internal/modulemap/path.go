package modulemap

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func canonicalPath(path, base string) (string, error) {
	if isDriveRelativePath(path) {
		return "", fmt.Errorf("drive-relative paths are unsupported: %q", path)
	}
	if isWindowsAbsolutePath(path) && !filepath.IsAbs(path) && !isWindowsHost() {
		return "", fmt.Errorf("Windows absolute paths are not native on this platform: %q", path)
	}
	path = filepath.FromSlash(path)
	if !filepath.IsAbs(path) {
		if base == "" {
			var err error
			base, err = os.Getwd()
			if err != nil {
				return "", err
			}
		}
		base = filepath.FromSlash(base)
		path = filepath.Join(base, path)
	}
	abs, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func referrerDirectory(referrer string) (string, error) {
	if referrer == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		return filepath.Abs(cwd)
	}
	if isFileURL(referrer) {
		path, err := localFileURLPath(referrer)
		if err != nil {
			return "", err
		}
		id, err := canonicalPath(path, "")
		if err != nil {
			return "", err
		}
		return filepath.Dir(id), nil
	}
	// A Windows drive path has a colon but is still a native filesystem path.
	if filepath.IsAbs(referrer) || isWindowsHost() && isWindowsAbsolutePath(referrer) {
		id, err := canonicalPath(referrer, "")
		if err != nil {
			return "", err
		}
		return filepath.Dir(id), nil
	}
	if scheme, ok := urlScheme(referrer); ok {
		return "", fmt.Errorf("referrer is not a local file path (scheme %q)", scheme)
	}
	id, err := canonicalPath(referrer, "")
	if err != nil {
		return "", err
	}
	return filepath.Dir(id), nil
}

func localFileURLPath(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if !strings.EqualFold(u.Scheme, "file") || u.Opaque != "" {
		return "", fmt.Errorf("expected hierarchical file URL")
	}
	if u.Host != "" && !strings.EqualFold(u.Host, "localhost") {
		return "", fmt.Errorf("non-local authority %q is unsupported", u.Host)
	}
	if strings.Contains(raw, "?") || strings.Contains(raw, "#") || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" || u.RawFragment != "" {
		return "", fmt.Errorf("query and fragment components are unsupported")
	}
	path := u.Path // net/url has already decoded percent-escapes.
	if path == "" {
		return "", fmt.Errorf("file URL has no path")
	}
	if isWindowsHost() && len(path) >= 3 && path[0] == '/' && isASCIILetter(path[1]) && path[2] == ':' {
		path = path[1:]
	}
	if !filepath.IsAbs(filepath.FromSlash(path)) && !(isWindowsHost() && isWindowsAbsolutePath(path)) {
		return "", fmt.Errorf("file URL path must be absolute")
	}
	return filepath.FromSlash(path), nil
}

func isFileURL(value string) bool {
	return len(value) >= len("file:") && strings.EqualFold(value[:len("file:")], "file:")
}

func isRelativeSpecifier(value string) bool {
	if strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		return true
	}
	separator := string(os.PathSeparator)
	return isWindowsHost() && (strings.HasPrefix(value, "."+separator) || strings.HasPrefix(value, ".."+separator))
}

func isWindowsAbsolutePath(value string) bool {
	if len(value) >= 3 && isASCIILetter(value[0]) && value[1] == ':' && isPathSeparator(value[2]) {
		return true
	}
	return strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, `//`)
}

func isDriveRelativePath(value string) bool {
	return len(value) >= 2 && isASCIILetter(value[0]) && value[1] == ':' &&
		(len(value) == 2 || !isPathSeparator(value[2]))
}

func isPathSeparator(value byte) bool { return value == '/' || value == '\\' }

func isASCIILetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isWindowsHost() bool { return runtime.GOOS == "windows" }

func urlScheme(value string) (string, bool) {
	colon := strings.IndexByte(value, ':')
	if colon <= 0 {
		return "", false
	}
	first := value[0]
	if !isASCIILetter(first) {
		return "", false
	}
	for i := 1; i < colon; i++ {
		c := value[i]
		if !isASCIILetter(c) && !(c >= '0' && c <= '9') && c != '+' && c != '-' && c != '.' {
			return "", false
		}
	}
	return value[:colon], true
}

func hasPrefixFold(value, prefix string) bool {
	return len(value) >= len(prefix) && strings.EqualFold(value[:len(prefix)], prefix)
}
