package modulemap

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RemoteReader fetches a canonical HTTP/HTTPS ID and returns its final ID after
// redirects. retained may be called synchronously to avoid reading a response
// whose final ID already has source in this runtime. It must not be retained or
// called outside this callback.
type RemoteReader func(id string, retained func(string) bool) (source, finalID string, err error)

// Loader resolves and reads modules for one runtime. Successfully read sources
// are retained by canonical absolute ID until this Loader is discarded.
type Loader struct {
	snapshot *Snapshot
	read     func(string) ([]byte, error)
	remote   RemoteReader
	entryID  string
	hasEntry bool
	entryErr error

	mu      sync.Mutex
	sources map[string]string
	aliases map[string]string
}

// NewLoader creates a runtime-local source cache. When entryName is non-empty,
// entryCode is the in-memory source for that canonical module ID.
func NewLoader(snapshot *Snapshot, entryName, entryCode string) *Loader {
	return NewReaderLoader(snapshot, entryName, entryCode, os.ReadFile)
}

// NewReaderLoader reads modules through the execution filesystem selected by its owner.
func NewReaderLoader(snapshot *Snapshot, entryName, entryCode string, read func(string) ([]byte, error)) *Loader {
	return NewRemoteLoader(snapshot, entryName, entryCode, read, nil)
}

// NewRemoteLoader adds an explicit remote reader without granting local file
// access. A nil remote reader preserves NewReaderLoader's local-only behavior.
func NewRemoteLoader(snapshot *Snapshot, entryName, entryCode string, read func(string) ([]byte, error), remote RemoteReader) *Loader {
	l := &Loader{snapshot: snapshot, read: read, remote: remote, sources: make(map[string]string), aliases: make(map[string]string)}
	if entryName != "" {
		l.hasEntry = true
		if IsHTTPURL(entryName) {
			l.entryErr = fmt.Errorf("HTTP/HTTPS entry URLs are unsupported by the local module loader: %q", entryName)
		} else if snapshot != nil && snapshot.virtual {
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
	return l.load(specifier, referrer, false)
}

// LoadLocal resolves an entry before reading and rejects remote IDs, including
// URL map targets. Subsequent dependency loads may use Load with HTTP enabled.
func (l *Loader) LoadLocal(specifier, referrer string) (source, resolved string, err error) {
	return l.load(specifier, referrer, true)
}

func (l *Loader) load(specifier, referrer string, localOnly bool) (source, resolved string, err error) {
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
	remote := IsHTTPURL(resolved)
	if remote {
		if localOnly {
			return "", "", fmt.Errorf("HTTP/HTTPS Worker entry URLs are unsupported: %q", resolved)
		}
		if l.remote == nil {
			return "", "", fmt.Errorf("HTTP/HTTPS module loading is unsupported by the local module loader: %q", resolved)
		}
	} else if err := validateModuleExtension(resolved); err != nil {
		return "", "", err
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	requested := resolved
	if finalID, ok := l.aliases[resolved]; ok {
		resolved = finalID
	}
	if source, ok := l.sources[resolved]; ok {
		return source, resolved, nil
	}
	if remote {
		source, finalID, err := l.remote(requested, func(id string) bool {
			_, ok := l.sources[id]
			return ok
		})
		if err != nil {
			return "", "", fmt.Errorf("load module %q from %q: %w", specifier, referrer, err)
		}
		finalID, err = CanonicalHTTPURL(finalID)
		if err != nil {
			return "", "", err
		}
		if retained, ok := l.sources[finalID]; ok {
			source = retained
		} else {
			l.sources[finalID] = source
		}
		l.aliases[requested] = finalID
		return source, finalID, nil
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
