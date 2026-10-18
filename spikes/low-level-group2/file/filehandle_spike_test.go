//go:build lowlevel2_spike

package filehandle

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
	"github.com/go-quickjs/go-quickjs/stdlib"
)

// These tests are an opt-in Go↔QuickJS prototype. The handle module is local
// to this spike and does not alter NOE's runtime or the pinned stdlib.

func TestFileHandleAgainstRangeStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt, loop, host := newSpikeRuntime(t)
	if err := host.install(); err != nil {
		t.Fatal(err)
	}

	mainSource := `
import fs from "fs";
import { open } from "spike:file";

const range = fs.createReadStream(` + jsQuote(path) + `, { start: 2, end: 4 }).getReader();
const rangeFirst = await range.read();
const rangeSecond = await range.read();

const handle = open(` + jsQuote(path) + `, "r+");
const atTwo = await handle.readAt(2, 2);
const atZero = await handle.readAt(0, 2);
const shortAtEnd = await handle.readAt(4, 4);
const eofAtEnd = await handle.readAt(6, 2);
const bytesWritten = await handle.writeAt(2, new Uint8Array([88, 89]));
const afterWrite = await fs.promises.readFile(` + jsQuote(path) + `);
await handle.close();
await handle.close();
let readAfterCloseRejected = false;
try { await handle.readAt(0, 1); } catch { readAfterCloseRejected = true; }

export const report = JSON.stringify({
  rangeBytes: Array.from(rangeFirst.value),
  rangeDoneAfterOneChunk: rangeSecond.done,
  atTwo: Array.from(atTwo.data),
  atZero: Array.from(atZero.data),
  shortBytes: Array.from(shortAtEnd.data),
  shortReadCount: shortAtEnd.bytesRead,
  shortReadEOF: shortAtEnd.eof,
  eofReadCount: eofAtEnd.bytesRead,
  eofReadEOF: eofAtEnd.eof,
  bytesWritten,
  afterWrite: Array.from(afterWrite),
  readAfterCloseRejected
});
`
	ns := evalModule(t, rt, loop, mainSource)
	reportValue, err := ns.Get("report")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		RangeBytes             []byte `json:"rangeBytes"`
		RangeDoneAfterOneChunk bool   `json:"rangeDoneAfterOneChunk"`
		AtTwo                  []byte `json:"atTwo"`
		AtZero                 []byte `json:"atZero"`
		ShortBytes             []byte `json:"shortBytes"`
		ShortReadCount         int    `json:"shortReadCount"`
		ShortReadEOF           bool   `json:"shortReadEOF"`
		EOFReadCount           int    `json:"eofReadCount"`
		EOFReadEOF             bool   `json:"eofReadEOF"`
		BytesWritten           int    `json:"bytesWritten"`
		AfterWrite             []byte `json:"afterWrite"`
		ReadAfterCloseRejected bool   `json:"readAfterCloseRejected"`
	}
	if err := json.Unmarshal([]byte(reportValue.String()), &got); err != nil {
		t.Fatalf("decode JS report: %v; report=%s", err, reportValue.String())
	}
	if string(got.RangeBytes) != "cde" || !got.RangeDoneAfterOneChunk {
		t.Errorf("range stream = %q, done=%v; want one fixed cde range then done", got.RangeBytes, got.RangeDoneAfterOneChunk)
	}
	if string(got.AtTwo) != "cd" || string(got.AtZero) != "ab" {
		t.Errorf("same handle positional reads = %q then %q; want cd then ab", got.AtTwo, got.AtZero)
	}
	if string(got.ShortBytes) != "ef" || got.ShortReadCount != 2 || !got.ShortReadEOF {
		t.Errorf("short EOF read = %q, count=%d, eof=%v; want ef, 2, true", got.ShortBytes, got.ShortReadCount, got.ShortReadEOF)
	}
	if got.EOFReadCount != 0 || !got.EOFReadEOF {
		t.Errorf("EOF read count=%d eof=%v; want 0, true", got.EOFReadCount, got.EOFReadEOF)
	}
	if got.BytesWritten != 2 || string(got.AfterWrite) != "abXYef" {
		t.Errorf("partial WriteAt = %d, final bytes %q; want 2 and abXYef", got.BytesWritten, got.AfterWrite)
	}
	if !got.ReadAfterCloseRejected {
		t.Error("readAt after close resolved; want rejection")
	}
}

func TestOpenHandleDoesNotKeepRuntimeAliveAndRuntimeCloseReclaimsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "still-open.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	rt, loop, host := newSpikeRuntime(t)
	if err := host.install(); err != nil {
		t.Fatal(err)
	}
	ns, err := rt.EvalModule("main.mjs", `import { open } from "spike:file"; export const handle = open(`+jsQuote(path)+`, "r");`)
	if err != nil {
		t.Fatal(err)
	}
	handleValue, err := ns.Get("handle")
	if err != nil {
		t.Fatal(err)
	}
	if err := loop.Run(context.Background()); err != nil {
		t.Fatalf("idle loop: %v", err)
	}
	if rt.Busy() {
		t.Fatal("an open handle alone kept the QuickJS runtime busy")
	}
	handle := host.onlyHandle(t)
	if _, err := handle.file.Stat(); err != nil {
		t.Fatalf("handle was not still open after idle loop; JS value=%v: %v", handleValue.Kind(), err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if !handle.isClosed() {
		t.Fatal("runtime close did not close the open handle")
	}
	if _, err := handle.file.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Stat after runtime close = %v; want os.ErrClosed", err)
	}
}

func TestFileReadDoesNotBlockQuickJSTimerAndPromiseDelivery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "async.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, loop, host := newSpikeRuntime(t)
	if err := host.install(); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	allowRead := make(chan struct{})
	timerFired := make(chan struct{})
	var releaseOnce sync.Once
	var timerOnce sync.Once
	var releasedByWatchdog atomic.Bool
	watchDone := make(chan struct{})
	host.beforeRead = func() {
		close(started)
		<-allowRead
	}
	if err := rt.Set("markSpikeTimer", func() bool {
		timerOnce.Do(func() { close(timerFired) })
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("releaseSpikeRead", func() bool {
		releaseOnce.Do(func() { close(allowRead) })
		return true
	}); err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(watchDone)
		select {
		case <-timerFired:
		case <-time.After(time.Second):
			releasedByWatchdog.Store(true)
			releaseOnce.Do(func() { close(allowRead) })
		}
	}()

	mainSource := `
import { open } from "spike:file";
const handle = open(` + jsQuote(path) + `, "r");
const read = handle.readAt(0, 1);
await new Promise(resolve => setTimeout(() => {
  markSpikeTimer();
  releaseSpikeRead();
  resolve();
}, 0));
const result = await read;
export const timerRanWhileReadWasPending = result.bytesRead === 1;
`
	ns := evalModule(t, rt, loop, mainSource)
	select {
	case <-started:
	default:
		t.Fatal("read worker did not start")
	}
	select {
	case <-watchDone:
	case <-time.After(time.Second):
		t.Fatal("watchdog did not finish")
	}
	select {
	case <-timerFired:
	default:
		t.Fatal("the JS timer did not run")
	}
	if releasedByWatchdog.Load() {
		t.Fatal("watchdog released the read before the JS timer; file I/O blocked the owner loop")
	}
	timerResult, err := ns.Get("timerRanWhileReadWasPending")
	if err != nil || !timerResult.Bool() {
		t.Fatalf("timer/read result=%v, err=%v; want true", timerResult, err)
	}
}

func newSpikeRuntime(t *testing.T) (*quickjs.Runtime, *stdlib.Loop, *fileHost) {
	t.Helper()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	if err := stdlib.Install(rt, stdlib.Config{
		Stdout: io.Discard,
		Stderr: io.Discard,
		FS:     &stdlib.FS{},
		Loop:   loop,
	}); err != nil {
		t.Fatal(err)
	}
	host := newFileHost(rt, loop)
	t.Cleanup(func() { _ = rt.Close() })
	return rt, loop, host
}

func evalModule(t *testing.T, rt *quickjs.Runtime, loop *stdlib.Loop, source string) quickjs.Value {
	t.Helper()
	rt.SetModuleLoader(func(spec, _ string) (string, string, error) {
		if spec == "spike:main" {
			return source, "spike:main", nil
		}
		return "", "", os.ErrNotExist
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	v, err := rt.EvalContext(ctx, `import("spike:main")`)
	if err == nil {
		err = loop.Run(ctx)
	}
	if err == nil {
		v, err = v.Await(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// jsQuote emits a quoted JavaScript string for this test's temporary path.
func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

type fileHost struct {
	rt         *quickjs.Runtime
	loop       *stdlib.Loop
	mu         sync.Mutex
	handles    map[*fileHandle]struct{}
	beforeRead func()
}

func newFileHost(rt *quickjs.Runtime, loop *stdlib.Loop) *fileHost {
	h := &fileHost{rt: rt, loop: loop, handles: make(map[*fileHandle]struct{})}
	rt.OnClose(h.closeAll)
	return h
}

func (h *fileHost) install() error {
	return h.rt.SetModule("spike:file", map[string]any{"open": h.open})
}

func (h *fileHost) open(path, mode string) (quickjs.Value, error) {
	flags := os.O_RDONLY
	switch mode {
	case "r":
	case "r+":
		flags = os.O_RDWR
	default:
		return quickjs.Value{}, errors.New(`mode must be "r" or "r+"`)
	}
	f, err := os.OpenFile(path, flags, 0)
	if err != nil {
		return quickjs.Value{}, err
	}
	handle := &fileHandle{host: h, file: f}
	h.mu.Lock()
	h.handles[handle] = struct{}{}
	h.mu.Unlock()
	o := h.rt.NewObject()
	if err := errors.Join(
		o.Set("readAt", handle.readAt),
		o.Set("writeAt", handle.writeAt),
		o.Set("close", handle.closeValue),
	); err != nil {
		_ = handle.close()
		return quickjs.Value{}, err
	}
	return o, nil
}

func (h *fileHost) async(run func() (any, error), convert func(any) (any, error)) *quickjs.Promise {
	p := h.rt.NewPromise()
	h.loop.Begin()
	go func() {
		defer h.loop.Done()
		value, err := run()
		h.loop.Post(func() {
			if err != nil {
				p.RejectError(err)
				return
			}
			if convert != nil {
				value, err = convert(value)
				if err != nil {
					p.RejectError(err)
					return
				}
			}
			if err := p.Resolve(value); err != nil {
				p.RejectError(err)
			}
		})
	}()
	return p
}

func (h *fileHost) closeAll() {
	h.mu.Lock()
	handles := make([]*fileHandle, 0, len(h.handles))
	for handle := range h.handles {
		handles = append(handles, handle)
	}
	h.mu.Unlock()
	for _, handle := range handles {
		_ = handle.close()
	}
}

func (h *fileHost) remove(handle *fileHandle) {
	h.mu.Lock()
	delete(h.handles, handle)
	h.mu.Unlock()
}

func (h *fileHost) onlyHandle(t *testing.T) *fileHandle {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.handles) != 1 {
		t.Fatalf("open handles = %d; want 1", len(h.handles))
	}
	for handle := range h.handles {
		return handle
	}
	panic("unreachable")
}

type readAtResult struct {
	data      []byte
	bytesRead int
	eof       bool
}

type fileHandle struct {
	host *fileHost
	file *os.File

	mu       sync.Mutex
	closed   bool
	closeMu  sync.Mutex
	closeErr error
}

func (f *fileHandle) readAt(offset int64, length int) *quickjs.Promise {
	if offset < 0 || length < 0 {
		p := f.host.rt.NewPromise()
		p.RejectError(errors.New("offset and length must be nonnegative"))
		return p
	}
	return f.host.async(func() (any, error) {
		if f.host.beforeRead != nil {
			f.host.beforeRead()
		}
		file, err := f.openFile()
		if err != nil {
			return nil, err
		}
		buf := make([]byte, length)
		n, readErr := file.ReadAt(buf, offset)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			return nil, readErr
		}
		return readAtResult{data: append([]byte(nil), buf[:n]...), bytesRead: n, eof: errors.Is(readErr, io.EOF)}, nil
	}, func(value any) (any, error) {
		result := value.(readAtResult)
		obj := f.host.rt.NewObject()
		if err := errors.Join(
			obj.Set("data", f.host.rt.NewBytes(result.data)),
			obj.Set("bytesRead", result.bytesRead),
			obj.Set("eof", result.eof),
		); err != nil {
			return nil, err
		}
		return obj, nil
	})
}

func (f *fileHandle) writeAt(offset int64, input quickjs.Value) *quickjs.Promise {
	bytes, ok := input.Bytes()
	if !ok {
		p := f.host.rt.NewPromise()
		p.RejectError(errors.New("data must be a Uint8Array"))
		return p
	}
	data := append([]byte(nil), bytes...)
	if offset < 0 {
		p := f.host.rt.NewPromise()
		p.RejectError(errors.New("offset must be nonnegative"))
		return p
	}
	return f.host.async(func() (any, error) {
		file, err := f.openFile()
		if err != nil {
			return nil, err
		}
		n, err := file.WriteAt(data, offset)
		if err != nil {
			return nil, err
		}
		return n, nil
	}, nil)
}

func (f *fileHandle) closeValue() *quickjs.Promise {
	return f.host.async(func() (any, error) { return nil, f.close() }, nil)
}

func (f *fileHandle) openFile() (*os.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, os.ErrClosed
	}
	return f.file, nil
}

func (f *fileHandle) close() error {
	f.closeMu.Lock()
	defer f.closeMu.Unlock()
	f.mu.Lock()
	if f.closed {
		err := f.closeErr
		f.mu.Unlock()
		return err
	}
	f.closed = true
	f.mu.Unlock()
	err := f.file.Close()
	f.mu.Lock()
	f.closeErr = err
	f.mu.Unlock()
	f.host.remove(f)
	return err
}

func (f *fileHandle) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}
