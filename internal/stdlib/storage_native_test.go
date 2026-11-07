package stdlib

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestStorageSerializerDoesNotCallPrototypeToJSON(t *testing.T) {
	root := t.TempDir()
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	scope, err := NewExecutionScope(loop, ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer scope.Close()
	if _, err := webAPIs(rt, nil, loop); err != nil {
		t.Fatal(err)
	}
	if err := installStorage(rt, Config{
		Loop: loop,
		FS:   &FS{Cwd: root},
		LocalStorage: &LocalStorageOptions{
			Path: "state.json",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`
		let nestedCalls = 0;
		Object.prototype.toJSON = function () {
		  nestedCalls++;
		  localStorage.getItem("from-toJSON");
		  return this;
		};
		Array.prototype.toJSON = function () {
		  nestedCalls++;
		  localStorage.getItem("from-array-toJSON");
		  return this;
		};
		Object.defineProperty(Object.prototype, "get", {
		  configurable: true,
		  get() {
		    nestedCalls++;
		    localStorage.getItem("from-inherited-descriptor");
		    return undefined;
		  },
		});
		localStorage.setItem("key\ud800", "value\udfff");
		if (nestedCalls !== 0) throw Error("storage internals called a user prototype hook");
		const restoredKey = localStorage.key(0);
		const restoredValue = localStorage.getItem("key\ud800");
		if (restoredKey.length !== 4 || restoredKey.charCodeAt(3) !== 0xd800 ||
		    restoredValue.length !== 6 || restoredValue.charCodeAt(5) !== 0xdfff) {
		  throw Error("storage did not preserve UTF-16 code units");
		}
		delete Object.prototype.get;
		delete Object.prototype.toJSON;
		delete Array.prototype.toJSON;
	`); err != nil {
		t.Fatalf("localStorage operation invoked a prototype callback: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Version int             `json:"version"`
		Entries json.RawMessage `json:"entries"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("stored document is not valid JSON: %v", err)
	}
	if document.Version != 1 || !json.Valid(document.Entries) {
		t.Fatalf("stored document has invalid versioned entries: %s", data)
	}
}

func TestStoragePathLocksCanonicalizeSymlinkAliasesAndHonorContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation privileges vary on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "state.json")
	alias := filepath.Join(dir, "alias.json")
	if err := os.WriteFile(target, []byte(emptyStorageDocument), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if got, want := storageLockKey(target), storageLockKey(alias); got != want {
		t.Fatalf("canonical storage lock keys differ: %q and %q", got, want)
	}
	release, err := acquireStoragePath(context.Background(), storageLockKey(target))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := acquireStoragePath(ctx, storageLockKey(alias))
		done <- err
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("contended lock error = %v, want context deadline", err)
		}
	case <-time.After(time.Second):
		t.Fatal("contended storage lock ignored context cancellation")
	}
}
