package modulemap

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

func virtualCanonical(value, base string) (string, error) {
	if isFileURL(value) {
		u, err := url.Parse(value)
		if err != nil || u.Opaque != "" || (u.Host != "" && u.Host != "localhost") || u.RawQuery != "" || u.ForceQuery || strings.Contains(value, "#") || u.Fragment != "" {
			return "", fmt.Errorf("invalid virtual local file URL %q", value)
		}
		value = u.Path
		if !strings.HasPrefix(value, "/") {
			return "", fmt.Errorf("file URL path must be absolute")
		}
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if isDriveRelativePath(value) || isWindowsAbsolutePath(value) || (len(value) >= 3 && value[0] == '/' && isASCIILetter(value[1]) && value[2] == ':') {
		return "", fmt.Errorf("virtual path cannot have a volume: %q", value)
	}
	if scheme, ok := urlScheme(value); ok {
		return "", fmt.Errorf("unsupported module URL scheme %q", scheme)
	}
	if !strings.HasPrefix(value, "/") {
		value = path.Join(base, value)
	}
	return path.Clean("/" + strings.TrimPrefix(value, "/")), nil
}
func virtualTarget(value, base string) (string, error) {
	if IsHTTPURL(value) {
		return CanonicalHTTPURL(value)
	}
	normalized := strings.ReplaceAll(value, "\\", "/")
	if !strings.HasPrefix(normalized, "/") && !isFileURL(value) && !strings.HasPrefix(normalized, "./") && !strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("target must be a relative path, absolute path, local file URL, or HTTP/HTTPS URL")
	}
	resolved, err := virtualCanonical(value, base)
	if err != nil {
		return "", err
	}
	if err := validateModuleExtension(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}
func virtualResolve(specifier, referrer string) (string, error) {
	normalized := strings.ReplaceAll(specifier, "\\", "/")
	if strings.HasPrefix(normalized, "/") || isFileURL(specifier) {
		return virtualCanonical(specifier, "/")
	}
	if strings.HasPrefix(normalized, "./") || strings.HasPrefix(normalized, "../") {
		from, err := virtualCanonical(referrer, "/")
		if err != nil {
			return "", err
		}
		return virtualCanonical(specifier, path.Dir(from))
	}
	if scheme, ok := urlScheme(specifier); ok {
		return "", fmt.Errorf("unsupported module URL scheme %q", scheme)
	}
	return "", fmt.Errorf("unmapped bare module specifier %q", specifier)
}
