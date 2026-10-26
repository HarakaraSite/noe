package modulemap

import (
	"fmt"
	"net/url"
	"strings"
)

// IsHTTPURL reports whether value has an HTTP or HTTPS scheme. It recognizes
// malformed HTTP URLs too; CanonicalHTTPURL validates their structure.
func IsHTTPURL(value string) bool {
	scheme, ok := urlScheme(value)
	return ok && (strings.EqualFold(scheme, "http") || strings.EqualFold(scheme, "https"))
}

// CanonicalHTTPURL returns an absolute HTTP/HTTPS module ID. It uses net/url's
// hierarchical URI rules, preserving query order, path escapes and non-empty
// fragments. Only literal dot segments are removed, not percent-encoded dots.
func CanonicalHTTPURL(value string) (string, error) {
	u, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("invalid HTTP module URL %q: %w", value, err)
	}
	if !IsHTTPURL(value) || u.Opaque != "" || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("invalid HTTP module URL %q: expected an absolute hierarchical HTTP/HTTPS URL with a host", value)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	if port := u.Port(); u.Scheme == "http" && port == "80" || u.Scheme == "https" && port == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":"+port)
	}
	// Resolving an absolute reference normalizes its escaped path without
	// decoding escaped separators or treating escaped dots as dot segments.
	u = (&url.URL{}).ResolveReference(u)
	if u.Path == "" {
		u.Path = "/"
	}
	return u.String(), nil
}

// resolveHTTPReference handles URL references before local/virtual path rules.
// Bare names still require an exact map entry, and explicit file URLs remain
// local regardless of the referrer's origin.
func resolveHTTPReference(specifier, referrer string) (id string, handled bool, err error) {
	if IsHTTPURL(specifier) {
		id, err = CanonicalHTTPURL(specifier)
		return id, true, err
	}
	if !IsHTTPURL(referrer) || !(strings.HasPrefix(specifier, "./") ||
		strings.HasPrefix(specifier, "../") || strings.HasPrefix(specifier, "/") ||
		strings.HasPrefix(specifier, "?") || strings.HasPrefix(specifier, "#")) {
		return "", false, nil
	}
	baseID, err := CanonicalHTTPURL(referrer)
	if err != nil {
		return "", true, fmt.Errorf("resolve %q from %q: %w", specifier, referrer, err)
	}
	base, _ := url.Parse(baseID) // CanonicalHTTPURL has validated this URL.
	if strings.HasPrefix(specifier, "//") {
		id, err = CanonicalHTTPURL(base.Scheme + ":" + specifier)
		return id, true, err
	}
	ref, err := url.Parse(specifier)
	if err != nil {
		return "", true, fmt.Errorf("resolve %q from %q: %w", specifier, referrer, err)
	}
	resolved := base.ResolveReference(ref)
	// net/url cannot distinguish an absent fragment from an explicit empty
	// fragment. An explicit # must clear the referrer's fragment.
	if strings.Contains(specifier, "#") {
		resolved.Fragment = ref.Fragment
		resolved.RawFragment = ref.RawFragment
	}
	// Preserve an explicitly empty query when the reference inherits it.
	if ref.Path == "" && ref.Host == "" && ref.User == nil && !ref.ForceQuery && ref.RawQuery == "" {
		resolved.ForceQuery = base.ForceQuery
	}
	id, err = CanonicalHTTPURL(resolved.String())
	return id, true, err
}
