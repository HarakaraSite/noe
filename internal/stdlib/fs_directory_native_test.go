package stdlib

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func directoryHostForTest(t *testing.T, scoped bool) (*quickjs.Runtime, *Loop, *ExecutionScope, *fileHost) {
	t.Helper()
	rt := quickjs.New()
	loop := NewLoop(rt)
	rt.OnClose(loop.Close)
	var scope *ExecutionScope
	if scoped {
		var err error
		scope, err = NewExecutionScope(loop, context.Background())
		if err != nil {
			t.Fatal(err)
		}
	}
	backend, err := filesystem.New(filesystem.Config{Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	fs := &fsHost{rt: rt, cfg: &FS{Loop: loop}, backend: backend}
	host, err := newFileHost(fs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if scope != nil {
			scope.Close()
		}
		loop.Close()
		_ = rt.Close()
	})
	return rt, loop, scope, host
}

func TestDirectoryNativeBatchBoundAndEOF(t *testing.T) {
	_, _, _, host := directoryHostForTest(t, false)
	root := host.fs.backend.Cwd()
	for i := range 29 {
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("entry-%02d", i)), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cursor := &directoryCursor{host: host, path: ".", open: func(path string) (directoryBackend, error) { return os.Open(path) }}
	seen := map[string]bool{}
	for {
		batch, err := cursor.readBatch(7)
		if err != nil {
			t.Fatal(err)
		}
		if len(batch.entries.entries) > 7 {
			t.Fatalf("batch contains %d entries", len(batch.entries.entries))
		}
		for _, entry := range batch.entries.entries {
			name := entry["name"].(string)
			if seen[name] {
				t.Fatalf("duplicate entry %q", name)
			}
			seen[name] = true
		}
		if batch.done {
			break
		}
	}
	if len(seen) != 29 {
		t.Fatalf("entries=%d", len(seen))
	}
	host.mu.Lock()
	owned := len(host.resources)
	host.mu.Unlock()
	if owned != 0 {
		t.Fatalf("EOF retained %d descriptors", owned)
	}
	if err := cursor.close(); err != nil {
		t.Fatal(err)
	}
}

type controlledDirectory struct {
	directoryBackend
	started chan struct{}
	gate    chan struct{}
	once    sync.Once
	closed  atomic.Int32
	err     error
}

func (d *controlledDirectory) ReadDir(n int) ([]os.DirEntry, error) {
	if d.started != nil {
		close(d.started)
		<-d.gate
	}
	if d.err != nil {
		return nil, d.err
	}
	return d.directoryBackend.ReadDir(n)
}

func (d *controlledDirectory) Close() error {
	d.closed.Add(1)
	if d.gate != nil {
		d.once.Do(func() { close(d.gate) })
	}
	return d.directoryBackend.Close()
}

func TestDirectoryNativeOwnerCleanupAndReadError(t *testing.T) {
	for _, owner := range []string{"runtime", "loop", "execution", "read error"} {
		t.Run(owner, func(t *testing.T) {
			rt, loop, scope, host := directoryHostForTest(t, owner == "execution")
			if err := os.WriteFile(filepath.Join(host.fs.backend.Cwd(), "one"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(host.fs.backend.Cwd())
			if err != nil {
				t.Fatal(err)
			}
			backend := &controlledDirectory{directoryBackend: file}
			failure := errors.New("directory read failed")
			if owner == "read error" {
				backend.err = failure
			}
			cursor := &directoryCursor{host: host, path: ".", open: func(string) (directoryBackend, error) { return backend, nil }}
			_, err = cursor.readBatch(1)
			if owner == "read error" {
				if !errors.Is(err, failure) {
					t.Fatalf("read error=%v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			switch owner {
			case "runtime":
				if err := rt.Close(); err != nil {
					t.Fatal(err)
				}
			case "loop":
				loop.Close()
			case "execution":
				scope.Close()
			}
			if owner != "read error" {
				select {
				case <-host.closeDone:
				case <-time.After(2 * time.Second):
					t.Fatal("owner did not close descriptor host")
				}
			}
			if backend.closed.Load() != 1 {
				t.Fatalf("descriptor closed %d times", backend.closed.Load())
			}
			host.mu.Lock()
			owned := len(host.resources)
			host.mu.Unlock()
			if owned != 0 {
				t.Fatalf("owner retained %d descriptors", owned)
			}
		})
	}
}

func TestDirectoryNativeCloseInterruptsReadAndRejectsLateOpen(t *testing.T) {
	t.Run("reading", func(t *testing.T) {
		_, _, _, host := directoryHostForTest(t, false)
		file, err := os.Open(host.fs.backend.Cwd())
		if err != nil {
			t.Fatal(err)
		}
		backend := &controlledDirectory{directoryBackend: file, started: make(chan struct{}), gate: make(chan struct{})}
		cursor := &directoryCursor{host: host, path: ".", open: func(string) (directoryBackend, error) { return backend, nil }}
		finished := make(chan error, 1)
		go func() { _, err := cursor.readBatch(1); finished <- err }()
		<-backend.started
		if err := cursor.close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("close did not interrupt directory read")
		}
		if backend.closed.Load() != 1 {
			t.Fatalf("closed %d times", backend.closed.Load())
		}
	})
	t.Run("opening", func(t *testing.T) {
		_, _, _, host := directoryHostForTest(t, false)
		file, err := os.Open(host.fs.backend.Cwd())
		if err != nil {
			t.Fatal(err)
		}
		backend := &controlledDirectory{directoryBackend: file}
		started, proceed := make(chan struct{}), make(chan struct{})
		cursor := &directoryCursor{host: host, path: ".", open: func(string) (directoryBackend, error) {
			close(started)
			<-proceed
			return backend, nil
		}}
		finished := make(chan error, 1)
		go func() { _, err := cursor.readBatch(1); finished <- err }()
		<-started
		if err := cursor.close(); err != nil {
			t.Fatal(err)
		}
		close(proceed)
		if err := <-finished; !errors.Is(err, os.ErrClosed) {
			t.Fatalf("late open=%v", err)
		}
		if backend.closed.Load() != 1 {
			t.Fatalf("late descriptor closed %d times", backend.closed.Load())
		}
	})
}
