package nativeio

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
)

func TestFileAPIReadWriteAtAndCwd(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "indexed.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := streamRuntime(t, Options{Cwd: cwd})
	ns := evalStreams(t, rt, loop, `import {open} from "noe:fs-handle";
const reader=await open("indexed.bin");
const initial=await reader.readAt(1,2);
await reader.close();
const file=await open("indexed.bin",{mode:"r+"});
const short=await file.readAt(4,4);
const zero=await file.readAt(6,0);
const count=await file.writeAt(2,new Uint8Array([88,89]));
await file.close();
export const report=JSON.stringify({initial:Array.from(initial.data),initialCount:initial.bytesRead,initialEOF:initial.eof,
  short:Array.from(short.data),shortCount:short.bytesRead,shortEOF:short.eof,
  zeroCount:zero.bytesRead,zeroEOF:zero.eof,count});`)
	var got struct {
		Initial      []byte `json:"initial"`
		InitialCount int    `json:"initialCount"`
		InitialEOF   bool   `json:"initialEOF"`
		Short        []byte `json:"short"`
		ShortCount   int    `json:"shortCount"`
		ShortEOF     bool   `json:"shortEOF"`
		ZeroCount    int    `json:"zeroCount"`
		ZeroEOF      bool   `json:"zeroEOF"`
		Count        int    `json:"count"`
	}
	decodeFileReport(t, ns, &got)
	if string(got.Initial) != "bc" || got.InitialCount != 2 || got.InitialEOF {
		t.Errorf("initial read = %q, count=%d, eof=%v", got.Initial, got.InitialCount, got.InitialEOF)
	}
	if string(got.Short) != "ef" || got.ShortCount != 2 || !got.ShortEOF {
		t.Errorf("short read = %q, count=%d, eof=%v", got.Short, got.ShortCount, got.ShortEOF)
	}
	if got.ZeroCount != 0 || got.ZeroEOF {
		t.Errorf("zero-length read count=%d eof=%v; want 0,false", got.ZeroCount, got.ZeroEOF)
	}
	if got.Count != 2 {
		t.Errorf("writeAt returned %d; want 2", got.Count)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abXYef" {
		t.Errorf("file after positional write = %q; want abXYef", data)
	}
}

func TestFileAPIValidatesInputsAndReturnsOSCode(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "existing.bin"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := streamRuntime(t, Options{Cwd: cwd})
	ns := evalStreams(t, rt, loop, `import {open} from "noe:fs-handle";
const file=await open("missing.bin").then(() => null, e => ({message:e.message,code:e.code}));
const invalidPath=await open("").then(() => null, e => e.code);
const invalidMode=await open("missing.bin",{mode:"w"}).then(() => null, e => e.code);
const missingRPlus=await open("should-not-create.bin",{mode:"r+"}).then(() => null, e => e.code);
const existing=await open("existing.bin",{mode:"r+"});
const invalidNumbers=[-1,1.5,NaN,Infinity,Number.MAX_SAFE_INTEGER+1];
const rejectedCode=p=>p.then(() => "resolved", e => e.code);
const readPositions=await Promise.all(invalidNumbers.map(n=>rejectedCode(existing.readAt(n,1))));
const readLengths=await Promise.all(invalidNumbers.map(n=>rejectedCode(existing.readAt(0,n))));
const writePositions=await Promise.all(invalidNumbers.map(n=>rejectedCode(existing.writeAt(n,new Uint8Array([1])))));
await existing.close();
const readonly=await open("existing.bin");
const readonlyWrite=await readonly.writeAt(0,new Uint8Array([9])).then(() => null,e=>({code:e.code,bytesWritten:e.bytesWritten}));
await readonly.close();
const closedZero=await readonly.readAt(0,0).then(() => "resolved",e=>e.code);
export const report=JSON.stringify({fileCode:file.code,fileMessage:file.message,invalidPath,invalidMode,missingRPlus,readPositions,readLengths,writePositions,readonlyWrite,closedZero});`)
	var got struct {
		FileCode       any      `json:"fileCode"`
		FileMessage    string   `json:"fileMessage"`
		InvalidPath    string   `json:"invalidPath"`
		InvalidMode    string   `json:"invalidMode"`
		MissingRPlus   any      `json:"missingRPlus"`
		ReadPositions  []string `json:"readPositions"`
		ReadLengths    []string `json:"readLengths"`
		WritePositions []string `json:"writePositions"`
		ReadonlyWrite  struct {
			Code         any `json:"code"`
			BytesWritten int `json:"bytesWritten"`
		} `json:"readonlyWrite"`
		ClosedZero string `json:"closedZero"`
	}
	decodeFileReport(t, ns, &got)
	if code, ok := got.FileCode.(float64); !ok || code == 0 {
		t.Errorf("OS open error code=%#v (%T); want a nonzero numeric OS code", got.FileCode, got.FileCode)
	}
	if got.FileMessage == "" || got.InvalidPath != "ERR_INVALID_ARG" || got.InvalidMode != "ERR_INVALID_ARG" {
		t.Errorf("invalid/open results = %+v", got)
	}
	if code, ok := got.MissingRPlus.(float64); !ok || code == 0 {
		t.Errorf("r+ missing-file code=%#v; want an OS error code", got.MissingRPlus)
	}
	checkInvalidCodes := func(name string, codes []string, want int) {
		t.Helper()
		if len(codes) != want {
			t.Errorf("%s returned %d results; want %d", name, len(codes), want)
			return
		}
		for i, code := range codes {
			if code != "ERR_INVALID_ARG" {
				t.Errorf("%s[%d] code=%q; want ERR_INVALID_ARG", name, i, code)
			}
		}
	}
	checkInvalidCodes("read position", got.ReadPositions, 5)
	checkInvalidCodes("read length", got.ReadLengths, 5)
	checkInvalidCodes("write position", got.WritePositions, 5)
	if code, ok := got.ReadonlyWrite.Code.(float64); !ok || code == 0 || got.ReadonlyWrite.BytesWritten != 0 {
		t.Errorf("read-only write result=%+v; want OS error code and bytesWritten 0", got.ReadonlyWrite)
	}
	if got.ClosedZero != "ERR_FILE_CLOSED" {
		t.Errorf("zero-length read after close code=%q; want ERR_FILE_CLOSED", got.ClosedZero)
	}
	if _, err := os.Stat(filepath.Join(cwd, "should-not-create.bin")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("r+ open created missing file: Stat error=%v", err)
	}
}

func TestOpenFileAloneDoesNotKeepRuntimeAliveAndCloseReclaimsIt(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "idle.bin")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, h, loop := streamRuntime(t, Options{Cwd: cwd})
	evalStreams(t, rt, loop, `import {open} from "noe:fs-handle"; export const file=await open("idle.bin");`)
	if rt.Busy() {
		t.Fatalf("open file kept runtime active: busy=%v", rt.Busy())
	}
	file := onlyOwnedFile(t, h)
	if _, err := file.file.(*os.File).Stat(); err != nil {
		t.Fatalf("open handle was not retained: %v", err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if !file.closed {
		t.Fatal("runtime close did not close the file")
	}
	if _, err := file.file.(*os.File).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Stat after runtime close = %v; want os.ErrClosed", err)
	}
}

func TestRuntimeCloseDisposesFileOpenedAfterCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late-open.bin")
	if err := os.WriteFile(path, []byte("late"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, h, _ := streamRuntime(t, Options{})
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseOpen := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseOpen)
	var opened *os.File
	promise := h.openFileUsing(path, "r", func(path string, flags int, perm os.FileMode) (fileBackend, error) {
		close(started)
		<-release
		file, err := os.OpenFile(path, flags, perm)
		opened = file
		return file, err
	})
	if promise == nil {
		t.Fatal("open did not return a promise")
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("open backend did not start")
	}
	closed := make(chan error, 1)
	go func() { closed <- rt.Close() }()
	select {
	case <-h.ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("runtime close did not cancel the file host")
	}
	releaseOpen()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("runtime close did not finish after open returned")
	}
	if opened == nil {
		t.Fatal("controlled open did not produce a file")
	}
	if _, err := opened.Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("late-open file Stat() = %v; want os.ErrClosed", err)
	}
}

func TestFileReadAndWriteErrorsRetainPartialResults(t *testing.T) {
	rt, h, loop := streamRuntime(t, Options{})
	backend := &fakeFileBackend{
		readData: []byte{0, 255, 12},
		readErr:  errors.New("read stopped after bytes"),
		writeN:   2,
		writeErr: errors.New("write stopped after bytes"),
	}
	file := registerFakeFile(t, rt, h, backend)
	ns := evalStreams(t, rt, loop, `const read=await globalThis.fakeFile.readAt(7,6).then(() => null, e => ({message:e.message,code:e.code,bytesRead:e.bytesRead,data:Array.from(e.data)}));
const input=new Uint8Array([1,2,3,4]);
const writing=globalThis.fakeFile.writeAt(11,input);
input.fill(99);
const write=await writing.then(() => null, e => ({message:e.message,code:e.code,bytesWritten:e.bytesWritten}));
await globalThis.fakeFile.close();
export const report=JSON.stringify({read,write});`)
	var got struct {
		Read struct {
			Message   string `json:"message"`
			Code      string `json:"code"`
			BytesRead int    `json:"bytesRead"`
			Data      []byte `json:"data"`
		} `json:"read"`
		Write struct {
			Message      string `json:"message"`
			Code         string `json:"code"`
			BytesWritten int    `json:"bytesWritten"`
		} `json:"write"`
	}
	decodeFileReport(t, ns, &got)
	if got.Read.BytesRead != 3 || !bytes.Equal(got.Read.Data, backend.readData) || got.Read.Code != "ERR_IO" || got.Read.Message != backend.readErr.Error() {
		t.Errorf("partial read error = %+v", got.Read)
	}
	if got.Write.BytesWritten != 2 || got.Write.Code != "ERR_IO" || got.Write.Message != backend.writeErr.Error() {
		t.Errorf("partial write error = %+v", got.Write)
	}
	if backend.writeOffset != 11 || !bytes.Equal(backend.written, []byte{1, 2, 3, 4}) {
		t.Errorf("write snapshot = offset %d bytes %v; want offset 11 and original bytes", backend.writeOffset, backend.written)
	}
	if !file.closed {
		t.Fatal("fake handle did not close")
	}
}

func TestReadAtRejectsMakeSliceRangeWithoutBackendRead(t *testing.T) {
	if goruntime.GOARCH != "amd64" && goruntime.GOARCH != "arm64" {
		t.Skip("the requested safe integer exceeds the Go slice allocation range on 64-bit targets")
	}
	rt, h, loop := streamRuntime(t, Options{})
	backend := &fakeFileBackend{readData: []byte{1}}
	registerFakeFile(t, rt, h, backend)
	ns := evalStreams(t, rt, loop, `const zero=await globalThis.fakeFile.readAt(0,0);
const large=await globalThis.fakeFile.readAt(0,Number.MAX_SAFE_INTEGER).then(() => null,e=>({code:e.code,message:e.message}));
await globalThis.fakeFile.close();
export const report=JSON.stringify({zeroIsBytes:zero.data instanceof Uint8Array,zeroCount:zero.bytesRead,zeroEOF:zero.eof,large});`)
	var got struct {
		ZeroIsBytes bool `json:"zeroIsBytes"`
		ZeroCount   int  `json:"zeroCount"`
		ZeroEOF     bool `json:"zeroEOF"`
		Large       struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"large"`
	}
	decodeFileReport(t, ns, &got)
	if !got.ZeroIsBytes || got.ZeroCount != 0 || got.ZeroEOF {
		t.Errorf("zero-length result = %+v; want empty Uint8Array, 0 bytes, no EOF", got)
	}
	if got.Large.Code != "ERR_INVALID_ARG" || got.Large.Message == "" {
		t.Errorf("oversize read result = %+v; want a range error", got.Large)
	}
	if backend.readCalls.Load() != 0 {
		t.Errorf("backend received %d reads for zero and rejected oversized request", backend.readCalls.Load())
	}
}

func TestFileCloseWaitsForStartedIOAndIsIdempotent(t *testing.T) {
	rt, h, loop := streamRuntime(t, Options{})
	backend := &fakeFileBackend{readData: []byte{42}, readErr: errors.New("read interrupted by close"), readStarted: make(chan struct{}), readGate: make(chan struct{}), readFinished: make(chan struct{})}
	registerFakeFile(t, rt, h, backend)
	if err := rt.Set("fakeReadStarted", func() bool {
		select {
		case <-backend.readStarted:
			return true
		default:
			return false
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("fakeReadFinished", func() bool {
		select {
		case <-backend.readFinished:
			return true
		default:
			return false
		}
	}); err != nil {
		t.Fatal(err)
	}
	ns := evalStreams(t, rt, loop, `const pending=globalThis.fakeFile.readAt(0,1).then(() => "resolved", e => e.code);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
await globalThis.fakeFile.close();
const closedAfterRead=fakeReadFinished();
const readResult=await pending;
await globalThis.fakeFile.close();
const lateRead=await globalThis.fakeFile.readAt(0,1).then(() => "resolved", e => e.code);
export const report=JSON.stringify({closedAfterRead,readResult,lateRead});`)
	var got struct {
		ClosedAfterRead bool   `json:"closedAfterRead"`
		ReadResult      string `json:"readResult"`
		LateRead        string `json:"lateRead"`
	}
	decodeFileReport(t, ns, &got)
	if !got.ClosedAfterRead || got.ReadResult != "ERR_IO" || got.LateRead != "ERR_FILE_CLOSED" {
		t.Errorf("close/read results = %+v", got)
	}
	if backend.closeCount.Load() != 1 {
		t.Errorf("backend close count=%d; want one", backend.closeCount.Load())
	}
}

func TestFileCloseRejectsNewIOImmediately(t *testing.T) {
	rt, h, loop := streamRuntime(t, Options{})
	backend := &fakeFileBackend{readData: []byte{5}}
	registerFakeFile(t, rt, h, backend)
	ns := evalStreams(t, rt, loop, `const closing=globalThis.fakeFile.close();
const read=await globalThis.fakeFile.readAt(0,1).then(() => "resolved", e => e.code);
const write=await globalThis.fakeFile.writeAt(0,new Uint8Array([1])).then(() => "resolved", e => e.code);
await closing;
export const report=JSON.stringify({read,write});`)
	var got struct {
		Read  string `json:"read"`
		Write string `json:"write"`
	}
	decodeFileReport(t, ns, &got)
	if got.Read != "ERR_FILE_CLOSED" || got.Write != "ERR_FILE_CLOSED" {
		t.Errorf("I/O after close call = read %q, write %q; want immediate closed errors", got.Read, got.Write)
	}
	if backend.readCalls.Load() != 0 || backend.writeCalls.Load() != 0 {
		t.Errorf("backend received post-close I/O: reads=%d writes=%d", backend.readCalls.Load(), backend.writeCalls.Load())
	}
}

func TestFileReadCompletesOnOwnerAfterTimerRuns(t *testing.T) {
	rt, h, loop := streamRuntime(t, Options{})
	backend := &fakeFileBackend{readData: []byte{7}, readGate: make(chan struct{}), readStarted: make(chan struct{})}
	registerFakeFile(t, rt, h, backend)
	if err := rt.Set("fakeReadStarted", func() bool {
		select {
		case <-backend.readStarted:
			return true
		default:
			return false
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("releaseFakeRead", func() bool {
		backend.releaseRead()
		return true
	}); err != nil {
		t.Fatal(err)
	}
	ns := evalStreams(t, rt, loop, `const pending=globalThis.fakeFile.readAt(0,1);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
await new Promise(resolve=>setTimeout(()=>{releaseFakeRead();resolve();},0));
const result=await pending;
await globalThis.fakeFile.close();
export const report=result.bytesRead===1&&result.data[0]===7;`)
	value, err := ns.Get("report")
	if err != nil || !value.Bool() {
		t.Fatalf("read result=%v err=%v; want owner-delivered byte", value, err)
	}
}

type fakeFileBackend struct {
	readData     []byte
	readErr      error
	readStarted  chan struct{}
	readFinished chan struct{}
	readGate     chan struct{}
	readOnce     sync.Once
	written      []byte
	writeOffset  int64
	writeN       int
	writeErr     error
	mu           sync.Mutex
	closeOnce    sync.Once
	closeCount   atomic.Int32
	readCalls    atomic.Int32
	writeCalls   atomic.Int32
}

func (f *fakeFileBackend) ReadAt(p []byte, _ int64) (int, error) {
	f.readCalls.Add(1)
	if f.readStarted != nil {
		f.readOnce.Do(func() { close(f.readStarted) })
	}
	if f.readGate != nil {
		<-f.readGate
	}
	if f.readFinished != nil {
		close(f.readFinished)
	}
	n := copy(p, f.readData)
	return n, f.readErr
}

func (f *fakeFileBackend) WriteAt(p []byte, offset int64) (int, error) {
	f.writeCalls.Add(1)
	f.mu.Lock()
	f.written = append([]byte(nil), p...)
	f.writeOffset = offset
	f.mu.Unlock()
	n := f.writeN
	if n > len(p) {
		n = len(p)
	}
	return n, f.writeErr
}

func (f *fakeFileBackend) Close() error {
	f.closeCount.Add(1)
	f.closeOnce.Do(func() {
		if f.readGate != nil {
			close(f.readGate)
		}
	})
	return nil
}

func (f *fakeFileBackend) releaseRead() {
	if f.readGate != nil {
		f.closeOnce.Do(func() { close(f.readGate) })
	}
}

func registerFakeFile(t *testing.T, rt *quickjs.Runtime, h *Host, backend fileBackend) *fileHandle {
	t.Helper()
	f := newFileHandle(h, backend)
	if !h.own(f) {
		t.Fatal("host rejected fake file handle")
	}
	value, err := f.object()
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("fakeFile", value); err != nil {
		t.Fatal(err)
	}
	return f
}

func onlyOwnedFile(t *testing.T, h *Host) *fileHandle {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for resource := range h.resources {
		if file, ok := resource.(*fileHandle); ok {
			return file
		}
	}
	t.Fatal("no file handle is registered")
	return nil
}

func decodeFileReport(t *testing.T, module quickjs.Value, target any) {
	t.Helper()
	value, err := module.Get("report")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(value.String()), target); err != nil {
		t.Fatalf("decode report %q: %v", value.String(), err)
	}
}
