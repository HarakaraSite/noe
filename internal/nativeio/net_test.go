package nativeio

import (
	"context"
	"errors"
	"io"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
	"noe/internal/stdlib"
)

func netRuntime(t *testing.T, cwd string, dial netDialFunc) (*quickjs.Runtime, *Host) {
	return netRuntimeWithListen(t, cwd, dial, nil)
}

func netRuntimeWithListen(t *testing.T, cwd string, dial netDialFunc, listen netListenFunc) (*quickjs.Runtime, *Host) {
	t.Helper()
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	if err := stdlib.Install(rt, stdlib.Config{Stdout: io.Discard, Stderr: io.Discard, Loop: loop}); err != nil {
		_ = rt.Close()
		t.Fatal(err)
	}
	rt.OnClose(loop.Close)
	ctx, cancel := context.WithCancel(rt.Context())
	h := &Host{rt: rt, ctx: ctx, cancel: cancel, opts: Options{Cwd: cwd}, resources: make(map[io.Closer]struct{})}
	rt.OnClose(h.Close)
	var err error
	if dial == nil && listen == nil {
		err = h.installNet()
	} else if listen == nil {
		err = h.installNetWithDial(dial)
	} else {
		err = h.installNetWithFuncs(dial, listen)
	}
	if err != nil {
		_ = rt.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rt.Close() })
	return rt, h
}

func evalNet(t *testing.T, rt *quickjs.Runtime, src string) quickjs.Value {
	t.Helper()
	v, err := rt.Eval(src)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func awaitNet(t *testing.T, v quickjs.Value) quickjs.Value {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := v.Await(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

type socketFixture struct {
	listener net.Listener
	options  string
	path     string
}

func listenSocket(t *testing.T, network string) socketFixture {
	t.Helper()
	if network == "unix" {
		if runtime.GOOS == "windows" {
			t.Skip("Unix sockets are unavailable on Windows")
		}
		path := filepath.Join(t.TempDir(), "net.sock")
		listener, err := net.Listen("unix", path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		return socketFixture{listener: listener, path: path, options: `{network:"unix",path:"net.sock"}`}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	addr := listener.Addr().(*net.TCPAddr)
	return socketFixture{listener: listener, options: `{network:"tcp",host:"127.0.0.1",port:` + strconv.Itoa(addr.Port) + `}`}
}

func TestNetConnectTCPAndUnixHalfClose(t *testing.T) {
	for _, network := range []string{"tcp", "unix"} {
		t.Run(network, func(t *testing.T) {
			fixture := listenSocket(t, network)
			serverResult := make(chan error, 1)
			go func() {
				peer, err := fixture.listener.Accept()
				if err != nil {
					serverResult <- err
					return
				}
				defer peer.Close()
				request, err := io.ReadAll(peer)
				if err != nil {
					serverResult <- err
					return
				}
				if string(request) != string([]byte{0, 255, 'x'}) {
					serverResult <- errors.New("request bytes changed")
					return
				}
				_, err = peer.Write([]byte{255, 0, 'o', 'k'})
				serverResult <- err
			}()

			rt, host := netRuntime(t, filepath.Dir(fixture.path), nil)
			result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  const c=await connect(`+fixture.options+`);
  try {
    const writer=c.writable.getWriter();
    await writer.write(new Uint8Array([0,255,120]));
    await writer.close();
    const reader=c.readable.getReader();
    const chunks=[];let size=0;
    for(;;){const r=await reader.read();if(r.done)break;chunks.push(r.value);size+=r.value.length;}
    const all=new Uint8Array(size);let at=0;for(const b of chunks){all.set(b,at);at+=b.length;}
    return Array.from(all).join(",");
  } finally { await c.close(); }
})()`))
			if got, want := result.String(), "255,0,111,107"; got != want {
				t.Fatalf("response bytes = %q, want %q", got, want)
			}
			select {
			case err := <-serverResult:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("server did not observe client FIN and return its response")
			}
			// AsyncWork.Complete can deliver the result before its producer calls
			// Done. Join the native producers before checking for retained work.
			host.wg.Wait()
			if rt.Busy() {
				t.Fatalf("closed connection retained runtime work: busy=%v", rt.Busy())
			}
		})
	}
}

func TestNetConnectValidationAndAbort(t *testing.T) {
	started := make(chan struct{})
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	rt, _ := netRuntime(t, t.TempDir(), dial)
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  const invalid=[undefined,null,[],{network:"tcp",host:"",port:1},{network:"tcp",host:"127.0.0.1",port:0},{network:"unix",path:""},{network:"other"}];
  for(const options of invalid){try{await connect(options);return "unexpected success"}catch(e){}}
  const controller=new AbortController(), reason=new Error("cancel dial");
  controller.abort(reason);
  try{await connect({network:"tcp",host:"127.0.0.1",port:1,signal:controller.signal});return "missed pre-abort"}
  catch(e){if(e!==reason)return "wrong pre-abort reason";}
  globalThis.dialController=new AbortController();
  globalThis.dialReason=new Error("stop pending dial");
  globalThis.pendingConnect=connect({network:"tcp",host:"127.0.0.1",port:1,signal:dialController.signal})
    .then(()=>"unexpected dial success",e=>e===dialReason?"same abort reason":"wrong pending abort reason");
  return "started";
})()`))
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("dial did not reach its native start barrier")
	}
	// The script's pending connect starts the injected native operation. Abort
	// only after that barrier so this checks cancellation of an in-flight dial.
	if _, err := rt.Eval(`dialController.abort(dialReason)`); err != nil {
		t.Fatal(err)
	}
	if got := awaitNet(t, mustGetNet(t, rt, "pendingConnect")).String(); got != "same abort reason" {
		t.Fatalf("pending dial abort reason = %q", got)
	}
	if rt.Busy() {
		t.Fatalf("aborted dial left active work: %v", rt.Busy())
	}
}

type observedNetConn struct {
	directionalNetConn
	readStarted  chan struct{}
	readDone     chan struct{}
	writeStarted chan struct{}
	writeDone    chan struct{}
	readOnce     sync.Once
	writeOnce    sync.Once
	readEndOnce  sync.Once
	writeEndOnce sync.Once
}

func (c *observedNetConn) Read(p []byte) (int, error) {
	c.readOnce.Do(func() { close(c.readStarted) })
	defer c.readEndOnce.Do(func() { close(c.readDone) })
	return c.directionalNetConn.Read(p)
}

func (c *observedNetConn) Write(p []byte) (int, error) {
	c.writeOnce.Do(func() { close(c.writeStarted) })
	defer c.writeEndOnce.Do(func() { close(c.writeDone) })
	return c.directionalNetConn.Write(p)
}

func observedDial(observed chan<- *observedNetConn) netDialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetWriteBuffer(4096)
		}
		directional, ok := conn.(directionalNetConn)
		if !ok {
			_ = conn.Close()
			return nil, errors.New("dialed connection has no directional shutdown")
		}
		wrapped := &observedNetConn{
			directionalNetConn: directional,
			readStarted:        make(chan struct{}),
			readDone:           make(chan struct{}),
			writeStarted:       make(chan struct{}),
			writeDone:          make(chan struct{}),
		}
		observed <- wrapped
		return wrapped, nil
	}
}

func TestNetReadCancelLeavesWriteUsable(t *testing.T) {
	fixture := listenSocket(t, "tcp")
	accepted := make(chan net.Conn, 1)
	serverResult := make(chan error, 1)
	go func() {
		peer, err := fixture.listener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		accepted <- peer
		data, err := io.ReadAll(peer)
		_ = peer.Close()
		if err == nil && string(data) != "write-after-read-cancel" {
			err = errors.New("write after read cancel was corrupted")
		}
		serverResult <- err
	}()

	observed := make(chan *observedNetConn, 1)
	dial := observedDial(observed)
	// Use a second runtime module install with the observer dialer.
	dir := t.TempDir()
	rt, _ := netRuntime(t, dir, dial)
	startedConn := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  globalThis.connection=await connect(`+fixture.options+`);
  globalThis.reader=connection.readable.getReader();
  globalThis.writer=connection.writable.getWriter();
  globalThis.readResult=reader.read();
  return true;
})()`))
	if !startedConn.Bool() {
		t.Fatal("connection setup did not complete")
	}
	client := <-observed
	select {
	case <-client.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native read did not reach its start channel")
	}
	select {
	case <-client.readDone:
		t.Fatal("read finished before cancellation")
	default:
	}
	if _, err := rt.Eval(`globalThis.cancelResult=reader.cancel()`); err != nil {
		t.Fatal(err)
	}
	awaitNet(t, mustGetNet(t, rt, "cancelResult"))
	readResult := awaitNet(t, mustGetNet(t, rt, "readResult"))
	done, _ := readResult.Get("done")
	if !done.Bool() {
		t.Fatalf("cancelled read result was not done: %v", readResult)
	}
	awaitNet(t, evalNet(t, rt, `writer.write(new TextEncoder().encode("write-after-read-cancel")).then(()=>writer.close())`))
	awaitNet(t, evalNet(t, rt, `(async()=>{await connection.close()})()`))
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive write after read cancel")
	}
	if rt.Busy() {
		t.Fatalf("closed socket retained active reference: %v", rt.Busy())
	}
}

func TestNetBlockedWriteAbortLeavesPendingReadUsable(t *testing.T) {
	fixture := listenSocket(t, "tcp")
	accepted := make(chan net.Conn, 1)
	readPeer := make(chan struct{})
	serverResult := make(chan error, 1)
	go func() {
		peer, err := fixture.listener.Accept()
		if err != nil {
			serverResult <- err
			return
		}
		accepted <- peer
		<-readPeer
		_, err = io.Copy(io.Discard, peer)
		if err == nil {
			_, err = peer.Write([]byte("reply-after-write-abort"))
		}
		_ = peer.Close()
		serverResult <- err
	}()

	observed := make(chan *observedNetConn, 1)
	rt, _ := netRuntime(t, t.TempDir(), observedDial(observed))
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  globalThis.connection=await connect(`+fixture.options+`);
  globalThis.reader=connection.readable.getReader();
  globalThis.writer=connection.writable.getWriter();
  globalThis.readSettled=false;globalThis.writeSettled=false;
  globalThis.pendingRead=reader.read().then(v=>{readSettled=true;return v},e=>{readSettled=true;throw e});
  globalThis.pendingWrite=writer.write(new Uint8Array(32*1024*1024)).then(v=>{writeSettled=true;return v},e=>{writeSettled=true;throw e});
  pendingRead.catch(()=>{});pendingWrite.catch(()=>{});
  return true;
})()`))
	client := <-observed
	select {
	case <-accepted:
	case <-time.After(2 * time.Second):
		t.Fatal("server did not accept connection")
	}
	select {
	case <-client.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native read did not reach its start channel")
	}
	select {
	case <-client.writeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native write did not reach its start channel")
	}
	select {
	case <-client.readDone:
		t.Fatal("read completed before write abort")
	default:
	}
	select {
	case <-client.writeDone:
		t.Fatal("large write completed before abort; peer is intentionally not reading")
	default:
	}
	if _, err := rt.Eval(`globalThis.abortPromise=writer.abort(new Error("stop write"))`); err != nil {
		t.Fatal(err)
	}
	awaitNet(t, mustGetNet(t, rt, "abortPromise"))
	statuses := evalNet(t, rt, `[readSettled,writeSettled].join(",")`)
	if got := statuses.String(); got != "false,true" {
		t.Fatalf("after write abort read/write settled = %q", got)
	}
	close(readPeer)
	readResult := awaitNet(t, mustGetNet(t, rt, "pendingRead"))
	value, _ := readResult.Get("value")
	bytes, ok := value.Bytes()
	if !ok || string(bytes) != "reply-after-write-abort" {
		t.Fatalf("pending read after write abort = %v, bytes=%q", value, string(bytes))
	}
	awaitNet(t, evalNet(t, rt, `(async()=>{await connection.close()})()`))
	select {
	case err := <-serverResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not observe write abort and return response")
	}
}

func TestNetCloseSettlesPendingStreamsAndReleasesKeepalive(t *testing.T) {
	fixture := listenSocket(t, "tcp")
	accepted := make(chan net.Conn, 1)
	go func() {
		peer, err := fixture.listener.Accept()
		if err == nil {
			accepted <- peer
		}
	}()
	observed := make(chan *observedNetConn, 1)
	rt, _ := netRuntime(t, t.TempDir(), observedDial(observed))
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  globalThis.connection=await connect(`+fixture.options+`);
  globalThis.reader=connection.readable.getReader();
  globalThis.writer=connection.writable.getWriter();
  globalThis.readStarted=false;globalThis.writeStarted=false;
  globalThis.pendingRead=reader.read().then(v=>{readStarted=true;return v},e=>{readStarted=true;return {error:e.message}});
  globalThis.pendingWrite=writer.write(new Uint8Array(32*1024*1024)).then(()=>({ok:true}),e=>({error:e.message}));
  return true;
})()`))
	client := <-observed
	peer := <-accepted
	defer peer.Close()
	if !rt.Busy() {
		t.Fatal("open connection did not keep the runtime alive")
	}
	for name, ch := range map[string]<-chan struct{}{"read": client.readStarted, "write": client.writeStarted} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("native %s did not start", name)
		}
	}
	if _, err := rt.Eval(`globalThis.closePromise=connection.close()`); err != nil {
		t.Fatal(err)
	}
	awaitNet(t, mustGetNet(t, rt, "closePromise"))
	awaitNet(t, mustGetNet(t, rt, "pendingRead"))
	awaitNet(t, mustGetNet(t, rt, "pendingWrite"))
	flags := evalNet(t, rt, `Promise.all([reader.closed.then(()=>"read-resolved",()=>"read-rejected"),writer.closed.then(()=>"write-resolved",()=>"write-rejected")]).then(x=>x.join(","))`)
	if got := awaitNet(t, flags).String(); got != "read-rejected,write-rejected" {
		t.Fatalf("stream closed results = %q", got)
	}
	if rt.Busy() {
		t.Fatalf("full close did not release runtime keepalive: busy=%v", rt.Busy())
	}
}

func mustGetNet(t *testing.T, rt *quickjs.Runtime, name string) quickjs.Value {
	t.Helper()
	v, err := rt.Get(name)
	if err != nil {
		t.Fatalf("get %s: %v", name, err)
	}
	return v
}

func TestNetStartSignalStopsOnlyPendingDial(t *testing.T) {
	fixture := listenSocket(t, "tcp")
	peerCh := make(chan net.Conn, 1)
	go func() {
		peer, err := fixture.listener.Accept()
		if err == nil {
			peerCh <- peer
		}
	}()
	rt, _ := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  const controller=new AbortController();
  globalThis.connection=await connect({...`+fixture.options+`,signal:controller.signal});
  controller.abort(new Error("too late"));
  const writer=connection.writable.getWriter();
  await writer.write(new Uint8Array([42]));
  await writer.close();
  await connection.close();
  return "connected";
})()`))
	if got := result.String(); got != "connected" {
		t.Fatalf("post-connect abort result = %q", got)
	}
	peer := <-peerCh
	defer peer.Close()
	got, err := io.ReadAll(peer)
	if err != nil || string(got) != string([]byte{42}) {
		t.Fatalf("peer data after late abort = %v, err=%v", got, err)
	}
	if rt.Busy() {
		t.Fatalf("closed connection retained active work: %v", rt.Busy())
	}
}

func TestNetModuleInputRejectsNUL(t *testing.T) {
	rt, _ := netRuntime(t, t.TempDir(), nil)
	result := awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
	try { await connect({network:"unix",path:"bad\u0000path"}); return "resolved"; }
  catch(e) { return e instanceof TypeError && e.message.includes("NUL") ? "rejected" : e.message; }
})()`))
	if got := result.String(); got != "rejected" {
		t.Fatalf("NUL path result = %q", got)
	}
	if rt.Busy() {
		t.Fatalf("invalid input started native work: %v", rt.Busy())
	}
}

func TestNetRuntimeCloseReclaimsPendingRead(t *testing.T) {
	fixture := listenSocket(t, "tcp")
	peerCh := make(chan net.Conn, 1)
	go func() {
		peer, err := fixture.listener.Accept()
		if err == nil {
			peerCh <- peer
		}
	}()
	observed := make(chan *observedNetConn, 1)
	rt, _ := netRuntime(t, t.TempDir(), observedDial(observed))
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  globalThis.connection=await connect(`+fixture.options+`);
  globalThis.pendingRead=connection.readable.getReader().read();
  pendingRead.catch(()=>{});
  return true;
})()`))
	client := <-observed
	peer := <-peerCh
	defer peer.Close()
	select {
	case <-client.readStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native read did not reach its start channel")
	}
	if err := rt.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-client.readDone:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime close did not finish native read")
	}
	if rt.Busy() {
		t.Fatalf("runtime close retained native reference: %v", rt.Busy())
	}
}

type trackedCloseConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *trackedCloseConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}

func TestNetRuntimeCloseWaitsForLateDialCleanup(t *testing.T) {
	dialStarted := make(chan struct{})
	returnDial := make(chan struct{})
	closedConn := make(chan struct{})
	var releaseOnce sync.Once
	releaseDial := func() { releaseOnce.Do(func() { close(returnDial) }) }
	defer releaseDial()
	dial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		close(dialStarted)
		<-ctx.Done()
		<-returnDial
		client, peer := net.Pipe()
		_ = peer.Close()
		return &trackedCloseConn{Conn: client, closed: closedConn}, nil
	}
	rt, h := netRuntime(t, t.TempDir(), dial)
	awaitNet(t, evalNet(t, rt, `(async()=>{
  const {connect}=await import("noe:sockets");
  globalThis.controller=new AbortController();
  globalThis.pending=connect({network:"tcp",host:"127.0.0.1",port:1,signal:controller.signal}).catch(()=>null);
  return true;
})()`))
	select {
	case <-dialStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("native dial did not reach its start channel")
	}
	if _, err := rt.Eval(`controller.abort(new Error("cancel pending dial"))`); err != nil {
		t.Fatal(err)
	}
	closedRuntime := make(chan error, 1)
	go func() { closedRuntime <- rt.Close() }()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case err := <-closedRuntime:
			t.Fatalf("runtime close returned before late dial disposal: %v", err)
		case <-deadline:
			t.Fatal("runtime close did not begin")
		default:
		}
		h.mu.Lock()
		hostClosed := h.closed
		h.mu.Unlock()
		if hostClosed {
			break
		}
		runtime.Gosched()
	}
	select {
	case err := <-closedRuntime:
		t.Fatalf("runtime close returned before releasing dial gate: %v", err)
	default:
	}
	releaseDial()
	select {
	case err := <-closedRuntime:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime close did not wait for Go dial cleanup")
	}
	select {
	case <-closedConn:
	default:
		t.Fatal("late successful socket was not closed before runtime close returned")
	}
}
