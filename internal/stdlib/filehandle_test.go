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
	ns := evalFiles(t, rt, loop, `export const file=await openOwnedFile(`+strconv.Quote(path)+`,"r");`)
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
	rt, h, loop := fileRuntime(t, "")
	backend := &fakeFileBackend{
		readData: []byte{0, 255, 12},
		readErr:  errors.New("read stopped after bytes"),
		writeN:   2,
		writeErr: errors.New("write stopped after bytes"),
	}
	file := registerFakeFile(t, rt, h, backend)
	ns := evalFiles(t, rt, loop, `const read=await globalThis.fakeFile.read(new Uint8Array(6),0,6,7).then(() => null, e => ({message:e.message,code:e.code,bytesRead:e.bytesRead,data:Array.from(e.buffer.subarray(0,e.bytesRead))}));
const input=new Uint8Array([1,2,3,4]);
const writing=globalThis.fakeFile.write(input,0,input.length,11);
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
	ns := evalFiles(t, rt, loop, `const zero=await fakeFile.read(new Uint8Array(0));
const large=await nativeFile.read(0,Number.MAX_SAFE_INTEGER).then(()=>null,e=>({code:e.code,message:e.message}));
await fakeFile.close();export const report=JSON.stringify({zeroCount:zero.bytesRead,large});`)
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
	ns := evalFiles(t, rt, loop, `const pending=globalThis.fakeFile.read(new Uint8Array(1),0,1,0).then(() => "resolved", e => e.code);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
const closing=globalThis.fakeFile.close();
await new Promise(resolve=>setTimeout(()=>{if(fakeCloseCount()!==0)throw Error("graceful close interrupted read");releaseFakeRead();resolve();},5));
await closing;
const closedAfterRead=fakeReadFinished();
const readResult=await pending;
await globalThis.fakeFile.close();
const lateRead=await globalThis.fakeFile.read(new Uint8Array(1),0,1,0).then(() => "resolved", e => e.code);
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
const read=await globalThis.fakeFile.read(new Uint8Array(1),0,1,0).then(() => "resolved", e => e.code);
const write=await globalThis.fakeFile.write(new Uint8Array([1]),0,1,0).then(() => "resolved", e => e.code);
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
	ns := evalFiles(t, rt, loop, `const pending=globalThis.fakeFile.read(new Uint8Array(1),0,1,0);
async function waitForRead(){if(!fakeReadStarted()){await new Promise(r=>setTimeout(r,0));return waitForRead();}}
await waitForRead();
await new Promise(resolve=>setTimeout(()=>{releaseFakeRead();resolve();},0));
const result=await pending;
await globalThis.fakeFile.close();
export const report=result.bytesRead===1&&result.buffer[0]===7;`)
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

func (f *fakeFileBackend) Read(p []byte) (int, error)  { return f.ReadAt(p, 0) }
func (f *fakeFileBackend) Write(p []byte) (int, error) { return f.WriteAt(p, 0) }

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
	h, err := newFileHost(&fsHost{rt: rt, cfg: &FS{Loop: loop}, backend: backend})
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
	ns := evalFiles(t, rt, loop, `import fs from "node:fs";
import promises,{open} from "node:fs/promises";
import bare,{open as bareOpen} from "fs/promises";
if(open!==promises.open||open!==fs.promises.open||open!==bareOpen||open!==bare.open)throw Error("open aliases differ");
let removed=false;try{await import("noe:fs-handle")}catch{removed=true}
if(!removed)throw Error("retired module still exists");
const f=await open("indexed.bin","r+");
try{
 const first=new Uint8Array(2);const read=await f.read(first);
 if(read.buffer!==first||read.bytesRead!==2||new TextDecoder().decode(first)!=="ab")throw Error("sequential read");
 const shortBuffer=new Uint8Array(4).fill(33);const short=await f.read(shortBuffer,0,4,4);
 if(short.bytesRead!==2||shortBuffer.join()!=="101,102,33,33")throw Error("short read changed unread bytes");
 const storage=new Uint8Array(7).fill(33);const slice=new DataView(storage.buffer,1,5);
 const part=await f.read(slice,{offset:1,length:2,position:4});
 if(part.buffer!==slice||storage.join()!=="33,33,101,102,33,33,33")throw Error("read view boundaries");
 const next=await f.read({length:2});
 if(new TextDecoder().decode(next.buffer.subarray(0,next.bytesRead))!=="cd")throw Error("positional read changed cursor");
 const written=await f.write("XY");
 if(written.buffer!=="XY"||written.bytesWritten!==2)throw Error("string write");
 const writeBacking=new Uint8Array([0,90,81,0]);const writeView=new DataView(writeBacking.buffer,1,2);
 const writing=f.write(writeView,{offset:0,length:1,position:1});
 writeBacking.fill(99);
 const write=await writing;
 if(write.buffer!==writeView||write.bytesWritten!==1)throw Error("write view identity");
 const eof=await f.read(new Uint8Array(2));
 if(eof.bytesRead!==0||"eof" in eof||"readAt" in f||"writeAt" in f)throw Error("new handle surface/cursor");
 await f.close();await f.close();
 const closed=await f.read(new Uint8Array(0)).then(()=>false,e=>e.code==="ERR_FILE_CLOSED");
 if(!closed)throw Error("zero read after close");
}finally{await f.close()}
export const report=fs.readFileSync("indexed.bin","utf8");`)
	report, err := ns.Get("report")
	if err != nil || report.String() != "aZcdXY" {
		t.Fatalf("file=%s, error=%v", report.String(), err)
	}
}

func TestFileHandlePublicValidationAndOpenErrors(t *testing.T) {
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "file"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	rt, _, loop := fileRuntime(t, cwd)
	ns := evalFiles(t, rt, loop, `import {open} from "node:fs/promises";
const code=p=>p.then(()=>"resolved",e=>e.code);
const missing=await code(open("missing"));
if(typeof missing!=="number"||missing===0)throw Error("missing errno");
const invalid=[open(""),open("file",{mode:"r+"}),open("file","w"),open("file",null)];
const f=await open("file","r+");
for(const number of [-1,1.5,NaN,Infinity,Number.MAX_SAFE_INTEGER+1]){
 invalid.push(f.read(new Uint8Array(2),{position:number}),f.read(new Uint8Array(2),{length:number}),f.write(new Uint8Array(2),{position:number}));
}
invalid.push(f.read(new Uint8Array(2),{offset:2,length:1}),f.read({buffer:"bad"}),f.write("x",0,"hex"),f.write("x",NaN),f.write(new ArrayBuffer(2)));
const codes=await Promise.all(invalid.map(code));
if(codes.some(c=>c!=="ERR_INVALID_ARG"))throw Error("validation codes "+JSON.stringify(codes));
await f.close();
const r=await open("file");
const denied=await r.write(new Uint8Array([1])).then(()=>null,e=>({code:e.code,bytesWritten:e.bytesWritten}));
await r.close();if(typeof denied?.code!=="number"||denied.bytesWritten!==0)throw Error("readonly descriptor");
await code(open("must-not-create","r+"));
export const report=true;`)
	result, err := ns.Get("report")
	if err != nil || !result.Bool() {
		t.Fatalf("report=%v, error=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, "must-not-create")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("r+ created file: %v", err)
	}
}

func TestFileHandleRuntimeShutdownInterruptsGracefulClose(t *testing.T) {
	rt, h, _ := fileRuntime(t, "")
	backend := &fakeFileBackend{readData: []byte{42}, readGate: make(chan struct{}), readStarted: make(chan struct{})}
	registerFakeFile(t, rt, h, backend)
	if _, err := rt.Eval(`fakeFile.read(new Uint8Array(1))`); err != nil {
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
	evalFiles(t, rt, loop, `import {open,readFile} from "node:fs/promises";
const file=await open("file","r+");
const writing=file.write("after!",0);
const closing=file.close();
const late=file.write("bad",0).then(()=>false,e=>e.code==="ERR_FILE_CLOSED");
await closing;
if((await writing).bytesWritten!==6||!await late||(await readFile("file","utf8"))!=="after!")throw Error("close did not drain admitted write");`)
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
		return `import {open} from "node:fs/promises";
const f=await open(` + strconv.Quote(path) + `,"r+");try{const r=await f.read(new Uint8Array(2));if(r.buffer[0]!==98)throw Error("read");await f.write("!",0);}finally{await f.close()}`, "file-test", nil
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
