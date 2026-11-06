package modulemap

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func hasRequiredPrefixSeparator(target string, virtual bool) bool {
	if strings.HasSuffix(target, "/") {
		return true
	}
	if IsHTTPURL(target) || isFileURL(target) {
		return false
	}
	if virtual {
		return strings.HasSuffix(target, `\`)
	}
	return isWindowsHost() && (filepath.IsAbs(target) || isWindowsAbsolutePath(target) || isRelativeSpecifier(target)) && strings.HasSuffix(target, `\`)
}

func hasTargetDirectorySeparator(target string) bool {
	return strings.HasSuffix(target, "/") || isWindowsHost() && strings.HasSuffix(target, `\`)
}

func appendTargetSeparator(target string, virtual bool) string {
	separator := string(os.PathSeparator)
	if virtual {
		separator = "/"
	}
	if strings.HasSuffix(target, separator) {
		return target
	}
	return target + separator
}

func virtualMappingTarget(target, base string) (string, error) {
	if target == "" {
		return "", fmt.Errorf("target must not be empty")
	}
	if IsHTTPURL(target) {
		return CanonicalHTTPURL(target)
	}
	normalized := strings.ReplaceAll(target, `\`, "/")
	if !isFileURL(target) && !strings.HasPrefix(normalized, "/") &&
		!strings.HasPrefix(normalized, "./") && !strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("target must be a relative path, absolute path, local file URL, or HTTP/HTTPS URL")
	}
	resolved, err := virtualCanonical(target, base)
	if err != nil {
		return "", err
	}
	if strings.HasSuffix(normalized, "/") {
		resolved = appendTargetSeparator(resolved, true)
	}
	return resolved, nil
}

func canonicalScope(scope, base string, virtual bool) (string, error) {
	if scope == "" {
		return "", fmt.Errorf("scope key must not be empty")
	}
	if strings.IndexByte(scope, 0) >= 0 {
		return "", fmt.Errorf("scope key contains a NUL byte")
	}
	if IsHTTPURL(scope) {
		return CanonicalHTTPURL(scope)
	}
	if virtual {
		if scheme, ok := urlScheme(scope); ok && !isFileURL(scope) {
			return "", fmt.Errorf("unsupported scope URL scheme %q", scheme)
		}
		normalized := strings.ReplaceAll(scope, `\`, "/")
		resolved, err := virtualCanonical(scope, base)
		if err != nil {
			return "", err
		}
		if strings.HasSuffix(normalized, "/") {
			resolved = appendTargetSeparator(resolved, true)
		}
		return resolved, nil
	}
	if isFileURL(scope) {
		path, err := localFileURLPath(scope)
		if err != nil {
			return "", err
		}
		resolved, err := canonicalPath(path, "")
		if err != nil {
			return "", err
		}
		if hasTargetDirectorySeparator(path) {
			resolved = appendTargetSeparator(resolved, false)
		}
		return resolved, nil
	}
	if scheme, ok := urlScheme(scope); ok && !isWindowsAbsolutePath(scope) {
		return "", fmt.Errorf("unsupported scope URL scheme %q", scheme)
	}
	resolved, err := canonicalPath(scope, base)
	if err != nil {
		return "", err
	}
	if hasTargetDirectorySeparator(scope) {
		resolved = appendTargetSeparator(resolved, false)
	}
	return resolved, nil
}

func canonicalReferrer(referrer string, virtual bool) (string, bool) {
	if referrer == "" {
		return "", false
	}
	if IsHTTPURL(referrer) {
		id, err := CanonicalHTTPURL(referrer)
		return id, err == nil
	}
	if virtual {
		if _, ok := urlScheme(referrer); ok && !isFileURL(referrer) && !isWindowsAbsolutePath(referrer) {
			return "", false
		}
		id, err := virtualCanonical(referrer, "/")
		return id, err == nil
	}
	if isFileURL(referrer) {
		filePath, err := localFileURLPath(referrer)
		if err != nil {
			return "", false
		}
		id, err := canonicalPath(filePath, "")
		return id, err == nil
	}
	if isDriveRelativePath(referrer) {
		return "", false
	}
	if _, ok := urlScheme(referrer); ok && !isWindowsAbsolutePath(referrer) {
		return "", false
	}
	id, err := canonicalPath(referrer, "")
	return id, err == nil
}

func matchingScopes(scopes map[string]map[string]string, referrer string, virtual bool) []map[string]string {
	keys := make([]string, 0, len(scopes))
	for scope := range scopes {
		if scope == referrer || scopeHasDirectoryPrefix(scope, virtual) && strings.HasPrefix(referrer, scope) {
			keys = append(keys, scope)
		}
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	matches := make([]map[string]string, 0, len(keys))
	for _, key := range keys {
		matches = append(matches, scopes[key])
	}
	return matches
}

func scopeHasDirectoryPrefix(scope string, virtual bool) bool {
	if IsHTTPURL(scope) || virtual {
		return strings.HasSuffix(scope, "/")
	}
	return strings.HasSuffix(scope, string(os.PathSeparator))
}

func resolveMapping(mappings map[string]string, specifier string, virtual bool) (string, bool, error) {
	if target, ok := mappings[specifier]; ok {
		return target, true, nil
	}
	best := ""
	for key := range mappings {
		if strings.HasSuffix(key, "/") && len(key) > len(best) && strings.HasPrefix(specifier, key) {
			best = key
		}
	}
	if best == "" {
		return "", false, nil
	}
	target, err := resolvePrefixTarget(mappings[best], specifier[len(best):], virtual)
	return target, true, err
}

func resolvePrefixTarget(target, suffix string, virtual bool) (string, error) {
	if IsHTTPURL(target) {
		baseURL, err := url.Parse(target)
		if err != nil {
			return "", err
		}
		reference, err := url.Parse(suffix)
		if err != nil {
			return "", err
		}
		candidateURL := baseURL.ResolveReference(reference)
		candidate, err := CanonicalHTTPURL(candidateURL.String())
		if err != nil {
			return "", err
		}
		resolvedURL, _ := url.Parse(candidate)
		if !strings.HasPrefix(candidate, target) ||
			!urlPathWithinPrefix(baseURL.EscapedPath(), resolvedURL.EscapedPath()) {
			return "", fmt.Errorf("prefix target %q does not contain suffix %q", target, suffix)
		}
		return candidate, nil
	}
	if virtual {
		candidate, err := virtualCanonical(target+suffix, "/")
		if err != nil {
			return "", err
		}
		base := path.Clean(strings.TrimSuffix(target, "/"))
		if base == "." {
			base = "/"
		}
		if candidate != base && !strings.HasPrefix(candidate, strings.TrimSuffix(base, "/")+"/") {
			return "", fmt.Errorf("prefix target %q does not contain suffix %q", target, suffix)
		}
		return candidate, nil
	}
	pathSuffix := suffix
	if isWindowsHost() {
		pathSuffix = strings.ReplaceAll(pathSuffix, "/", string(os.PathSeparator))
	}
	resolved, err := canonicalPath(target+pathSuffix, "")
	if err != nil {
		return "", err
	}
	base := filepath.Clean(target)
	rel, err := filepath.Rel(base, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("prefix target %q does not contain suffix %q", target, suffix)
	}
	return resolved, nil
}

func urlPathWithinPrefix(base, candidate string) bool {
	baseParts := normalizedURLPathParts(base)
	candidateParts := normalizedURLPathParts(candidate)
	if len(baseParts) > 0 && baseParts[len(baseParts)-1] == "" {
		baseParts = baseParts[:len(baseParts)-1]
	}
	if len(candidateParts) < len(baseParts) {
		return false
	}
	for i, part := range baseParts {
		if candidateParts[i] != part {
			return false
		}
	}
	return true
}

func normalizedURLPathParts(escapedPath string) []string {
	parts := strings.Split(escapedPath, "/")
	stack := make([]string, 0, len(parts))
	for i, part := range parts {
		if i == 0 && part == "" {
			continue
		}
		switch escapedDotSegment(part) {
		case 1:
			if i == len(parts)-1 {
				stack = append(stack, "")
			}
		case 2:
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			if i == len(parts)-1 {
				stack = append(stack, "")
			}
		default:
			stack = append(stack, part)
		}
	}
	return stack
}

func escapedDotSegment(segment string) int {
	switch strings.ToLower(segment) {
	case ".", "%2e":
		return 1
	case "..", ".%2e", "%2e.", "%2e%2e":
		return 2
	default:
		return 0
	}
}

func isReservedBuiltinSpecifier(specifier string) bool {
	return hasPrefixFold(specifier, "node:") || hasPrefixFold(specifier, "noe:") || func() bool {
		_, reserved := standardLibraryBareModules[specifier]
		return reserved
	}()
}
