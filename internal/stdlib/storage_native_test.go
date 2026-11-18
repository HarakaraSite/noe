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
	"unicode/utf8"

	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestStorageSerializerDoesNotCallPrototypeToJSON(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "state.json")
	const legacy = `{"version":1,"entries":[["\u0032","\udfff\u0000\"\\"],["\u0031","\ud83d\ude03"]]}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
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
		String.prototype.toJSON = function () {
		  nestedCalls++;
		  localStorage.getItem("from-string-toJSON");
		  return this;
		};
		const stringify = JSON.stringify;
		JSON.stringify = function () {
		  nestedCalls++;
		  localStorage.getItem("from-replaced-stringify");
		};
		Object.defineProperty(Object.prototype, "get", {
		  configurable: true,
		  get() {
		    nestedCalls++;
		    localStorage.getItem("from-inherited-descriptor");
		    return undefined;
		  },
		});
		const legacyValue = "\udfff\u0000\"\\";
		const legacyEmoji = "\ud83d\ude03";
		if (localStorage.getItem("2") !== legacyValue || localStorage.getItem("1") !== legacyEmoji) {
		  throw Error("storage could not read the legacy v1 entries");
		}
		const key = "key\ud800\"\\";
		const value = "value\u0000\t\n\u00e9日本語\ud83d\ude00\udfff \ud800";
		localStorage.setItem(key, value);
		if (localStorage.getItem("2") !== legacyValue) throw Error("storage changed a legacy value while rewriting v1");
		localStorage.setItem("2", "updated");
		const restoredKey = localStorage.key(2);
		const restoredValue = localStorage.getItem(key);
		if (restoredKey !== key || restoredValue !== value || localStorage.getItem("1") !== legacyEmoji) {
		  throw Error("storage did not preserve UTF-16 code units");
		}
		if (localStorage.key(0) !== "2" || localStorage.key(1) !== "1" || localStorage.getItem("2") !== "updated") {
		  throw Error("storage changed entry order while updating a legacy v1 document");
		}
		if (nestedCalls !== 0) throw Error("storage internals called a user prototype hook");
		JSON.stringify = stringify;
		delete Object.prototype.get;
		delete Object.prototype.toJSON;
		delete Array.prototype.toJSON;
		delete String.prototype.toJSON;
	`); err != nil {
		t.Fatalf("localStorage serialization failed: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.Valid(data) {
		t.Fatalf("stored document is not valid UTF-8: %q", data)
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
