package stdlib

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	quickjs "github.com/go-quickjs/go-quickjs"
)

type receiveRig struct {
	url     string
	notices chan string
	stop    func()
}

// The owner stays on one goroutine. Tests are independent HTTP clients;
// notify is a separate synchronization channel, never part of the upload.
func newReceiveRig(t *testing.T, limit int64, handler string) *receiveRig {
	t.Helper()
	rig := &receiveRig{notices: make(chan string, 64)}
	ready := make(chan string, 1)
	done := make(chan error, 1)
	loops := make(chan *Loop, 1)
	go func() {
		rt := quickjs.New()
		defer rt.Close()
		loop := NewLoop(rt)
		defer loop.Close()
		loops <- loop
		if err := Install(rt, Config{Loop: loop, Stdout: io.Discard, Stderr: io.Discard, Serve: &Serve{MaxBodyBytes: limit, Allow: func(string) error { return nil }}}); err != nil {
			done <- err
			return
		}
		if err := rt.Set("notify", func(message string) { rig.notices <- message }); err != nil {
			done <- err
			return
		}
		source := `const server=serve({port:0,onError(error){notify("error:"+error.message);return new Response("handled",{status:422});}},` + handler + `);`
		if _, err := rt.Eval(source); err != nil {
			done <- err
			return
		}
		v, err := rt.Eval(`server.url`)
		if err != nil {
			done <- err
			return
		}
		ready <- v.String()
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		done <- loop.Run(ctx)
	}()
	loop := <-loops
	var once sync.Once
	rig.stop = func() {
		once.Do(func() {
			loop.Close()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("owner: %v", err)
				}
			case <-time.After(4 * time.Second):
				t.Error("owner/HTTP cleanup did not finish")
			}
		})
	}
	t.Cleanup(rig.stop)
	select {
	case rig.url = <-ready:
	case err := <-done:
		t.Fatalf("setup: %v", err)
	case <-time.After(4 * time.Second):
		t.Fatal("setup timed out")
	}
	return rig
}

func (r *receiveRig) notice(t *testing.T, want string) {
	t.Helper()
	select {
	case got := <-r.notices:
		if got != want {
			t.Fatalf("notice=%q want=%q", got, want)
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("missing notice %q", want)
	}
}

func receiveConn(t *testing.T, rig *receiveRig, headers string) (net.Conn, *bufio.Reader) {
	t.Helper()
	addr := strings.TrimPrefix(rig.url, "http://")
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(6 * time.Second))
	if _, err := fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: %s\r\n%s\r\n", addr, headers); err != nil {
		t.Fatal(err)
	}
	return conn, bufio.NewReader(conn)
}

func receiveResponse(t *testing.T, reader *bufio.Reader) *http.Response {
	t.Helper()
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func receiveText(t *testing.T, res *http.Response) string {
	t.Helper()
	b, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestServeReceivesBeforeEOF(t *testing.T) {
	for _, framing := range []string{"length", "chunked"} {
		t.Run(framing, func(t *testing.T) {
			rig := newReceiveRig(t, 0, `async request=>{notify("dispatch");let bytes=[];for await(const chunk of request.body){bytes.push(...chunk);if(bytes.length===2)notify("prefix");}return Response.json(bytes);}`)
			header := "Content-Length: 7\r\n"
			if framing == "chunked" {
				header = "Transfer-Encoding: chunked\r\n"
			}
			conn, reader := receiveConn(t, rig, header)
			rig.notice(t, "dispatch") // No request body byte has been sent yet.
			if framing == "chunked" {
				fmt.Fprint(conn, "2\r\n")
				conn.Write([]byte{0, 255})
				fmt.Fprint(conn, "\r\n")
			} else {
				conn.Write([]byte{0, 255})
			}
			rig.notice(t, "prefix") // Remaining body and EOF are still gated here.
			if framing == "chunked" {
				fmt.Fprint(conn, "5\r\n")
				conn.Write([]byte{17, 34, 128, 0, 255})
				fmt.Fprint(conn, "\r\n0\r\n\r\n")
			} else {
				conn.Write([]byte{17, 34, 128, 0, 255})
			}
			if got := receiveText(t, receiveResponse(t, reader)); got != "[0,255,17,34,128,0,255]" {
				t.Fatal(got)
			}
		})
	}
}

func TestServeEarlyResponseDoesNotDrainProducer(t *testing.T) {
	cases := []struct {
		name, handler string
		limit         int64
		prefix        string
		status        int
		want          string
	}{
		{"unread", `request=>new Response("accepted")`, 0, "", 200, "accepted"},
		{"partial", `async request=>{await request.body.getReader().read();return new Response("partial");}`, 0, "ab", 200, "partial"},
		{"cap", `async request=>new Response(await request.text())`, 2, "ab", 200, "ab"},
		{"cancel", `async request=>{const reader=request.body.getReader();const pending=reader.read();notify("reading");await reader.cancel();await pending;return new Response("cancelled");}`, 0, "", 200, "cancelled"},
		{"onError", `request=>{throw Error("broken")}`, 0, "", 422, "handled"},
	}
	for _, tc := range cases {
		for _, framing := range []string{"length", "chunked"} {
			t.Run(tc.name+"/"+framing, func(t *testing.T) {
				rig := newReceiveRig(t, tc.limit, tc.handler)
				header := "Content-Length: 20\r\n"
				if framing == "chunked" {
					header = "Transfer-Encoding: chunked\r\n"
				}
				conn, reader := receiveConn(t, rig, header)
				if tc.prefix != "" {
					if framing == "chunked" {
						fmt.Fprintf(conn, "%x\r\n%s\r\n", len(tc.prefix), tc.prefix)
					} else {
						io.WriteString(conn, tc.prefix)
					}
				}
				res := receiveResponse(t, reader)
				if res.StatusCode != tc.status || receiveText(t, res) != tc.want {
					t.Fatalf("unexpected response: %v", res.Status)
				}
				// No producer EOF or remaining bytes have been sent.
				rig.stop()
			})
		}
	}
}

func TestServeExpectInputAndResponseOrdering(t *testing.T) {
	for _, expectation := range []string{"100-continue", "100-continue, 100-continue", "other, 100-CoNtInUe"} {
		for _, kind := range []string{"direct", "hwm0", "hwm1", "cancel"} {
			t.Run(expectation+"/"+kind, func(t *testing.T) {
				handler := `request=>new Response(request.body)`
				if kind == "hwm0" || kind == "hwm1" {
					hwm := "0"
					if kind == "hwm1" {
						hwm = "1"
					}
					handler = `request=>{const reader=request.body.getReader();return new Response(new ReadableStream({async pull(c){notify("pull");const r=await reader.read();if(r.done)c.close();else c.enqueue(r.value);}},{highWaterMark:` + hwm + `}));}`
				}
				if kind == "cancel" {
					handler = `async request=>{const reader=request.body.getReader();const p=reader.read();notify("reading");await reader.cancel();await p;return new Response(new ReadableStream({pull(c){c.enqueue(new TextEncoder().encode("cancelled"));c.close();}},{highWaterMark:0}));}`
				}
				rig := newReceiveRig(t, 0, handler)
				conn, reader := receiveConn(t, rig, "Content-Length: 7\r\nExpect: "+expectation+"\r\n")
				interim := receiveResponse(t, reader)
				if interim.StatusCode != 100 {
					t.Fatalf("interim=%d", interim.StatusCode)
				}
				if kind == "hwm1" {
					rig.notice(t, "pull")
					conn.Write([]byte{0, 255})
				}
				res := receiveResponse(t, reader)
				if kind == "cancel" {
					if got := receiveText(t, res); got != "cancelled" {
						t.Fatal(got)
					}
					rig.stop()
					return
				}
				if kind != "hwm1" {
					conn.Write([]byte{0, 255})
				}
				prefix := make([]byte, 2)
				if _, err := io.ReadFull(res.Body, prefix); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(prefix, []byte{0, 255}) {
					t.Fatalf("prefix=%v", prefix)
				}
				conn.Write([]byte{17, 34, 128, 0, 255})
				rest, err := io.ReadAll(res.Body)
				res.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(rest, []byte{17, 34, 128, 0, 255}) {
					t.Fatalf("rest=%v", rest)
				}
			})
		}
	}
}

type serveReadProbe struct {
	data  []byte
	err   error
	sizes []int
	reads int
}

func (p *serveReadProbe) Read(b []byte) (int, error) {
	p.sizes = append(p.sizes, len(b))
	p.reads++
	n := copy(b, p.data)
	p.data = p.data[n:]
	if len(p.data) == 0 {
		return n, p.err
	}
	return n, nil
}
func (p *serveReadProbe) Close() error { return nil }

func TestServeBodyBoundedReadAndTerminalBytes(t *testing.T) {
	for _, terminal := range []error{io.EOF, errors.New("truncated")} {
		p := &serveReadProbe{data: []byte{0, 255, 17}, err: terminal}
		b := &serveBody{body: p, remaining: 100000}
		data, err := b.read()
		if err != nil || !bytes.Equal(data, []byte{0, 255, 17}) {
			t.Fatalf("bytes=%v error=%v", data, err)
		}
		_, err = b.read()
		if !errors.Is(err, terminal) || p.reads != 1 || p.sizes[0] != serveReadSize {
			t.Fatalf("terminal=%v reads=%d sizes=%v", err, p.reads, p.sizes)
		}
	}
	for _, limit := range []int64{-1, 1, 2, serveReadSize + 1} {
		p := &serveReadProbe{data: bytes.Repeat([]byte{42}, serveReadSize+2), err: io.EOF}
		b := &serveBody{body: p, remaining: limit}
		var got []byte
		for {
			chunk, err := b.read()
			got = append(got, chunk...)
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		want := limit
		if want < 0 {
			want = 0
		}
		if int64(len(got)) != want {
			t.Fatalf("limit=%d got=%d", limit, len(got))
		}
		for _, n := range p.sizes {
			if n > serveReadSize {
				t.Fatalf("oversized read %d", n)
			}
		}
	}
}

func TestServeReceiveBodyMethodsAndOwnership(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{
  const path=new URL(request.url).searchParams.get("mode");
  if(path==="clone"){const copy=request.clone();return new Response((await request.text())+":"+(await copy.text()));}
  if(path==="transfer"){const copy=new Request(request);const text=await copy.text();let rejected=false;try{await request.text()}catch{rejected=true}return new Response(text+":"+rejected);}
  if(path==="json")return Response.json(await request.json());
  if(path==="form")return new Response((await request.formData()).get("key"));
  if(path==="blob")return new Response(await(await request.blob()).text());
  if(path==="array")return new Response(new Uint8Array(await request.arrayBuffer()));
  if(path==="null")return new Response(String(request.body===null));
  const text=await request.text();let rejected=false;try{await request.text()}catch{rejected=true}return new Response(text+":"+rejected);
 }`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	for _, tc := range []struct{ mode, body, contentType, want string }{{"text", "hello", "", "hello:true"}, {"clone", "hello", "", "hello:hello"}, {"transfer", "hello", "", "hello:true"}, {"json", `{"value":42}`, "application/json", `{"value":42}`}, {"form", "key=value", "application/x-www-form-urlencoded", "value"}, {"blob", "hello", "", "hello"}, {"array", "hello", "", "hello"}, {"null", "", "", "true"}} {
		req, err := http.NewRequest("POST", rig.url+"?mode="+tc.mode, strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if tc.contentType != "" {
			req.Header.Set("Content-Type", tc.contentType)
		}
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if got := receiveText(t, res); got != tc.want {
			t.Fatalf("%s=%q", tc.mode, got)
		}
	}
}

// A real net/http Body.Read is held inside Go's Expect wrapper. This barrier
// proves the physical read started, rather than just scheduling a JS read.
func TestServeExpectCanceledPhysicalReadPreservesDelayedResponse(t *testing.T) {
	entered := make(chan struct{})
	proceed := make(chan struct{})
	later := make(chan struct{})
	finished := make(chan error, 1)
	fixture := http.Server{}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rc := http.NewResponseController(w)
		rc.EnableFullDuplex()
		w.WriteHeader(100)
		body := &serveBody{body: &notifyingServeBody{ReadCloser: r.Body, entered: entered}, remaining: 32}
		readDone := make(chan error, 1)
		go func() { _, err := body.read(); readDone <- err }()
		<-proceed
		body.cancel()
		body.interruptCanceled(rc)
		if err := <-readDone; err == nil {
			finished <- errors.New("cancelled physical read succeeded")
			return
		}
		if !body.locallyStopped() || r.Context().Err() == nil {
			finished <- errors.New("expected local context cancellation")
			return
		}
		w.Header().Set("Connection", "close")
		w.WriteHeader(200)
		io.WriteString(w, "first")
		if err := rc.Flush(); err != nil {
			finished <- err
			return
		}
		<-later
		io.WriteString(w, "second")
		err := rc.Flush()
		body.close(rc)
		finished <- err
	})
	go fixture.Serve(listener)
	t.Cleanup(func() { fixture.Close() })
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	fmt.Fprintf(conn, "POST / HTTP/1.1\r\nHost: local\r\nExpect: 100-continue\r\nContent-Length: 20\r\n\r\n")
	reader := bufio.NewReader(conn)
	if receiveResponse(t, reader).StatusCode != 100 {
		t.Fatal("no100")
	}
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("native body read not entered")
	}
	close(proceed)
	res := receiveResponse(t, reader)
	prefix := make([]byte, 5)
	if _, err := io.ReadFull(res.Body, prefix); err != nil {
		t.Fatal(err)
	}
	if string(prefix) != "first" {
		t.Fatal(string(prefix))
	}
	close(later)
	if got := receiveText(t, res); got != "second" {
		t.Fatal(got)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("physical input cleanup incomplete")
	}
}

type notifyingServeBody struct {
	io.ReadCloser
	entered chan struct{}
	once    sync.Once
}

func (b *notifyingServeBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	return b.ReadCloser.Read(p)
}

func TestServeMalformedBodyReadRejectsAfterDispatch(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{notify("dispatch");try{await request.text();return new Response("unexpected")}catch(error){notify("rejected");return new Response("caught")}}`)
	conn, reader := receiveConn(t, rig, "Transfer-Encoding: chunked\r\n")
	rig.notice(t, "dispatch")
	fmt.Fprint(conn, "not-a-chunk-size\r\n")
	rig.notice(t, "rejected")
	if got := receiveText(t, receiveResponse(t, reader)); got != "caught" {
		t.Fatal(got)
	}
}

func TestServeNormalEOFReusesConnection(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>new Response(await request.text())`)
	client := &http.Client{Timeout: 4 * time.Second}
	defer client.CloseIdleConnections()
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(rig.url, "http://"), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(4 * time.Second))
	reader := bufio.NewReader(conn)
	for i := 0; i < 2; i++ {
		fmt.Fprint(conn, "POST / HTTP/1.1\r\nHost: local\r\nContent-Length: 2\r\n\r\nab")
		res := receiveResponse(t, reader)
		if got := receiveText(t, res); got != "ab" || res.Close {
			t.Fatalf("request %d body=%q close=%t", i, got, res.Close)
		}
	}
}

func TestServeSynchronousCloseForcesPendingInput(t *testing.T) {
	rig := newReceiveRig(t, 0, `async request=>{const reader=request.body.getReader();reader.read().catch(()=>{});notify("closing");server.close();return new Response("late")}`)
	conn, _ := receiveConn(t, rig, "Content-Length: 20\r\n")
	rig.notice(t, "closing")
	started := time.Now()
	buffer := make([]byte, 1)
	_, err := conn.Read(buffer)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("pending input connection remained open")
	}
	if elapsed < 4*time.Second || elapsed > 7*time.Second {
		t.Fatalf("forced shutdown delay=%v error=%v", elapsed, err)
	}
	rig.stop()
	// Force-close must release the listener as well as the pending body.
	listener, err := net.Listen("tcp", strings.TrimPrefix(rig.url, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	listener.Close()
}

func TestServerRuntimeForceCloseInterruptsGracefulShutdown(t *testing.T) {
	rt := quickjs.New()
	defer rt.Close()
	loop := NewLoop(rt)
	defer loop.Close()
	loop.Begin()
	lifetime := newServerLifetime(loop)
	entered := make(chan struct{})
	shuttingDown := make(chan struct{})
	graceDone := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !lifetime.enter() {
			return
		}
		defer lifetime.leave()
		close(entered)
		<-r.Context().Done()
	})}
	srv.RegisterOnShutdown(func() { close(shuttingDown) })
	rt.OnClose(func() { lifetime.force(srv) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	go srv.Serve(listener)
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprint(conn, "GET / HTTP/1.1\r\nHost: local\r\n\r\n")
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("handler not entered")
	}
	go func() {
		lifetime.stopAdmission()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			lifetime.force(srv)
		} else {
			lifetime.wait()
		}
		close(graceDone)
	}()
	select {
	case <-shuttingDown:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown not entered")
	}
	rt.Close() // Must use the force path even while Shutdown is in progress.
	select {
	case <-graceDone:
	case <-time.After(time.Second):
		t.Fatal("force close did not release graceful Shutdown")
	}
	if loop.Pending() {
		t.Fatal("listener keepalive left pending")
	}
}

// Exercise the actual serveJS Request source and Go promise bridge, with
// barriers on the owner so no-demand and completed-pull counts are observable.
func TestServeJSBodyReadsOnlyOnDemand(t *testing.T) {
	type checkpoint struct {
		name   string
		resume chan struct{}
	}
	checkpoints := make(chan checkpoint, 2)
	finished := make(chan error, 1)
	loops := make(chan *Loop, 1)
	var nativeCalls atomic.Int32
	probe := &countedServeReader{reader: strings.NewReader("binary")}
	go func() {
		rt := quickjs.New()
		defer rt.Close()
		loop := NewLoop(rt)
		defer loop.Close()
		loops <- loop
		if err := Install(rt, Config{Loop: loop}); err != nil {
			finished <- err
			return
		}
		rt.Set("checkpoint", func(name string) *quickjs.Promise {
			p := rt.NewPromise()
			resume := make(chan struct{})
			checkpoints <- checkpoint{name, resume}
			loop.Begin()
			go func() {
				defer loop.Done()
				select {
				case <-resume:
					loop.Post(func() { p.Resolve(nil) })
				case <-loop.Context().Done():
				}
			}()
			return p
		})
		var dispatch quickjs.Value
		host := rt.NewObject()
		host.Set("listen", func(_ quickjs.Value, handler quickjs.Value) map[string]any {
			dispatch = handler
			return map[string]any{}
		})
		api, err := evalWithHost(rt, "<test-serve>", serveJS, host)
		if err != nil {
			finished <- err
			return
		}
		serve, err := api.Get("serve")
		if err != nil {
			finished <- err
			return
		}
		rt.Set("serve", serve)
		_, err = rt.Eval(`serve(async request=>{await Promise.resolve();await checkpoint("no demand");const reader=request.body.getReader();const first=await reader.read();if(first.value.length!==6)throw Error("bytes");await checkpoint("one pull");await reader.cancel();return new Response("ok");})`)
		if err != nil {
			finished <- err
			return
		}
		ex := &exchange{method: "POST", url: "http://fixture/", hasBody: true, input: &serveBody{body: probe, remaining: 100000}, answer: make(chan *reply, 1), chunks: make(chan []byte), gone: make(chan struct{}), ready: make(chan struct{})}
		s := &servers{rt: rt, cfg: &Serve{Loop: loop}}
		// Count entry synchronously, so a native read scheduled by an eager
		// source cannot escape observation by waiting for a Go timeslice.
		rt.Set("recordRead", func() { nativeCalls.Add(1) })
		rt.Set("originalDispatch", dispatch)
		wrapped, err := rt.Eval(`(raw,done,sink)=>{const read=raw.read;raw.read=()=>{recordRead();return read();};return originalDispatch(raw,done,sink);}`)
		if err != nil {
			finished <- err
			return
		}
		s.handle(wrapped, ex)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := loop.Run(ctx); err != nil {
			finished <- err
			return
		}
		select {
		case response := <-ex.answer:
			if string(response.body) != "ok" {
				finished <- fmt.Errorf("response=%q", response.body)
				return
			}
		default:
			finished <- errors.New("missing response")
			return
		}
		finished <- nil
	}()
	loop := <-loops
	t.Cleanup(loop.Close)
	for i, want := range []string{"no demand", "one pull"} {
		select {
		case c := <-checkpoints:
			if c.name != want || probe.reads.Load() != int32(i) || nativeCalls.Load() != int32(i) {
				t.Fatalf("checkpoint=%q nativeCalls=%d nativeReads=%d want=%q/%d", c.name, nativeCalls.Load(), probe.reads.Load(), want, i)
			}
			if i == 1 && probe.maxSize.Load() != serveReadSize {
				t.Fatalf("native buffer size=%d", probe.maxSize.Load())
			}
			close(c.resume)
		case err := <-finished:
			t.Fatalf("owner ended early: %v", err)
		case <-time.After(4 * time.Second):
			t.Fatal("missing demand checkpoint")
		}
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("owner did not finish")
	}
}

type countedServeReader struct {
	reader  io.Reader
	reads   atomic.Int32
	maxSize atomic.Int32
}

func (r *countedServeReader) Read(b []byte) (int, error) {
	r.reads.Add(1)
	r.maxSize.Store(int32(len(b)))
	return r.reader.Read(b)
}
func (r *countedServeReader) Close() error { return nil }
