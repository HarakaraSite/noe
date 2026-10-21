package modulemap

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Loader resolves and reads modules for one runtime. Successfully read sources
// are retained by canonical absolute ID until this Loader is discarded.
type Loader struct {
	snapshot *Snapshot
	read     func(string) ([]byte, error)
	entryID  string
	entry    string
	hasEntry bool
	entryErr error

	mu      sync.Mutex
	sources map[string]string
}

// NewLoader creates a runtime-local source cache. When entryName is non-empty,
// entryCode is the in-memory source for that canonical module ID.
func NewLoader(snapshot *Snapshot, entryName, entryCode string) *Loader {
	return NewReaderLoader(snapshot, entryName, entryCode, os.ReadFile)
}

// NewReaderLoader reads modules through the execution filesystem selected by its owner.
func NewReaderLoader(snapshot *Snapshot, entryName, entryCode string, read func(string) ([]byte, error)) *Loader {
	l := &Loader{snapshot: snapshot, read: read, sources: make(map[string]string)}
	if entryName != "" {
		l.hasEntry = true
		l.entry = entryCode
		if snapshot != nil && snapshot.virtual {
			l.entryID, l.entryErr = virtualCanonical(entryName, "/")
		} else if isFileURL(entryName) {
			path, err := localFileURLPath(entryName)
			if err != nil {
				l.entryErr = fmt.Errorf("entry name %q: %w", entryName, err)
			} else {
				l.entryID, l.entryErr = canonicalPath(path, "")
			}
		} else {
			l.entryID, l.entryErr = canonicalPath(entryName, "")
		}
		if l.entryErr == nil {
			l.sources[l.entryID] = entryCode
		}
	}
	return l
}

// Load has quickjs.ModuleLoader's signature and can be passed directly to
// Runtime.SetModuleLoader.
func (l *Loader) Load(specifier, referrer string) (source, resolved string, err error) {
	if l == nil {
		return "", "", fmt.Errorf("module loader is nil")
	}
	if l.entryErr != nil {
		return "", "", l.entryErr
	}
	resolved, err = l.snapshot.Resolve(specifier, referrer)
	if err != nil {
		return "", "", err
	}
	if err := validateModuleExtension(resolved); err != nil {
		return "", "", err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	if source, ok := l.sources[resolved]; ok {
		return source, resolved, nil
	}
	if l.read == nil {
		return "", "", fmt.Errorf("module filesystem is unavailable")
	}
	data, err := l.read(resolved)
	if err != nil {
		return "", "", fmt.Errorf("read module %q: %w", resolved, err)
	}
	source = string(data)
	l.sources[resolved] = source
	return source, resolved, nil
}

// EntryID returns the canonical entry ID, or an empty string when this loader
// has no in-memory entry source.
func (l *Loader) EntryID() (string, error) {
	if l == nil || !l.hasEntry {
		return "", nil
	}
	if l.entryErr != nil {
		return "", l.entryErr
	}
	if l.snapshot != nil && l.snapshot.virtual {
		return l.entryID, nil
	}
	return filepath.Clean(l.entryID), nil
}
