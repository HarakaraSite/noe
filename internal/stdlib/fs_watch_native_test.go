package stdlib

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func newNativeFSWatchTest(t *testing.T, cfg filesystem.Config, withLoop bool) (*quickjs.Runtime, *Loop, *fsHost, *fsWatchHost) {
	t.Helper()
	rt := quickjs.New()
	var loop *Loop
	if withLoop {
		loop = NewLoop(rt)
		rt.OnClose(loop.Close)
	}
	t.Cleanup(func() {
		if loop != nil {
			loop.Close()
		}
		_ = rt.Close()
	})
	backend, err := filesystem.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fsCfg := &FS{Root: cfg.Root, Cwd: cfg.Cwd, ReadOnly: cfg.ReadOnly, Loop: loop}
	f := &fsHost{rt: rt, cfg: fsCfg, backend: backend}
	host, err := newFSWatchHost(f)
	if err != nil {
		t.Fatal(err)
	}
	return rt, loop, f, host
}

func openNativeFSWatch(t *testing.T, host *fsWatchHost, path string, persistent bool, maxQueue int, ignoreOverflow, bytes bool) quickjs.Value {
	t.Helper()
	object, err := host.open(path, persistent, maxQueue, ignoreOverflow, bytes)
	if err != nil {
		t.Fatal(err)
	}
	return object
}

func fsWatchCall(t *testing.T, object quickjs.Value, method string) quickjs.Value {
	t.Helper()
	fn, err := object.Get(method)
	if err != nil {
		t.Fatal(err)
	}
	value, err := fn.Call()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fsWatchNext(t *testing.T, object quickjs.Value) quickjs.Value {
	t.Helper()
	return fsWatchCall(t, object, "next")
}

func awaitFSWatch(t *testing.T, promise quickjs.Value) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	value, err := promise.Await(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fsWatchResourceForTest(t *testing.T, host *fsWatchHost) *fsWatch {
	t.Helper()
	host.mu.Lock()
	defer host.mu.Unlock()
	for resource := range host.resources {
		return resource
	}
	t.Fatal("watch host has no live resource")
	return nil
}

func TestFSWatchNativeDirectoryAndFileTargetDelivery(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: dir}, true)

	directory := openNativeFSWatch(t, host, ".", true, 8, false, false)
	pendingCreate := fsWatchNext(t, directory)
	created := filepath.Join(dir, "created")
	if err := os.WriteFile(created, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	createdResult := awaitFSWatch(t, pendingCreate)
	done, err := createdResult.Get("done")
	if err != nil || done.Bool() {
		t.Fatal("directory watch ended before a create event")
	}
	createdEvent, err := createdResult.Get("value")
	if err != nil {
		t.Fatal(err)
	}
	typeValue, err := createdEvent.Get("eventType")
	if err != nil || (typeValue.String() != "rename" && typeValue.String() != "change") {
		t.Fatalf("create event type = %q, error = %v", typeValue.String(), err)
	}
	filename, err := createdEvent.Get("filename")
	if err != nil || filename.String() != "created" {
		t.Fatalf("create filename = %q, error = %v", filename.String(), err)
	}
	if strings.Contains(filename.String(), dir) {
		t.Fatalf("watch leaked physical directory in filename %q", filename.String())
	}
	fsWatchCall(t, directory, "close")

	file := openNativeFSWatch(t, host, "target", true, 8, false, false)
	pendingWrite := fsWatchNext(t, file)
	if err := os.WriteFile(target, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeResult := awaitFSWatch(t, pendingWrite)
	writeEvent, err := writeResult.Get("value")
	if err != nil {
		t.Fatal(err)
	}
	typeValue, err = writeEvent.Get("eventType")
	if err != nil || typeValue.String() != "change" {
		t.Fatalf("file write event type = %q, error = %v", typeValue.String(), err)
	}
	filename, err = writeEvent.Get("filename")
	if err != nil || filename.String() != "target" {
		t.Fatalf("file target filename = %q, error = %v", filename.String(), err)
	}
	fsWatchCall(t, file, "close")
	if rt.Busy() {
		t.Fatal("explicit close left native watch work referenced")
	}
}

func TestFSWatchNativePendingNextCloseAndLiveness(t *testing.T) {
	rt, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: t.TempDir()}, true)
	directory := openNativeFSWatch(t, host, ".", false, 8, false, false)
	resource := fsWatchResourceForTest(t, host)
	if rt.Busy() {
		t.Fatal("unreferenced watcher kept the runtime busy")
	}
	fsWatchCall(t, directory, "ref")
	fsWatchCall(t, directory, "ref")
	if !rt.Busy() {
		t.Fatal("ref did not keep the runtime busy")
	}
	fsWatchCall(t, directory, "unref")
	fsWatchCall(t, directory, "unref")
	if rt.Busy() {
		t.Fatal("unref left the runtime busy")
	}

	first := fsWatchNext(t, directory)
	second := fsWatchNext(t, directory)
	closeFn, err := directory.Get("close")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closeFn.Call(); err != nil {
		t.Fatal(err)
	}
	if _, err := closeFn.Call(); err != nil {
		t.Fatal(err)
	}
	for _, pending := range []quickjs.Value{first, second} {
		result := awaitFSWatch(t, pending)
		done, err := result.Get("done")
		if err != nil || !done.Bool() {
			t.Fatalf("pending next result done = %v, error = %v", done.Bool(), err)
		}
	}
	resource.mu.Lock()
	closed := resource.closed
	resource.mu.Unlock()
	if !closed || rt.Busy() {
		t.Fatalf("close state closed=%v busy=%v", closed, rt.Busy())
	}
	afterClose := awaitFSWatch(t, fsWatchNext(t, directory))
	done, err := afterClose.Get("done")
	if err != nil || !done.Bool() {
		t.Fatalf("next after close done = %v, error = %v", done.Bool(), err)
	}
}

func TestFSWatchNativePendingNextFIFO(t *testing.T) {
	rt, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: t.TempDir()}, true)
	object := openNativeFSWatch(t, host, ".", true, 2, false, false)
	resource := fsWatchResourceForTest(t, host)
	first := resource.next().Value()
	if err := rt.Set("firstWatchRead", first); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`globalThis.watchReadOrder=[];firstWatchRead.then(()=>watchReadOrder.push(1));`); err != nil {
		t.Fatal(err)
	}
	if !resource.enqueue(fsWatchEvent{typeName: "rename", filename: []byte("first")}) {
		t.Fatal("first event was rejected")
	}
	if !resource.enqueue(fsWatchEvent{typeName: "change", filename: []byte("second")}) {
		t.Fatal("second event was rejected")
	}
	// Call the native method directly so no owner job is drained between the
	// queued events and the second request. Observe settlement, not only values.
	second := resource.next().Value()
	if err := rt.Set("secondWatchRead", second); err != nil {
		t.Fatal(err)
	}
	if _, err := rt.Eval(`secondWatchRead.then(()=>watchReadOrder.push(2));`); err != nil {
		t.Fatal(err)
	}
	for i, pending := range []quickjs.Value{first, second} {
		result := awaitFSWatch(t, pending)
		event, err := result.Get("value")
		if err != nil {
			t.Fatal(err)
		}
		filename, err := event.Get("filename")
		want := []string{"first", "second"}[i]
		if err != nil || filename.String() != want {
			t.Fatalf("next %d filename = %q, error = %v; want %q", i, filename.String(), err, want)
		}
	}
	order, err := rt.Eval(`JSON.stringify(watchReadOrder)`)
	if err != nil || order.String() != "[1,2]" {
		t.Fatalf("next settlement order=%q error=%v", order.String(), err)
	}
	fsWatchCall(t, object, "close")
}

func TestFSWatchNativeRootCwdReadOnlyAndErrors(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sub, "target")
	if err := os.WriteFile(target, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(sub, "escape")); err != nil {
			t.Fatal(err)
		}
	}
	_, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Root: root, Cwd: "/sub", ReadOnly: true}, true)
	watch := openNativeFSWatch(t, host, "target", true, 8, false, false)
	pending := fsWatchNext(t, watch)
	if err := os.WriteFile(target, []byte("after"), 0o600); err != nil {
		t.Fatal(err)
	}
	result := awaitFSWatch(t, pending)
	event, err := result.Get("value")
	if err != nil {
		t.Fatal(err)
	}
	filename, err := event.Get("filename")
	if err != nil || filename.String() != "target" {
		t.Fatalf("rooted watch filename = %q, error = %v", filename.String(), err)
	}
	fsWatchCall(t, watch, "close")

	missing, err := host.open("missing", true, 8, false, false)
	if err == nil || missing.IsObject() {
		t.Fatal("opening a missing target succeeded")
	}
	var detail *fileIOError
	if !errors.As(err, &detail) || detail.code != "ENOENT" {
		t.Fatalf("missing target error = %v, want ENOENT", err)
	}
	if strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing target error exposed the host root or omitted input: %v", err)
	}
	if runtime.GOOS != "windows" {
		if _, err := host.open("escape", true, 8, false, false); err == nil {
			t.Fatal("outward target symlink was accepted")
		} else if strings.Contains(err.Error(), root) || !strings.Contains(err.Error(), "escape") {
			t.Fatalf("outward symlink error exposed the host root or omitted input: %v", err)
		}
	}
}

func TestFSWatchNativeBoundedQueueOverflow(t *testing.T) {
	rt, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: t.TempDir()}, true)
	ignored := openNativeFSWatch(t, host, ".", false, 1, true, false)
	resource := fsWatchResourceForTest(t, host)
	if !resource.enqueue(fsWatchEvent{typeName: "rename", filename: []byte("first")}) || !resource.enqueue(fsWatchEvent{typeName: "change", filename: []byte("dropped")}) || !resource.enqueue(fsWatchEvent{typeName: "rename", filename: []byte("dropped-again")}) {
		t.Fatal("ignore overflow unexpectedly failed")
	}
	if len(resource.queue) != 1 || resource.dropped != 2 {
		t.Fatalf("queue length=%d dropped=%d, want 1 and 2", len(resource.queue), resource.dropped)
	}
	result := awaitFSWatch(t, fsWatchNext(t, ignored))
	dropped, err := result.Get("dropped")
	if err != nil || dropped.Float() != 2 {
		t.Fatalf("delivered dropped count = %v, error = %v", dropped.Float(), err)
	}
	event, err := result.Get("value")
	if err != nil {
		t.Fatal(err)
	}
	filename, err := event.Get("filename")
	if err != nil || filename.String() != "first" {
		t.Fatalf("first queued filename = %q, error = %v", filename.String(), err)
	}
	fsWatchCall(t, ignored, "close")

	failed := openNativeFSWatch(t, host, ".", false, 1, false, false)
	failedResource := fsWatchResourceForTest(t, host)
	if !failedResource.enqueue(fsWatchEvent{typeName: "rename", filename: []byte("one")}) {
		t.Fatal("initial event was rejected")
	}
	if failedResource.enqueue(fsWatchEvent{typeName: "rename", filename: []byte("two")}) {
		t.Fatal("queue overflow did not fail")
	}
	var detail *fileIOError
	if len(failedResource.queue) != 0 || !errors.As(failedResource.terminalErr, &detail) || detail.code != "ERR_FS_WATCH_QUEUE_OVERFLOW" {
		t.Fatalf("overflow state queue=%d error=%v", len(failedResource.queue), failedResource.terminalErr)
	}
	_, err = fsWatchNext(t, failed).Await(context.Background())
	var exception *quickjs.Error
	if !errors.As(err, &exception) {
		t.Fatalf("overflow next error = %v, want JS rejection", err)
	}
	code, err := exception.Value().Get("code")
	if err != nil || code.String() != "ERR_FS_WATCH_QUEUE_OVERFLOW" {
		t.Fatalf("overflow rejection code = %q, error = %v", code.String(), err)
	}
	fsWatchCall(t, failed, "close")
	if rt.Busy() {
		t.Fatal("overflow or close left native work referenced")
	}
}

func TestFSWatchNativeRuntimeLoopAndScopeCleanup(t *testing.T) {
	t.Run("runtime", func(t *testing.T) {
		rt, _, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: t.TempDir()}, true)
		_ = openNativeFSWatch(t, host, ".", true, 8, false, false)
		resource := fsWatchResourceForTest(t, host)
		if err := rt.Close(); err != nil {
			t.Fatal(err)
		}
		select {
		case <-resource.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("runtime close did not join the watcher reader")
		}
		resource.mu.Lock()
		closed := resource.closed
		resource.mu.Unlock()
		if !closed {
			t.Fatal("runtime close left watcher open")
		}
	})

	t.Run("loop", func(t *testing.T) {
		_, loop, _, host := newNativeFSWatchTest(t, filesystem.Config{Cwd: t.TempDir()}, true)
		_ = openNativeFSWatch(t, host, ".", true, 8, false, false)
		resource := fsWatchResourceForTest(t, host)
		loop.Close()
		select {
		case <-host.closeDone:
		case <-time.After(2 * time.Second):
			t.Fatal("loop close did not complete native watcher cleanup")
		}
		select {
		case <-resource.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("loop close did not join the watcher reader")
		}
		resource.mu.Lock()
		closed := resource.closed
		resource.mu.Unlock()
		if !closed {
			t.Fatal("loop close left watcher open")
		}
	})

	t.Run("scope", func(t *testing.T) {
		rt := quickjs.New()
		loop := NewLoop(rt)
		rt.OnClose(loop.Close)
		t.Cleanup(func() { loop.Close(); _ = rt.Close() })
		scope, err := NewExecutionScope(loop, context.Background())
		if err != nil {
			t.Fatal(err)
		}
		backend, err := filesystem.New(filesystem.Config{Cwd: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		f := &fsHost{rt: rt, cfg: &FS{Loop: loop}, backend: backend}
		host, err := newFSWatchHost(f)
		if err != nil {
			t.Fatal(err)
		}
		_ = openNativeFSWatch(t, host, ".", true, 8, false, false)
		resource := fsWatchResourceForTest(t, host)
		scope.Close()
		select {
		case <-host.closeDone:
		case <-time.After(2 * time.Second):
			t.Fatal("scope close did not complete native watcher cleanup")
		}
		select {
		case <-resource.readerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("scope close did not join the watcher reader")
		}
		resource.mu.Lock()
		closed := resource.closed
		resource.mu.Unlock()
		if !closed {
			t.Fatal("scope close left watcher open")
		}
		if _, err := rt.Eval("40+2"); err != nil {
			t.Fatalf("scope cleanup closed its borrowed runtime: %v", err)
		}
	})
}
