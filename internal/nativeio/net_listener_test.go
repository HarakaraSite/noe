package nativeio

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// waitNetOperations joins native operation goroutines without closing the Host.
// AsyncWork.Complete posts the Promise callback before marking its work done,
// so an awaited Promise alone is not a barrier for the final Busy check.
func waitNetOperations(t *testing.T, h *Host) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("native socket operations did not finish")
	}
}

func TestNetListenTCPAddressAcceptAndIndependentClose(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen,connect}=await import("noe:sockets");
  const listener=await listen({network:"tcp",host:"127.0.0.1",port:0});
  if(listener.address.network!=="tcp"||listener.address.host!=="127.0.0.1"||listener.address.port<1)
    throw Error("bad bound address: "+JSON.stringify(listener.address));
  const accepted=listener.accept();
  const client=await connect({network:"tcp",host:listener.address.host,port:listener.address.port});
  const server=await accepted;
  await listener.close();
  const request=server.readable.getReader();
  const writer=client.writable.getWriter();
  await writer.write(new TextEncoder().encode("request"));await writer.close();
  const first=await request.read(),end=await request.read();
  if(new TextDecoder().decode(first.value)!=="request"||!end.done)throw Error("server did not receive request and FIN");
  const reply=client.readable.getReader();
  const serverWriter=server.writable.getWriter();
  await serverWriter.write(new TextEncoder().encode("reply"));await serverWriter.close();
  const received=await reply.read(),replyEnd=await reply.read();
  if(new TextDecoder().decode(received.value)!=="reply"||!replyEnd.done)throw Error("accepted socket closed with listener");
  await Promise.all([client.close(),server.close()]);
  return JSON.stringify(listener.address);
})()`))
	if got := result.String(); !strings.Contains(got, `"network":"tcp"`) || !strings.Contains(got, `"port":`) {
		t.Fatalf("listener address = %q", got)
	}
	waitNetOperations(t, h)
	if rt.Busy() {
		t.Fatalf("closed listener and accepted sockets retained runtime work: busy=%v", rt.Busy())
	}
}

func TestNetListenMultiplePendingAcceptsEachGetOneConnection(t *testing.T) {
	rt, _ := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen,connect}=await import("noe:sockets");
  const listener=await listen({network:"tcp",host:"127.0.0.1",port:0});
  const accepting=[listener.accept(),listener.accept()];
  const clients=await Promise.all([0,1].map(()=>connect({network:"tcp",host:listener.address.host,port:listener.address.port})));
  const servers=await Promise.all(accepting);
  await listener.close();
  const values=await Promise.all(clients.map(async(client,i)=>{
    const writer=client.writable.getWriter();await writer.write(new Uint8Array([i+1]));await writer.close();
    const reader=servers[i].readable.getReader();const got=await reader.read(),end=await reader.read();
    if(got.value.length!==1||!end.done)throw Error("accept did not receive one connection");
    return got.value[0];
  }));
  await Promise.all([...clients,...servers].map(c=>c.close()));
  return values.sort().join(",");
})()`))
	if got := result.String(); got != "1,2" {
		t.Fatalf("two pending accepts received = %q", got)
	}
}

func TestNetListenCloseRejectsPendingAcceptAndReleasesKeepalive(t *testing.T) {
	rt, _ := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  const listener=await listen({network:"tcp",host:"127.0.0.1",port:0});
  globalThis.pendingAccept=listener.accept().then(()=>"resolved",()=>"rejected");
  const closing=listener.close();
  const afterClose=listener.accept().then(()=>"resolved",()=>"rejected");
  await closing;
  return [await pendingAccept,await afterClose].join(",");
})()`))
	if got := result.String(); got != "rejected,rejected" {
		t.Fatalf("pending accept after close = %q", got)
	}
	if rt.Busy() {
		t.Fatalf("closed listener retained runtime work: busy=%v", rt.Busy())
	}
}

func TestNetListenRejectsAcceptedSocketIfClosePrecedesOwnerDelivery(t *testing.T) {
	rt, h := netRuntime(t, t.TempDir(), nil)
	promise := rt.NewPromise()
	local, peer := net.Pipe()
	defer peer.Close()
	accepted := &trackedCloseConn{Conn: local, closed: make(chan struct{})}
	listener := &netListener{h: h}
	work := rt.StartAsyncWork()
	work.Post(func(closed error) {
		if closed != nil {
			_ = accepted.Close()
			return
		}
		listener.completeAccept(promise, accepted, nil)
	})
	work.Done()
	// The host completion is queued but not run until RunJobs. Closing here
	// covers an accepted connection that hasn't yet transferred to JS.
	listener.mu.Lock()
	listener.closed = true
	listener.mu.Unlock()
	if err := rt.RunJobs(); err != nil {
		t.Fatal(err)
	}
	if _, err := promise.Value().Await(t.Context()); err == nil || !strings.Contains(strings.ToLower(err.Error()), "closed") {
		t.Fatalf("queued accept after listener close = %v, want closed rejection", err)
	}
	select {
	case <-accepted.closed:
	case <-time.After(time.Second):
		t.Fatal("accepted socket queued before close was not disposed")
	}
}

func TestNetListenUnixCleanupAndExistingPathPreservation(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	dir := t.TempDir()
	rt, _ := netRuntime(t, dir, nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  const listener=await listen({network:"unix",path:"owned.sock"});
  const path=listener.address.path;
  await listener.close();
  return path;
})()`))
	path := filepath.Join(dir, "owned.sock")
	if got := result.String(); got != path {
		t.Fatalf("Unix bound path = %q, want %q", got, path)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("normally closed listener left Unix path: %v", err)
	}

	occupied := filepath.Join(dir, "occupied.sock")
	if err := os.WriteFile(occupied, []byte("keep existing file"), 0600); err != nil {
		t.Fatal(err)
	}
	result = awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  try { await listen({network:"unix",path:"occupied.sock"}); return "resolved"; }
  catch { return "rejected"; }
})()`))
	if got := result.String(); got != "rejected" {
		t.Fatalf("listen over existing pathname = %q", got)
	}
	if data, err := os.ReadFile(occupied); err != nil || string(data) != "keep existing file" {
		t.Fatalf("existing pathname changed: data=%q err=%v", data, err)
	}
	if rt.Busy() {
		t.Fatalf("Unix listener paths retained runtime work: busy=%v", rt.Busy())
	}
}

func TestBoundUnixListenerPreservesReplacementPath(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	path := filepath.Join(t.TempDir(), "replace.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := newBoundNetListener(listener, "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := bound.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "replacement" {
		t.Fatalf("replacement path was not preserved: data=%q err=%v", data, err)
	}
}

func TestNetListenUnixPrePublishReplacementSurvives(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	for _, replacement := range []string{"file", "socket"} {
		t.Run(replacement, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "public.sock")
			var replacementListener net.Listener
			listen := func(ctx context.Context, network, address string) (net.Listener, error) {
				bound, err := (&net.ListenConfig{}).Listen(ctx, network, address)
				if err != nil {
					return nil, err
				}
				if replacement == "file" {
					err = os.WriteFile(path, []byte("replacement file"), 0600)
				} else {
					replacementListener, err = net.Listen("unix", path)
				}
				if err != nil {
					_ = bound.Close()
					return nil, err
				}
				return bound, nil
			}
			t.Cleanup(func() {
				if replacementListener != nil {
					_ = replacementListener.Close()
				}
			})
			rt, h := netRuntimeWithListen(t, dir, nil, listen)
			result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  try { await listen({network:"unix",path:"public.sock"}); return "resolved"; }
  catch { return "rejected"; }
})()`))
			if got := result.String(); got != "rejected" {
				t.Fatalf("listen raced with replacement and = %q", got)
			}
			before, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("replacement path missing after failed publish: %v", err)
			}
			if replacement == "file" {
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "replacement file" {
					t.Fatalf("replacement file changed: data=%q err=%v", data, err)
				}
			} else {
				accepted := make(chan net.Conn, 1)
				acceptErr := make(chan error, 1)
				go func() {
					conn, err := replacementListener.Accept()
					if err != nil {
						acceptErr <- err
						return
					}
					accepted <- conn
				}()
				client, err := net.DialTimeout("unix", path, time.Second)
				if err != nil {
					t.Fatalf("replacement socket stopped accepting: %v", err)
				}
				defer client.Close()
				select {
				case conn := <-accepted:
					defer conn.Close()
				case err := <-acceptErr:
					t.Fatalf("replacement socket accept: %v", err)
				case <-time.After(time.Second):
					t.Fatal("replacement socket did not accept connection")
				}
			}
			after, err := os.Lstat(path)
			if err != nil || !os.SameFile(before, after) {
				t.Fatalf("replacement identity changed: before=%v after=%v err=%v", before, after, err)
			}
			waitNetOperations(t, h)
			if rt.Busy() {
				t.Fatalf("failed bind retained runtime work: busy=%v", rt.Busy())
			}
		})
	}
}

func TestBoundUnixListenerPreservesReplacementSocket(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	path := filepath.Join(t.TempDir(), "replace.sock")
	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		return (&net.ListenConfig{}).Listen(ctx, network, address)
	}
	bound, err := listenUnixSocket(context.Background(), path, listen)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	replacement, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer replacement.Close()
	before, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := bound.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := os.Lstat(path)
	if err != nil || !os.SameFile(before, after) {
		t.Fatalf("replacement socket path was removed or changed: before=%v after=%v err=%v", before, after, err)
	}
}

func TestNetListenUnixLongPathUsesShortPrivateAlias(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	base, err := os.MkdirTemp("/tmp", "n-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	maxPath := len(syscall.RawSockaddrUnix{}.Path) - 1
	componentLength := maxPath - len(base) - 3
	if componentLength < 1 {
		t.Skipf("temporary root too long to exercise maximum Unix path: base=%d max=%d", len(base), maxPath)
	}
	dir := filepath.Join(base, strings.Repeat("d", componentLength))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "s")
	stagePath := filepath.Join(dir, ".noe-net-123456789012", "s")
	if len(path) != maxPath || len(stagePath) <= maxPath {
		t.Skipf("host paths do not exercise Unix sun_path fallback: public=%d stage=%d max=%d", len(path), len(stagePath), maxPath)
	}
	rt, _ := netRuntime(t, dir, nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen,connect}=await import("noe:sockets");
  const listener=await listen({network:"unix",path:"s"});
  const accepted=listener.accept();
  const client=await connect({network:"unix",path:listener.address.path});
  const server=await accepted;
  await listener.close();
  await Promise.all([client.close(),server.close()]);
  return listener.address.path;
})()`))
	if got := result.String(); got != path {
		t.Fatalf("published long Unix address = %q, want %q", got, path)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("long-path listener left public pathname: %v", err)
	}
	if rt.Busy() {
		t.Fatalf("long-path listener retained work: busy=%v", rt.Busy())
	}
}

func TestNetListenUnixRejectsPathLongerThanSockaddrCapacity(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	base := t.TempDir()
	maxPath := len(syscall.RawSockaddrUnix{}.Path) - 1
	componentLength := maxPath - len(base) - 3
	if componentLength < 1 {
		t.Skipf("temporary root too long to exercise Unix path limit: base=%d max=%d", len(base), maxPath)
	}
	dir := filepath.Join(base, strings.Repeat("d", componentLength))
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	tooLongPath := filepath.Join(dir, "ss")
	if len(tooLongPath) != maxPath+1 {
		t.Skipf("constructed path length %d, want %d", len(tooLongPath), maxPath+1)
	}
	rt, _ := netRuntime(t, dir, nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  try { await listen({network:"unix",path:"ss"}); return "resolved"; }
  catch(error){return error.message.includes("at most")?"rejected":"wrong error: "+error.message}
})()`))
	if got := result.String(); got != "rejected" {
		t.Fatalf("overlong Unix path result = %q", got)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("overlong path left staging entries: entries=%v err=%v", entries, err)
	}
	if rt.Busy() {
		t.Fatalf("overlong path retained work: busy=%v", rt.Busy())
	}
}

func TestNetListenValidationAndPreAbortedSignal(t *testing.T) {
	rt, _ := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  const invalid=[undefined,null,[],{network:"tcp",host:"",port:0},{network:"tcp",host:"127.0.0.1",port:-1},{network:"tcp",host:"127.0.0.1",port:65536},{network:"unix",path:""},{network:"other"}];
  for(const options of invalid){try{await listen(options);return "unexpected success"}catch{}}
  const controller=new AbortController(),reason=new Error("do not bind");controller.abort(reason);
  try{await listen({network:"tcp",host:"127.0.0.1",port:0,signal:controller.signal});return "missed abort"}
  catch(error){return error===reason?"rejected":"wrong reason"}
})()`))
	if got := result.String(); got != "rejected" {
		t.Fatalf("listen validation/pre-abort result = %q", got)
	}
	if rt.Busy() {
		t.Fatalf("invalid listen started native work: busy=%v", rt.Busy())
	}
}

type gatedNetListener struct {
	closed chan struct{}
	once   sync.Once
	addr   net.Addr
}

func (l *gatedNetListener) Accept() (net.Conn, error) { return nil, net.ErrClosed }
func (l *gatedNetListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return nil
}
func (l *gatedNetListener) Addr() net.Addr { return l.addr }

func TestNetListenAbortClosesLateListenerAndRuntimeWaits(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	returnListener := make(chan struct{})
	listener := &gatedNetListener{closed: make(chan struct{}), addr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 43210}}
	listen := func(ctx context.Context, _, _ string) (net.Listener, error) {
		close(started)
		<-ctx.Done()
		close(canceled)
		<-returnListener
		return listener, nil
	}
	rt, h := netRuntimeWithListen(t, t.TempDir(), nil, listen)
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {listen}=await import("noe:sockets");
  globalThis.listenController=new AbortController();
  globalThis.listenReason=new Error("cancel pending listen");
  globalThis.pendingListen=listen({network:"tcp",host:"127.0.0.1",port:0,signal:listenController.signal})
    .then(()=>"unexpected success",error=>error===listenReason?"same abort reason":"wrong abort reason");
  return true;
})()`))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("listen did not reach its native start barrier")
	}
	if _, err := rt.Eval(`listenController.abort(listenReason)`); err != nil {
		t.Fatal(err)
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("AbortSignal did not cancel the pending listener")
	}

	closeDone := make(chan error, 1)
	go func() { closeDone <- rt.Close() }()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-closeDone:
			t.Fatalf("runtime close returned before late listener disposal: %v", err)
		case <-deadline:
			t.Fatal("runtime close did not begin")
		default:
		}
		h.mu.Lock()
		closed := h.closed
		h.mu.Unlock()
		if closed {
			break
		}
		runtime.Gosched()
	}
	select {
	case err := <-closeDone:
		t.Fatalf("runtime close returned before releasing listener gate: %v", err)
	default:
	}
	close(returnListener)
	select {
	case err := <-closeDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime close did not wait for late listener cleanup")
	}
	select {
	case <-listener.closed:
	case <-time.After(time.Second):
		t.Fatal("late listener was not disposed")
	}
}

func TestBoundUnixListenerCloseIsIdempotent(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("Unix sockets are unavailable on Windows")
	}
	path := filepath.Join(t.TempDir(), "once.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	bound, err := newBoundNetListener(listener, "unix", path)
	if err != nil {
		t.Fatal(err)
	}
	first := make(chan error, 1)
	second := make(chan error, 1)
	go func() { first <- bound.Close() }()
	go func() { second <- bound.Close() }()
	for _, result := range []<-chan error{first, second} {
		select {
		case err := <-result:
			if err != nil && !errors.Is(err, net.ErrClosed) {
				t.Fatal(err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("concurrent listener close did not settle")
		}
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("closed listener left pathname: %v", err)
	}
}
