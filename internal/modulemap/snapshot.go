// Package modulemap resolves local and HTTP/HTTPS ECMAScript modules using
// immutable import-map snapshots with bare mappings and scopes.
package modulemap

import (
	"bytes"
	json "encoding/json/v2"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Snapshot is an immutable, absolute-target import map. Its entries are kept
// private so callers can safely share a snapshot between runtimes.
type Snapshot struct {
	virtual bool
	imports map[string]string
	scopes  map[string]map[string]string
}

// Load reads and validates an import map. Relative targets are resolved from
// the directory containing path.
func Load(path string) (*Snapshot, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("import map path %q: %w", path, err)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read import map %q: %w", path, err)
	}
	return Parse(data, filepath.Dir(abs))
}

// Parse validates an import-map JSON document. baseDir is the directory used
// to resolve relative targets; an empty baseDir means the current directory.
func Parse(data []byte, baseDir string) (*Snapshot, error) {
	return parse(data, baseDir, false)
}

// ParseVirtual resolves paths in a rooted execution namespace without host volumes.
func ParseVirtual(data []byte, baseDir string) (*Snapshot, error) { return parse(data, baseDir, true) }

func parse(data []byte, baseDir string, virtual bool) (*Snapshot, error) {
	root, err := decodeUniqueJSON(data)
	if err != nil {
		return nil, fmt.Errorf("invalid import map JSON: %w", err)
	}
	object, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("import map must be a JSON object")
	}

	fields := make([]string, 0, len(object))
	for name := range object {
		if name != "imports" && name != "scopes" {
			fields = append(fields, name)
		}
	}
	sort.Strings(fields)
	if len(fields) != 0 {
		return nil, fmt.Errorf("unsupported import map field %q", fields[0])
	}

	directory := absoluteDirectory
	if virtual {
		directory = func(value string) (string, error) { return virtualCanonical(value, "/") }
	}
	base, err := directory(baseDir)
	if err != nil {
		return nil, fmt.Errorf("import map base directory %q: %w", baseDir, err)
	}

	imports := make(map[string]string)
	if raw, exists := object["imports"]; exists {
		var err error
		imports, err = parseMappings(raw, base, virtual, "import map imports")
		if err != nil {
			return nil, err
		}
	}

	scopes := make(map[string]map[string]string)
	if raw, exists := object["scopes"]; exists {
		rawScopes, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("import map field %q must be an object", "scopes")
		}
		keys := make([]string, 0, len(rawScopes))
		for key := range rawScopes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			normalized, err := canonicalScope(key, base, virtual)
			if err != nil {
				return nil, fmt.Errorf("import map scope %q: %w", key, err)
			}
			if _, exists := scopes[normalized]; exists {
				return nil, fmt.Errorf("import map scopes contain duplicate normalized key %q", normalized)
			}
			mappings, err := parseMappings(rawScopes[key], base, virtual, "import map scope "+fmt.Sprintf("%q", key))
			if err != nil {
				return nil, err
			}
			scopes[normalized] = mappings
		}
	}
	return &Snapshot{imports: imports, scopes: scopes, virtual: virtual}, nil
}

func parseMappings(raw any, base string, virtual bool, context string) (map[string]string, error) {
	entries, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", context)
	}
	mappings := make(map[string]string, len(entries))
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value, ok := entries[key].(string)
		if !ok {
			return nil, fmt.Errorf("%s key %q target must be a string", context, key)
		}
		if err := validateImportKey(key); err != nil {
			return nil, fmt.Errorf("%s key %q: %w", context, key, err)
		}
		target, err := resolveMappingTarget(value, base, virtual, strings.HasSuffix(key, "/"))
		if err != nil {
			return nil, fmt.Errorf("%s key %q target %q: %w", context, key, value, err)
		}
		mappings[key] = target
	}
	return mappings, nil
}

// Resolve turns an import specifier into its canonical local path or HTTP/HTTPS
// module ID, using the most specific matching scope before top-level imports.
func (s *Snapshot) Resolve(specifier, referrer string) (string, error) {
	if specifier == "" {
		return "", fmt.Errorf("module specifier is empty")
	}
	if s != nil && !isReservedBuiltinSpecifier(specifier) {
		if len(s.scopes) > 0 {
			if referrerID, ok := canonicalReferrer(referrer, s.virtual); ok {
				for _, scope := range matchingScopes(s.scopes, referrerID, s.virtual) {
					if target, matched, err := resolveMapping(scope, specifier, s.virtual); matched {
						return target, err
					}
				}
			}
		}
		if target, matched, err := resolveMapping(s.imports, specifier, s.virtual); matched {
			return target, err
		}
	}

	if id, handled, err := resolveHTTPReference(specifier, referrer); handled {
		return id, err
	}
	if s != nil && s.virtual {
		return virtualResolve(specifier, referrer)
	}
	if isFileURL(specifier) {
		path, err := localFileURLPath(specifier)
		if err != nil {
			return "", fmt.Errorf("invalid local file URL %q: %w", specifier, err)
		}
		return canonicalPath(path, "")
	}
	if isDriveRelativePath(specifier) {
		return "", fmt.Errorf("drive-relative paths are unsupported: %q", specifier)
	}
	if isWindowsAbsolutePath(specifier) && !filepath.IsAbs(specifier) && !isWindowsHost() {
		return "", fmt.Errorf("absolute Windows paths are not native on this platform: %q", specifier)
	}
	if filepath.IsAbs(specifier) {
		return canonicalPath(specifier, "")
	}
	if isRelativeSpecifier(specifier) {
		base, err := referrerDirectory(referrer)
		if err != nil {
			return "", fmt.Errorf("resolve %q from %q: %w", specifier, referrer, err)
		}
		return canonicalPath(filepath.Join(base, filepath.FromSlash(specifier)), "")
	}
	if scheme, ok := urlScheme(specifier); ok {
		return "", fmt.Errorf("unsupported module URL scheme %q", scheme)
	}
	return "", fmt.Errorf("unmapped bare module specifier %q", specifier)
}

// MarshalJSON returns a transportable snapshot whose targets are absolute.
func (s *Snapshot) MarshalJSON() ([]byte, error) {
	imports := map[string]string{}
	scopes := map[string]map[string]string{}
	if s != nil {
		for key, value := range s.imports {
			imports[key] = value
		}
		for scope, mappings := range s.scopes {
			copy := make(map[string]string, len(mappings))
			for key, value := range mappings {
				copy[key] = value
			}
			scopes[scope] = copy
		}
	}
	return json.Marshal(struct {
		Imports map[string]string            `json:"imports"`
		Scopes  map[string]map[string]string `json:"scopes"`
	}{Imports: imports, Scopes: scopes})
}

func absoluteDirectory(path string) (string, error) {
	if path == "" {
		var err error
		path, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}
	return canonicalPath(path, "")
}

func validateImportKey(key string) error {
	if key == "" {
		return fmt.Errorf("key must not be empty")
	}
	if key == "." || key == ".." || key[0] == '/' || key[0] == '\\' ||
		isRelativeSpecifier(key) || isWindowsAbsolutePath(key) || isDriveRelativePath(key) || isFileURL(key) {
		return fmt.Errorf("path and URL remapping is unsupported; key must be a bare specifier")
	}
	if key == "node:" || hasPrefixFold(key, "node:") {
		return fmt.Errorf("node: module names are reserved")
	}
	if hasPrefixFold(key, "noe:") {
		return fmt.Errorf("noe: module names are reserved")
	}
	if scheme, ok := urlScheme(key); ok {
		return fmt.Errorf("URL-like keys are unsupported (scheme %q)", scheme)
	}
	if bytes.IndexByte([]byte(key), 0) >= 0 {
		return fmt.Errorf("key contains a NUL byte")
	}
	if _, reserved := standardLibraryBareModules[key]; reserved {
		return fmt.Errorf("standard-library module name is reserved")
	}
	return nil
}

func resolveTarget(target, base string) (string, error) {
	return resolveMappingTarget(target, base, false, false)
}

func resolveMappingTarget(target, base string, virtual, prefix bool) (string, error) {
	if target == "" {
		return "", fmt.Errorf("target must not be empty")
	}
	if prefix && !hasRequiredPrefixSeparator(target, virtual) {
		return "", fmt.Errorf("prefix target must end in '/' (or a native Windows separator)")
	}
	if virtual {
		return virtualMappingTarget(target, base)
	}
	if IsHTTPURL(target) {
		return CanonicalHTTPURL(target)
	}
	var path string
	if isFileURL(target) {
		var err error
		path, err = localFileURLPath(target)
		if err != nil {
			return "", err
		}
	} else {
		if isDriveRelativePath(target) {
			return "", fmt.Errorf("drive-relative paths are unsupported")
		}
		if isWindowsAbsolutePath(target) && !filepath.IsAbs(target) && !isWindowsHost() {
			return "", fmt.Errorf("absolute Windows paths are not native on this platform")
		}
		if filepath.IsAbs(target) {
			path = target
		} else if isRelativeSpecifier(target) {
			path = filepath.Join(base, filepath.FromSlash(target))
		} else if scheme, ok := urlScheme(target); ok {
			return "", fmt.Errorf("unsupported target URL scheme %q", scheme)
		} else {
			return "", fmt.Errorf("target must be a relative path, absolute path, local file URL, or HTTP/HTTPS URL")
		}
	}
	resolved, err := canonicalPath(path, "")
	if err != nil {
		return "", err
	}
	if hasTargetDirectorySeparator(target) {
		resolved = appendTargetSeparator(resolved, false)
	}
	return resolved, nil
}

// decodeUniqueJSON uses JSON v2's strict document parser, which rejects
// duplicate names at every object depth, invalid UTF-8, and trailing values.
func decodeUniqueJSON(data []byte) (any, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	return value, nil
}

// Imports returns an independent copy of the normalized mappings.
func (s *Snapshot) Imports() map[string]string {
	if s == nil {
		return nil
	}
	entries := make(map[string]string, len(s.imports))
	for key, value := range s.imports {
		entries[key] = value
	}
	return entries
}

// Scopes returns an independent copy of the normalized scoped mappings.
func (s *Snapshot) Scopes() map[string]map[string]string {
	if s == nil {
		return nil
	}
	scopes := make(map[string]map[string]string, len(s.scopes))
	for scope, mappings := range s.scopes {
		copy := make(map[string]string, len(mappings))
		for key, value := range mappings {
			copy[key] = value
		}
		scopes[scope] = copy
	}
	return scopes
}
