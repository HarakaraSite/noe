// Package modulemap resolves local and HTTP/HTTPS ECMAScript modules using
// immutable import-map snapshots. It implements only exact bare mappings.
package modulemap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

// Snapshot is an immutable, absolute-target import map. Its entries are kept
// private so callers can safely share a snapshot between runtimes.
type Snapshot struct {
	virtual bool
	imports map[string]string
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
		if name != "imports" {
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
		entries, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("import map field %q must be an object", "imports")
		}
		keys := make([]string, 0, len(entries))
		for key := range entries {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			value, ok := entries[key].(string)
			if !ok {
				return nil, fmt.Errorf("import map key %q target must be a string", key)
			}
			if err := validateImportKey(key); err != nil {
				return nil, fmt.Errorf("import map key %q: %w", key, err)
			}
			resolve := resolveTarget
			if virtual {
				resolve = virtualTarget
			}
			target, err := resolve(value, base)
			if err != nil {
				return nil, fmt.Errorf("import map key %q target %q: %w", key, value, err)
			}
			imports[key] = target
		}
	}
	return &Snapshot{imports: imports, virtual: virtual}, nil
}

// Resolve turns an import specifier into its canonical local path or HTTP/HTTPS
// module ID. A bare specifier must have an exact entry in this snapshot.
func (s *Snapshot) Resolve(specifier, referrer string) (string, error) {
	if specifier == "" {
		return "", fmt.Errorf("module specifier is empty")
	}
	if s != nil {
		if target, ok := s.imports[specifier]; ok {
			return target, nil
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
	if s != nil {
		for key, value := range s.imports {
			imports[key] = value
		}
	}
	return json.Marshal(struct {
		Imports map[string]string `json:"imports"`
	}{Imports: imports})
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
	if key[len(key)-1] == '/' {
		return fmt.Errorf("prefix mappings are unsupported; key must be an exact bare specifier")
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
	if target == "" {
		return "", fmt.Errorf("target must not be empty")
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
	if err := validateModuleExtension(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func validateModuleExtension(path string) error {
	ext := filepath.Ext(path)
	if ext != ".js" && ext != ".mjs" {
		return fmt.Errorf("module path must end in .js or .mjs, got %q", ext)
	}
	return nil
}

// decodeUniqueJSON decodes JSON while rejecting duplicate keys at every object
// level. encoding/json's ordinary map decoding silently keeps the last value.
func decodeUniqueJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeJSONValue(decoder, "$")
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("trailing JSON value at byte %d", decoder.InputOffset())
		}
		return nil, fmt.Errorf("invalid trailing data at byte %d: %w", decoder.InputOffset(), err)
	}
	return value, nil
}

func decodeJSONValue(decoder *json.Decoder, location string) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("at %s byte %d: %w", location, decoder.InputOffset(), err)
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			object := make(map[string]any)
			for decoder.More() {
				keyToken, err := decoder.Token()
				if err != nil {
					return nil, fmt.Errorf("at %s byte %d: %w", location, decoder.InputOffset(), err)
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, fmt.Errorf("at %s byte %d: object key is not a string", location, decoder.InputOffset())
				}
				if _, duplicate := object[key]; duplicate {
					return nil, fmt.Errorf("duplicate JSON key %q at %s byte %d", key, location, decoder.InputOffset())
				}
				child, err := decodeJSONValue(decoder, location+"."+key)
				if err != nil {
					return nil, err
				}
				object[key] = child
			}
			if _, err := decoder.Token(); err != nil {
				return nil, fmt.Errorf("at %s byte %d: %w", location, decoder.InputOffset(), err)
			}
			return object, nil
		case '[':
			array := make([]any, 0)
			for i := 0; decoder.More(); i++ {
				child, err := decodeJSONValue(decoder, fmt.Sprintf("%s[%d]", location, i))
				if err != nil {
					return nil, err
				}
				array = append(array, child)
			}
			if _, err := decoder.Token(); err != nil {
				return nil, fmt.Errorf("at %s byte %d: %w", location, decoder.InputOffset(), err)
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q at %s byte %d", value, location, decoder.InputOffset())
		}
	default:
		return token, nil
	}
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
