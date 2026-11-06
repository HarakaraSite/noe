package stdlib

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/filesystem"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func TestOpenFileAloneDoesNotKeepRuntimeAliveAndCloseReclaimsIt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owned.bin")
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, h, loop := fileRuntime(t, "")
	if err := rt.Set("openOwnedFile", h.openFile); err != nil {
		t.Fatal(err)
	}
	ns := evalFiles(t, rt, loop, `export const file=await openOwnedFile(`+strconv.Quote(path)+`,"r",0o666);`)
	if _, err := ns.Get("file"); err != nil {
		t.Fatal(err)
	}
	file := onlyOwnedFile(t, h)
	if _, err := file.file.(*os.File).Stat(); err != nil {
		t.Fatal(err)
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	if !file.closed {
		t.Fatal("runtime did not reclaim open handle")
	}
	if _, err := file.file.(*os.File).Stat(); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Stat after shutdown=%v", err)
	}
}

func TestRuntimeCloseDisposesFileOpenedAfterCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "late-open.bin")
	if err := os.WriteFile(path, []byte("late"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, h, _ := fileRuntime(t, "")
	started := make(chan struct{})
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseOpen := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseOpen)
	var opened *os.File
	promise := h.openFileUsing(path, "r", 0o666, func(path string, flags int, perm os.FileMode) (fileBackend, error) {
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
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{
		readData: []byte{0, 255, 12},
		readErr:  errors.New("read stopped after bytes"),
		writeN:   2,
		writeErr: errors.New("write stopped after bytes"),
	}
	file := registerFakeFile(t, rt, h, backend)
	ns := evalFiles(t, rt, loop, `const read=await globalThis.fakeFile.readAt(7,6).then(() => null, e => ({message:e.message,code:e.code,bytesRead:e.bytesRead,data:Array.from(e.data)}));
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

func TestFileReadRejectsMakeSliceRangeWithoutBackendRead(t *testing.T) {
	if goruntime.GOARCH != "amd64" && goruntime.GOARCH != "arm64" {
		t.Skip("64-bit slice allocation range")
	}
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{readData: []byte{1}}
	f := registerFakeFile(t, rt, h, backend)
	if err := rt.Set("nativeFile", mustFileObject(t, f)); err != nil {
		t.Fatal(err)
	}
	ns := evalFiles(t, rt, loop, `const zero=await fakeFile.read(0);
const large=await nativeFile.read(0,Number.MAX_SAFE_INTEGER).then(()=>null,e=>({code:e.code,message:e.message}));
await fakeFile.close();export const report=JSON.stringify({zeroCount:zero.length,large});`)
	var got struct {
		ZeroCount int
		Large     struct{ Code, Message string }
	}
	decodeFileReport(t, ns, &got)
	if got.ZeroCount != 0 || got.Large.Code != "ERR_INVALID_ARG" || got.Large.Message == "" {
		t.Fatalf("results=%+v", got)
	}
	if backend.readCalls.Load() != 0 {
		t.Fatal("invalid/empty read reached backend")
	}
}

func TestFileCloseWaitsForStartedIOAndIsIdempotent(t *testing.T) {
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{readData: []byte{42}, readStarted: make(chan struct{}), readGate: make(chan struct{}), readFinished: make(chan struct{})}
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
	if err := rt.Set("releaseFakeRead", func() { backend.releaseRead() }); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("fakeCloseCount", func() int32 { return backend.closeCount.Load() }); err != nil {
		t.Fatal(err)
	}
	ns := evalFiles(t, rt, loop, `const pending=globalThis.fakeFile.read(1).then(() => "resolved", e => e.code);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
const closing=globalThis.fakeFile.close();
await new Promise(resolve=>setTimeout(()=>{if(fakeCloseCount()!==0)throw Error("graceful close interrupted read");releaseFakeRead();resolve();},5));
await closing;
const closedAfterRead=fakeReadFinished();
const readResult=await pending;
await globalThis.fakeFile.close();
const lateRead=await globalThis.fakeFile.read(1).then(() => "resolved", e => e.code);
export const report=JSON.stringify({closedAfterRead,readResult,lateRead});`)
	var got struct {
		ClosedAfterRead bool   `json:"closedAfterRead"`
		ReadResult      string `json:"readResult"`
		LateRead        string `json:"lateRead"`
	}
	decodeFileReport(t, ns, &got)
	if !got.ClosedAfterRead || got.ReadResult != "resolved" || got.LateRead != "ERR_FILE_CLOSED" {
		t.Errorf("close/read results = %+v", got)
	}
	if backend.closeCount.Load() != 1 {
		t.Errorf("backend close count=%d; want one", backend.closeCount.Load())
	}
}

func TestFileCloseRejectsNewIOImmediately(t *testing.T) {
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{readData: []byte{5}}
	registerFakeFile(t, rt, h, backend)
	ns := evalFiles(t, rt, loop, `const closing=globalThis.fakeFile.close();
const read=await globalThis.fakeFile.read(1).then(() => "resolved", e => e.code);
const write=await globalThis.fakeFile.write(new Uint8Array([1])).then(() => "resolved", e => e.code);
const stat=await globalThis.fakeFile.stat().then(() => "resolved", e => e.code);
const truncate=await globalThis.fakeFile.truncate(1).then(() => "resolved", e => e.code);
const sync=await globalThis.fakeFile.sync().then(() => "resolved", e => e.code);
await closing;
export const report=JSON.stringify({read,write,stat,truncate,sync});`)
	var got struct {
		Read     string `json:"read"`
		Write    string `json:"write"`
		Stat     string `json:"stat"`
		Truncate string `json:"truncate"`
		Sync     string `json:"sync"`
	}
	decodeFileReport(t, ns, &got)
	if got.Read != "ERR_FILE_CLOSED" || got.Write != "ERR_FILE_CLOSED" || got.Stat != "ERR_FILE_CLOSED" || got.Truncate != "ERR_FILE_CLOSED" || got.Sync != "ERR_FILE_CLOSED" {
		t.Errorf("operations after close call = %+v; want immediate closed errors", got)
	}
	if backend.readCalls.Load() != 0 || backend.writeCalls.Load() != 0 || backend.statCalls.Load() != 0 || backend.truncateCalls.Load() != 0 || backend.syncCalls.Load() != 0 {
		t.Errorf("backend received post-close I/O: reads=%d writes=%d stats=%d truncates=%d syncs=%d", backend.readCalls.Load(), backend.writeCalls.Load(), backend.statCalls.Load(), backend.truncateCalls.Load(), backend.syncCalls.Load())
	}
}

func TestFileCloseWaitsForStartedSyncAndRejectsNewMethods(t *testing.T) {
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{syncStarted: make(chan struct{}), syncGate: make(chan struct{})}
	registerFakeFile(t, rt, h, backend)
	if err := rt.Set("fakeSyncStarted", func() bool {
		select {
		case <-backend.syncStarted:
			return true
		default:
			return false
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("releaseFakeSync", func() bool { backend.releaseSync(); return true }); err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("fakeCloseCount", func() int32 { return backend.closeCount.Load() }); err != nil {
		t.Fatal(err)
	}
	ns := evalFiles(t, rt, loop, `const syncing=globalThis.fakeFile.sync();
async function waitForSync(){if(!fakeSyncStarted()){await new Promise(r=>setTimeout(r,0));return waitForSync();}}
await waitForSync();
const closing=globalThis.fakeFile.close();
const late=await Promise.all([
 globalThis.fakeFile.stat().then(()=>"resolved",e=>e.code),
 globalThis.fakeFile.truncate(1).then(()=>"resolved",e=>e.code),
 globalThis.fakeFile.sync().then(()=>"resolved",e=>e.code),
]);
await new Promise(resolve=>setTimeout(resolve,5));
const closeBeforeSyncFinishes=fakeCloseCount();
releaseFakeSync();
await Promise.all([syncing,closing]);
export const report=JSON.stringify({late,closeBeforeSyncFinishes});`)
	var got struct {
		Late                    []string `json:"late"`
		CloseBeforeSyncFinishes int32    `json:"closeBeforeSyncFinishes"`
	}
	decodeFileReport(t, ns, &got)
	if len(got.Late) != 3 || got.Late[0] != "ERR_FILE_CLOSED" || got.Late[1] != "ERR_FILE_CLOSED" || got.Late[2] != "ERR_FILE_CLOSED" || got.CloseBeforeSyncFinishes != 0 {
		t.Errorf("sync/close admission results=%+v", got)
	}
	if backend.syncCalls.Load() != 1 || backend.closeCount.Load() != 1 {
		t.Errorf("sync calls=%d, close calls=%d; want one admitted sync and one close", backend.syncCalls.Load(), backend.closeCount.Load())
	}
}

func TestFileReadCompletesOnOwnerAfterTimerRuns(t *testing.T) {
	rt, h, loop := fileRuntime(t, "")
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
	ns := evalFiles(t, rt, loop, `const pending=globalThis.fakeFile.read(1);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
await new Promise(resolve=>setTimeout(()=>{releaseFakeRead();resolve();},0));
const result=await pending;
await globalThis.fakeFile.close();
export const report=result.length===1&&result[0]===7;`)
	value, err := ns.Get("report")
	if err != nil || !value.Bool() {
		t.Fatalf("read result=%v err=%v; want owner-delivered byte", value, err)
	}
}

type fakeFileBackend struct {
	readData        []byte
	readErr         error
	readStarted     chan struct{}
	readFinished    chan struct{}
	readGate        chan struct{}
	readOnce        sync.Once
	statInfo        os.FileInfo
	statErr         error
	truncateErr     error
	truncateLen     int64
	syncErr         error
	syncStarted     chan struct{}
	syncGate        chan struct{}
	syncStartedOnce sync.Once
	syncGateOnce    sync.Once
	written         []byte
	writeOffset     int64
	writeN          int
	writeErr        error
	mu              sync.Mutex
	closeOnce       sync.Once
	closeCount      atomic.Int32
	readCalls       atomic.Int32
	writeCalls      atomic.Int32
	writeAtCalls    atomic.Int32
	writeSeqCalls   atomic.Int32
	statCalls       atomic.Int32
	truncateCalls   atomic.Int32
	syncCalls       atomic.Int32
}

func (f *fakeFileBackend) Read(p []byte) (int, error) { return f.ReadAt(p, 0) }
func (f *fakeFileBackend) Write(p []byte) (int, error) {
	f.writeSeqCalls.Add(1)
	return f.recordWrite(p, -1)
}

func (f *fakeFileBackend) Stat() (os.FileInfo, error) {
	f.statCalls.Add(1)
	if f.statErr != nil {
		return nil, f.statErr
	}
	if f.statInfo == nil {
		return nil, errors.New("fake stat info is not configured")
	}
	return f.statInfo, nil
}

func (f *fakeFileBackend) Truncate(length int64) error {
	f.truncateCalls.Add(1)
	f.mu.Lock()
	f.truncateLen = length
	f.mu.Unlock()
	return f.truncateErr
}

func (f *fakeFileBackend) Sync() error {
	f.syncCalls.Add(1)
	if f.syncStarted != nil {
		f.syncStartedOnce.Do(func() { close(f.syncStarted) })
	}
	if f.syncGate != nil {
		<-f.syncGate
	}
	return f.syncErr
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
	f.writeAtCalls.Add(1)
	return f.recordWrite(p, offset)
}

func (f *fakeFileBackend) recordWrite(p []byte, offset int64) (int, error) {
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
		if f.syncGate != nil {
			f.syncGateOnce.Do(func() { close(f.syncGate) })
		}
	})
	return nil
}

func (f *fakeFileBackend) releaseSync() {
	if f.syncGate != nil {
		f.syncGateOnce.Do(func() { close(f.syncGate) })
	}
}

func (f *fakeFileBackend) releaseRead() {
	if f.readGate != nil {
		f.closeOnce.Do(func() { close(f.readGate) })
	}
}

func registerFakeFile(t *testing.T, rt *quickjs.Runtime, h *fileHost, backend fileBackend) *fileHandle {
	t.Helper()
	f := newFileHandle(h, backend)
	if !h.own(f) {
		t.Fatal("host rejected fake file handle")
	}
	native := rt.NewObject()
	factory, err := evalWithHost(rt, "<test-filehandle>", fileHandleJS, native)
	if err != nil {
		t.Fatal(err)
	}
	wrap, err := factory.Get("handle")
	if err != nil {
		t.Fatal(err)
	}
	value, err := wrap.Call(mustFileObject(t, f))
	if err != nil {
		t.Fatal(err)
	}
	if err := rt.Set("fakeFile", value); err != nil {
		t.Fatal(err)
	}
	return f
}

func onlyOwnedFile(t *testing.T, h *fileHost) *fileHandle {
	t.Helper()
	h.mu.Lock()
	defer h.mu.Unlock()
	for file := range h.resources {
		return file
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

func mustFileObject(t *testing.T, f *fileHandle) quickjs.Value {
	t.Helper()
	v, err := f.object()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func fileRuntime(t *testing.T, cwd string) (*quickjs.Runtime, *fileHost, *Loop) {
	t.Helper()
	rt := quickjs.New()
	loop := NewLoop(rt)
	rt.OnClose(loop.Close)
	t.Cleanup(func() { _ = rt.Close() })
	if err := Install(rt, Config{Loop: loop, FS: &FS{Cwd: cwd}}); err != nil {
		t.Fatal(err)
	}
	backend, err := filesystem.New(filesystem.Config{Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	fs := &fsHost{rt: rt, cfg: &FS{Loop: loop}, backend: backend}
	if err := fs.installMetadata(); err != nil {
		t.Fatal(err)
	}
	h, err := newFileHost(fs)
	if err != nil {
		t.Fatal(err)
	}
	return rt, h, loop
}
func evalFiles(t *testing.T, rt *quickjs.Runtime, loop *Loop, src string) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt.SetModuleLoader(func(spec, ref string) (string, string, error) {
		if spec != "file-test" {
			return "", "", errors.New("unknown module: " + spec)
		}
		return src, "file-test", nil
	})
	v, err := rt.EvalContext(ctx, `import("file-test")`)
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

func TestFileHandlePublicAPISequentialAndPositional(t *testing.T) {
	cwd := t.TempDir()
	path := filepath.Join(cwd, "indexed.bin")
	if err := os.WriteFile(path, []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	ns := evalFiles(t, rt, loop, `import fs,{open} from "noe:fs";
if(open!==fs.open||!("readFile" in fs)||("promises" in fs))throw Error("filesystem exports differ");
let removed=false;try{await import("noe:fs-handle")}catch{removed=true}
if(!removed)throw Error("retired module still exists");
let nodeAliasRemoved=false;try{await import("node:noe:fs")}catch{nodeAliasRemoved=true}
if(!nodeAliasRemoved)throw Error("unexpected node: alias for noe:fs");
const f=await open("indexed.bin",{flags:"r+"});
try{
 const first=await f.read(2);
 if(!(first instanceof Uint8Array)||new TextDecoder().decode(first)!=="ab")throw Error("sequential read");
 const part=await f.readAt(4,4);
 if(new TextDecoder().decode(part)!=="ef")throw Error("positional short read");
 const next=await f.read(2);
 if(new TextDecoder().decode(next)!=="cd")throw Error("positional read changed cursor");
 const written=await f.write("XY");
 if(written!==2)throw Error("string write");
 const writeBacking=new Uint8Array([0,90,81,0]);const writeView=new DataView(writeBacking.buffer,1,2);
 const writing=f.writeAt(1,writeView);
 writeBacking.fill(99);
 const write=await writing;
 if(write!==2)throw Error("write view snapshot");
 const eof=await f.read(2);
 if(eof.length!==0||typeof f.readAt!=="function"||typeof f.writeAt!=="function")throw Error("handle surface/cursor");
 await f.close();await f.close();
 const closed=await f.read(0).then(()=>false,e=>e.code==="ERR_FILE_CLOSED");
 if(!closed)throw Error("zero read after close");
}finally{await f.close()}
const defaultRead=await open("indexed.bin");
const defaultBytes=await defaultRead.read();
await defaultRead.close();
if(new TextDecoder().decode(defaultBytes)!=="aZQdXY")throw Error("default read length");
export const report=fs.readFileSync("indexed.bin","utf8");`)
	report, err := ns.Get("report")
	if err != nil || report.String() != "aZQdXY" {
		t.Fatalf("file=%s, error=%v", report.String(), err)
	}
}

func TestFileHandlePublicValidationAndOpenErrors(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	ns := evalFiles(t, rt, loop, `import {open} from "noe:fs";
const code=p=>p.then(()=>"resolved",e=>e.code);
const missing=await open("missing").then(()=>null,e=>e);
if(missing?.code!=="ENOENT"||typeof missing.errno!=="number"||missing.errno<=0)throw Error("missing filesystem error");
const invalid=[open(""),open("file",{mode:"r+"}),open("file","r+"),open("file",null)];
const f=await open("file",{flags:"r+"});
for(const number of [-1,1.5,NaN,Infinity,Number.MAX_SAFE_INTEGER+1]){
 invalid.push(f.readAt(number,1),f.readAt(0,number),f.writeAt(number,new Uint8Array(2)));
 if(number!==-1)invalid.push(f.truncate(number));
}
invalid.push(f.write({}),f.write(new ArrayBuffer(2)));
const codes=await Promise.all(invalid.map(code));
if(codes.some(c=>c!=="ERR_INVALID_ARG"))throw Error("validation codes "+JSON.stringify(codes));
await f.close();
const r=await open("file");
const denied=await r.write(new Uint8Array([1])).then(()=>null,e=>({code:e.code,errno:e.errno,bytesWritten:e.bytesWritten}));
await r.close();if(typeof denied?.code!=="string"||typeof denied.errno!=="number"||denied.errno<=0||denied.bytesWritten!==0)throw Error("readonly descriptor");
const noCreate=await open("must-not-create",{flags:"r+"}).then(()=>false,e=>e.code==="ENOENT");
if(!noCreate)throw Error("r+ created a missing path");
export const report=true;`)
	result, err := ns.Get("report")
	if err != nil || !result.Bool() {
		t.Fatalf("report=%v, error=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "must-not-create")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("r+ created file: %v", err)
	}
}

func TestFileHandleOpenFlagsCreateExclusiveAppendAndMode(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "existing"), []byte("old contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	if err := rt.Set("permissionsAvailable", goruntime.GOOS != "windows"); err != nil {
		t.Fatal(err)
	}
	ns := evalFiles(t, rt, loop, `import fs from "noe:fs";
import {open} from "noe:fs";
const code=p=>p.then(()=>"resolved",e=>e.code);
const invalidModes=[-1,0o1000,1.5,NaN,Infinity,"600"];
const modeErrors=await Promise.all(invalidModes.map((mode,n)=>code(open("invalid-mode-"+n,{flags:"w",mode}))));
if(modeErrors.some(c=>c!=="ERR_INVALID_ARG"))throw Error("invalid modes "+JSON.stringify(modeErrors));

const trunc=await open("existing",{flags:"w"});
if((await trunc.stat()).size!==0)throw Error("w did not truncate");
const truncateReadDenied=await trunc.read(1).then(()=>false,e=>typeof e.code==="string"&&typeof e.errno==="number"&&e.errno>0);
await trunc.write("W");await trunc.close();
if(!truncateReadDenied)throw Error("w was not write-only");
const plus=await open("existing",{flags:"w+"});
if((await plus.stat()).size!==0)throw Error("w+ did not truncate");
await plus.write("X");
const plusRead=await plus.readAt(0,1);
if(plusRead[0]!==88)throw Error("w+ is not readable and writable");
await plus.close();

const append=await open("existing",{flags:"a"});
const appendReadDenied=await append.read(1).then(()=>false,e=>typeof e.code==="string"&&typeof e.errno==="number"&&e.errno>0);
 await append.writeAt(0,"A");await append.close();
if(!appendReadDenied||fs.readFileSync("existing","utf8")!=="XA")throw Error("a access or append semantics");
const appendPlus=await open("existing",{flags:"a+"});
const appendPlusRead=await appendPlus.read(2);
 await appendPlus.writeAt(0,"B");await appendPlus.close();
if(new TextDecoder().decode(appendPlusRead)!=="XA"||fs.readFileSync("existing","utf8")!=="XAB")throw Error("a+ access or append semantics");

const exclusive=await open("exclusive",{flags:"wx",mode:0o600});
const exclusiveMode=(await exclusive.stat()).mode;
if(permissionsAvailable&&((exclusiveMode&0o777)&~0o600)!==0)throw Error("mode exceeded requested permissions: "+exclusiveMode);
await exclusive.close();
const exclusiveModes=[["wx","wx-file"],["wx+","wx-plus-file"],["ax","ax-file"],["ax+","ax-plus-file"]];
for(const [flags,path] of exclusiveModes){
 const file=await open(path,{flags,mode:0o600});
 if(flags.endsWith("+")){
  await file.writeAt(0,"Q");
  const read=await file.readAt(0,1);
  if(read.length!==1||read[0]!==81)throw Error(flags+" is not readable and writable");
 }
 await file.close();
			const duplicate=await open(path,{flags}).then(()=>null,e=>e);
 if(duplicate?.code!=="EEXIST"||typeof duplicate.errno!=="number"||duplicate.errno<=0)throw Error(flags+" did not reject an existing path");
}
const defaultMode=await open("default-mode",{flags:"wx"});
if(permissionsAvailable&&((((await defaultMode.stat()).mode&0o777)&~0o666)!==0))throw Error("default mode exceeded 0o666");
await defaultMode.close();
const createdAppend=await open("created-by-a",{flags:"a"});await createdAppend.write("a");await createdAppend.close();
const createdAx=await open("created-by-ax",{flags:"ax"});await createdAx.close();
const createdAxPlus=await open("created-by-ax-plus",{flags:"ax+"});await createdAxPlus.writeAt(0,"z");await createdAxPlus.close();
if(fs.readFileSync("created-by-a","utf8")!=="a"||fs.readFileSync("created-by-ax-plus","utf8")!=="z")throw Error("append create flags");
export const report=true;`)
	result, err := ns.Get("report")
	if err != nil || !result.Bool() {
		t.Fatalf("report=%v, error=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "invalid-mode-0")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid mode created a file: %v", err)
	}
}

func TestFileHandleStatUsesDescriptorAndTruncatePreservesCursor(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "original"), []byte("abcdef"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	ns := evalFiles(t, rt, loop, `import fs from "noe:fs";
import {open,rename,stat} from "noe:fs";
const file=await open("original",{flags:"r+"});
try{
 await rename("original","moved");
 fs.writeFileSync("original","replacement is larger than the opened file");
 const descriptor=await file.stat();const pathNow=await stat("original");
 if(!descriptor.isFile()||descriptor.size!==6||pathNow.size<=descriptor.size)throw Error("stat followed the replaced path");
 const first=await file.read(2);
 if(new TextDecoder().decode(first)!=="ab")throw Error("initial cursor");
 await file.truncate(3);
 if((await file.stat()).size!==3)throw Error("truncate did not shrink");
 const afterShrink=await file.read(2);
 if(afterShrink.length!==1||afterShrink[0]!==99)throw Error("truncate moved the cursor");
 await file.truncate(7);
 const afterExtend=await file.read(4);
 if(afterExtend.length!==4||afterExtend.some(byte=>byte!==0))throw Error("truncate did not extend at the current cursor");
 if((await file.stat()).size!==7)throw Error("extended stat size");
 const negative=await file.truncate(-1).then(()=>false,error=>error.code==="ERR_INVALID_ARG");
 if(!negative||(await file.stat()).size!==7)throw Error("negative truncate changed the file");
 await file.truncate(5);await file.truncate();
 if((await file.stat()).size!==0)throw Error("default truncate length is not zero");
 await file.sync();
}finally{await file.close()}
export const report=true;`)
	result, err := ns.Get("report")
	if err != nil || !result.Bool() {
		t.Fatalf("report=%v, error=%v", result, err)
	}
	data, err := os.ReadFile(filepath.Join(cwd, "original"))
	if err != nil || string(data) != "replacement is larger than the opened file" {
		t.Fatalf("replacement bytes=%q, error=%v", data, err)
	}
}

func TestFileHandleRuntimeShutdownInterruptsGracefulClose(t *testing.T) {
	rt, h, _ := fileRuntime(t, "")
	backend := &fakeFileBackend{readData: []byte{42}, readGate: make(chan struct{}), readStarted: make(chan struct{})}
	registerFakeFile(t, rt, h, backend)
	if _, err := rt.Eval(`fakeFile.read(1)`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-backend.readStarted:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	if _, err := rt.Eval(`fakeFile.close()`); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- rt.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		backend.releaseRead()
		t.Fatal("shutdown waited for JS delivery or graceful close")
	}
	if backend.closeCount.Load() != 1 {
		t.Fatalf("close count=%d", backend.closeCount.Load())
	}
}

func TestFileHandleExecutionScopeCleanupKeepsBorrowedRuntime(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	scope, err := NewExecutionScope(loop, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	backend, err := filesystem.New(filesystem.Config{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := newFileHost(&fsHost{rt: rt, cfg: &FS{Loop: loop}, backend: backend})
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeFileBackend{}
	f := newFileHandle(h, fake)
	if !h.own(f) {
		t.Fatal("scope rejected handle")
	}
	scope.Close()
	if fake.closeCount.Load() != 1 {
		t.Fatal("scope did not reclaim handle")
	}
	if _, err := rt.Eval(`1+1`); err != nil {
		t.Fatalf("borrowed runtime closed: %v", err)
	}
}

func TestFileHandleCloseWaitsForAdmittedOperations(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	evalFiles(t, rt, loop, `import {open,readFile} from "noe:fs";
const file=await open("file",{flags:"r+"});
const writing=file.write("after!");
const closing=file.close();
const late=file.write("bad").then(()=>false,e=>e.code==="ERR_FILE_CLOSED");
await closing;
if(await writing!==6||!await late||(await readFile("file","utf8"))!=="after!")throw Error("close did not drain admitted write");`)
}

func TestFileHandleWithoutWebAPIsOrLoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt := quickjs.New()
	defer rt.Close()
	if err := Install(rt, Config{NoWebAPIs: true, FS: &FS{}}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rt.SetModuleLoader(func(spec, ref string) (string, string, error) {
		return `import {open} from "noe:fs";
const f=await open(` + strconv.Quote(path) + `,{flags:"r+"});try{const r=await f.read(2);if(r[0]!==98)throw Error("read");await f.writeAt(0,"!");}finally{await f.close()}`, "file-test", nil
	})
	result, err := rt.EvalContext(ctx, `import("file-test")`)
	if err == nil {
		_, err = result.Await(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "!efore" {
		t.Fatalf("bytes=%s, error=%v", data, err)
	}
}
