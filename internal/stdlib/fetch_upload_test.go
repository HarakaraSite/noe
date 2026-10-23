package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	quickjs "forge.harakara.site/littleisland/noe/third_party/go-quickjs"
	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

// Report rejected async test tasks instead of losing their diagnostics.
func runUpload(t *testing.T, cfg stdlib.Config, source string) (string, string) {
	t.Helper()
	return run(t, cfg, "Promise.resolve("+source+").catch(error=>console.error(error.stack||String(error)))")
}

type uploadTransport func(*http.Request) (*http.Response, error)

func (f uploadTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func uploadResponse(r *http.Request, body string) *http.Response {
	return &http.Response{StatusCode: 200, Status: "200 OK", Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: r}
}

func TestFetchUploadArrivesBeforeProducerEOF(t *testing.T) {
	prefix := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ack" {
			select {
			case <-prefix:
				io.WriteString(w, "ready")
			case <-r.Context().Done():
			}
			return
		}
		if r.ContentLength != -1 || len(r.TransferEncoding) != 1 || r.TransferEncoding[0] != "chunked" {
			t.Errorf("stream framing: length=%d transfer=%v", r.ContentLength, r.TransferEncoding)
		}
		first := make([]byte, 2)
		if _, err := io.ReadFull(r.Body, first); err != nil {
			t.Error(err)
			return
		}
		close(prefix)
		tail, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		w.Write(append(first, tail...))
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 5 * time.Second}}}, fmt.Sprintf(`
(async()=>{
 const {fetch}=await import("noe:http");let pulls=0;
 const stream=new ReadableStream({async pull(c){
  if(pulls++===0)c.enqueue(new Uint8Array([0,255]));
  else {if(await(await fetch(%q+"/ack")).text()!=="ready")throw Error("ack");
   c.enqueue(new Uint8Array([88,120,121,99]).subarray(1,3));c.close();}
 }},{highWaterMark:0});
 const response=await fetch(%q,{method:"POST",headers:{"content-length":"999"},body:stream});
 console.log((await response.bytes()).join());
})()`, server.URL, server.URL))
	if out != "0,255,120,121" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadBackpressure(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	client := &http.Client{Timeout: 5 * time.Second, Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		switch r.URL.Path {
		case "/probe":
			return uploadResponse(r, "ready"), nil
		case "/release":
			once.Do(func() { close(release) })
			return uploadResponse(r, "released"), nil
		}
		defer r.Body.Close()
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return uploadResponse(r, string(b)), nil
	})}
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, `
(async()=>{
 let pulls=0,ready;const first=new Promise(r=>ready=r);
 const body=new ReadableStream({pull(c){ready();if(++pulls<=3)c.enqueue(new Uint8Array([pulls]));else c.close();}},{highWaterMark:0});
 const pending=fetch("http://fixture/upload",{method:"POST",body});
 await first;await(await fetch("http://fixture/probe")).text();
 if(pulls!==1)throw Error("read ahead while write blocked: "+pulls);
 await(await fetch("http://fixture/release")).text();
 console.log((await(await pending).bytes()).join());
})()`)
	if out != "1,2,3" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadRequestOwnership(t *testing.T) {
	client := &http.Client{Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		if r.Body == nil {
			return uploadResponse(r, ""), nil
		}
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return uploadResponse(r, string(b)), nil
	})}
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, `
(async()=>{
 const make=()=>new Request("http://fixture/",{method:"POST",body:ReadableStream.from([new Uint8Array([65]),new Uint8Array(0),new Uint8Array([66])])});
 async function sent(r,init){return (await fetch(r,init)).text();}
 async function rejects(f){let yes=false;try{await f();}catch(e){yes=e instanceof TypeError;}if(!yes)throw Error("not rejected");}
 const direct=make();if(await sent(direct)!=="AB"||!direct.bodyUsed||direct.body.locked)throw Error("direct");
 await rejects(()=>sent(direct));await rejects(()=>direct.clone());
 const source=make(),moved=new Request(source);
 await rejects(()=>source.body.getReader());
 if(!source.bodyUsed||await sent(moved)!=="AB")throw Error("transfer");await rejects(()=>source.text());
 const overridden=make();if(await sent(overridden,{headers:{"x-override":"yes"}})!=="AB"||!overridden.bodyUsed)throw Error("override");
 const replaced=make();if(await sent(replaced,{body:"new"})!=="new"||replaced.bodyUsed||await replaced.text()!=="AB")throw Error("replacement");
 const original=make(),copy=original.clone();
 if((await Promise.all([sent(original),sent(copy)])).join()!=="AB,AB")throw Error("clone");
 const locked=make(),reader=locked.body.getReader();await rejects(()=>sent(locked));await reader.cancel();reader.releaseLock();
 const disturbed=make(),partial=disturbed.body.getReader();await partial.read();partial.releaseLock();
 if(!disturbed.bodyUsed)throw Error("partial read not recorded");await rejects(()=>sent(disturbed));await rejects(()=>disturbed.clone());
 const bytes=new Request("http://fixture/",{method:"POST",body:"bytes"});
 void bytes.body;await Promise.resolve();if(bytes.bodyUsed||await sent(bytes)!=="bytes")throw Error("byte body getter");await rejects(()=>sent(bytes));
 for(const body of [new Uint8Array([88,65,66,99]).subarray(1,3),new Uint8Array([65,66]).buffer]){
  if(await sent("http://fixture/",{method:"POST",body})!=="AB")throw Error("finite bytes");
 }
 for(const method of ["GET","HEAD"])await rejects(()=>fetch("http://fixture/",{method,body:ReadableStream.from([])}));
 const empty=new Request("http://fixture/");await(await fetch(empty)).text();if(empty.body!==null||empty.bodyUsed)throw Error("empty");
 console.log("request bodies owned once");
})()`)
	if out != "request bodies owned once" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadEarlyResponsePreservesDelayedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := http.NewResponseController(w).EnableFullDuplex(); err != nil {
			t.Error(err)
			return
		}
		if _, err := io.ReadFull(r.Body, make([]byte, 1)); err != nil {
			t.Error(err)
			return
		}
		w.WriteHeader(413)
		io.WriteString(w, "first-")
		w.(http.Flusher).Flush()
		// The second response part cannot already be in the client's read
		// buffer: it is withheld until the unfinished upload receives EOF.
		if _, err := io.ReadAll(r.Body); err != nil {
			t.Errorf("intentional upload stop was not EOF: %v", err)
			return
		}
		io.WriteString(w, "last")
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 5 * time.Second}}}, fmt.Sprintf(`
(async()=>{
 let calls=0,cancelled=0;
 const body=new ReadableStream({pull(c){if(++calls===1)c.enqueue(new Uint8Array([1]));else return new Promise(()=>{});},
  cancel(){cancelled++;return new Promise(()=>{});}},{highWaterMark:0});
 const source=new Request(%q,{method:"POST",body});
 const response=await fetch(new Request(source),{headers:{"x-transfer":"yes"}});
 if(response.status!==413||await response.text()!=="first-last")throw Error("early response lost");
 if(cancelled!==1||body.locked)throw Error("source not released");
 console.log("early response preserved");
})()`, server.URL))
	if out != "early response preserved" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadAbortPendingIO(t *testing.T) {
	for _, blocked := range []string{"read", "write"} {
		t.Run(blocked, func(t *testing.T) {
			done := make(chan struct{})
			client := &http.Client{Timeout: 5 * time.Second, Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/probe" {
					return uploadResponse(r, "ready"), nil
				}
				defer close(done)
				defer r.Body.Close()
				if blocked == "read" {
					_, err := r.Body.Read(make([]byte, 1))
					return nil, err
				}
				<-r.Context().Done()
				return nil, r.Context().Err()
			})}
			out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, fmt.Sprintf(`
(async()=>{
 const controller=new AbortController(),reason=new Error("abort upload");let ready,cancelled=0;
 const first=new Promise(r=>ready=r);
 const body=new ReadableStream({pull(c){ready();if(%q==="write")c.enqueue(new Uint8Array([1]));else return new Promise(()=>{});},
  cancel(){cancelled++;return Promise.reject(Error("cancel hook failed"));}},{highWaterMark:0});
 const pending=fetch("http://fixture/upload",{method:"POST",body,signal:controller.signal});
 await first;await(await fetch("http://fixture/probe")).text();controller.abort(reason);
 try{await pending;throw Error("abort ignored");}catch(e){if(e!==reason)throw e;}
 await Promise.resolve();if(cancelled!==1)throw Error("source not cancelled");
 console.log("upload aborted");
})()`, blocked))
			if out != "upload aborted" || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q", out, stderr)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("transport still blocked")
			}
		})
	}
}

func TestFetchUploadProducerFailure(t *testing.T) {
	client := &http.Client{Timeout: 5 * time.Second, Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return uploadResponse(r, string(b)), nil
	})}
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, `
(async()=>{
 for(const invalid of [false,true]){
  let pulls=0;const body=new ReadableStream({pull(c){if(++pulls===1)c.enqueue(new Uint8Array([1]));else if(invalid)c.enqueue("bad");else throw Error("producer failed");}},{highWaterMark:0});
  let rejected=false;try{await fetch("http://fixture/",{method:"POST",body});}catch(e){rejected=e instanceof TypeError&&e.message==="fetch failed"&&String(e.cause).includes(invalid?"Uint8Array":"producer failed");}
  if(!rejected||body.locked)throw Error("producer failure lost");
 }
 console.log("producer failures propagated");
})()`)
	if out != "producer failures propagated" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadTimeoutIncludesFirstChunkWait(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.Copy(io.Discard, r.Body) }))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 100 * time.Millisecond}}}, fmt.Sprintf(`
(async()=>{
 let cancelled=0;const body=new ReadableStream({pull(){return new Promise(()=>{});},cancel(){cancelled++;return new Promise(()=>{});}},{highWaterMark:0});
 let failed=false;try{await fetch(%q,{method:"POST",body});}catch(e){failed=e instanceof TypeError;}
 if(!failed||cancelled!==1||body.locked)throw Error("timeout did not release source");
 console.log("producer wait timed out");
})()`, server.URL))
	if out != "producer wait timed out" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/redirect/") {
			var code int
			fmt.Sscanf(r.URL.Path, "/redirect/%d", &code)
			http.NewResponseController(w).EnableFullDuplex()
			if code < 307 {
				if _, err := io.ReadFull(r.Body, make([]byte, 1)); err != nil {
					t.Error(err)
					return
				}
			} else {
				io.Copy(io.Discard, r.Body)
			}
			w.Header().Set("Location", "/final")
			w.WriteHeader(code)
			io.WriteString(w, "redirect")
			w.(http.Flusher).Flush()
			return
		}
		b, _ := io.ReadAll(r.Body)
		fmt.Fprintf(w, "%s:%s", r.Method, b)
	}))
	defer server.Close()
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: &http.Client{Timeout: 5 * time.Second}}}, fmt.Sprintf(`
(async()=>{
 for(const status of [307,308]){
  const stream=ReadableStream.from([new TextEncoder().encode("payload")]);
  const once=await fetch(%q+"/redirect/"+status,{method:"POST",body:stream});
  if(once.status!==status||once.redirected||await once.text()!=="redirect")throw Error("stream was replayed");
  const bytes=await fetch(%q+"/redirect/"+status,{method:"POST",body:"payload"});
  if(!bytes.redirected||await bytes.text()!=="POST:payload")throw Error("known body replay lost");
 }
 for(const status of [301,302,303]){
  let pulled=0,cancelled=0;const stream=new ReadableStream({pull(c){if(++pulled===1)c.enqueue(new Uint8Array([1]));else return new Promise(()=>{});},cancel(){cancelled++;}},{highWaterMark:0});
  const response=await fetch(%q+"/redirect/"+status,{method:"POST",body:stream});
  if(!response.redirected||await response.text()!=="GET:"||cancelled!==1||stream.locked)throw Error("body-dropping redirect");
 }
 console.log("redirect policies preserved");
})()`, server.URL, server.URL, server.URL))
	if out != "redirect policies preserved" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadPermissionFailureReleasesSource(t *testing.T) {
	for _, url := range []string{"http://fixture/", ":invalid"} {
		t.Run(url, func(t *testing.T) {
			out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Allow: func(*http.Request) error { return errors.New("upload denied") }}}, fmt.Sprintf(`
(async()=>{
 let cancelled=0;const body=new ReadableStream({pull(){return new Promise(()=>{});},cancel(){cancelled++;}},{highWaterMark:0});
 let failed=false;try{await fetch(%q,{method:"POST",body});}catch(e){failed=e instanceof TypeError;}
 if(!failed||cancelled!==1||body.locked)throw Error("failed upload retained source");
 console.log("source released");
})()`, url))
			if out != "source released" || stderr != "" {
				t.Fatalf("stdout=%q stderr=%q", out, stderr)
			}
		})
	}
}

func TestFetchUploadWithoutLoopBuffersFiniteStream(t *testing.T) {
	client := &http.Client{Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if r.ContentLength != 2 || r.GetBody == nil {
			t.Errorf("synchronous finite body: length=%d GetBody=%v", r.ContentLength, r.GetBody != nil)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return nil, err
		}
		return uploadResponse(r, string(b)), nil
	})}
	rt := quickjs.New()
	defer rt.Close()
	if err := stdlib.Install(rt, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}); err != nil {
		t.Fatal(err)
	}
	value, err := rt.Eval(`fetch("http://fixture/",{method:"POST",body:ReadableStream.from([new Uint8Array([65,66])])}).then(r=>r.text())`)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	value, err = value.Await(ctx)
	if err != nil || value.String() != "AB" {
		t.Fatalf("value=%v err=%v", value, err)
	}
}

func TestFetchUploadPreAbortedDoesNotAcquireBody(t *testing.T) {
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Allow: func(*http.Request) error {
		t.Error("pre-aborted request reached transport admission")
		return errors.New("unexpected request")
	}}}, `
(async()=>{
 const controller=new AbortController(),reason=new Error("already cancelled");controller.abort(reason);
 for(const mode of ["direct","inherited","override"]){
  let pulls=0,cancels=0;const body=new ReadableStream({pull(){pulls++;},cancel(){cancels++;}},{highWaterMark:0});
  const request=new Request("http://fixture/",{method:"POST",body,signal:mode==="override"?undefined:controller.signal});
  const init=mode==="direct"?undefined:mode==="inherited"?{}:{signal:controller.signal};
  try{await fetch(request,init);throw Error("abort ignored");}catch(e){if(e!==reason)throw e;}
  if(request.bodyUsed||body.locked||pulls!==0||cancels!==0)throw Error("pre-aborted body acquired: "+mode);
 }
 console.log("pre-aborted body untouched");
})()`)
	if out != "pre-aborted body untouched" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadSignalListenerLifetime(t *testing.T) {
	client := &http.Client{Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
		defer r.Body.Close()
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			return nil, err
		}
		return uploadResponse(r, strings.Repeat("x", 96<<10)), nil
	})}
	out, stderr := runUpload(t, stdlib.Config{Fetch: &stdlib.Fetch{Client: client}}, `
(async()=>{
 class Signal extends EventTarget {
  constructor(){super();this.aborted=false;this.listeners=0;}
  addEventListener(...args){this.listeners++;super.addEventListener(...args);}
  removeEventListener(...args){this.listeners--;super.removeEventListener(...args);}
 }
 const signal=new Signal();
 for(const consume of [true,false]){
  const response=await fetch("http://fixture/",{method:"POST",body:ReadableStream.from([new Uint8Array([1])]),signal});
  if(signal.listeners!==1)throw Error("signal detached before response finished");
  if(consume){if((await response.bytes()).length!==96*1024)throw Error("response bytes");}else await response.body.cancel();
  if(signal.listeners!==0)throw Error("signal listener retained");
 }
 console.log("signal lifetime matches response");
})()`)
	if out != "signal lifetime matches response" || stderr != "" {
		t.Fatalf("stdout=%q stderr=%q", out, stderr)
	}
}

func TestFetchUploadCloseReleasesGoReadWithoutOwnerCallback(t *testing.T) {
	for _, closeRuntime := range []bool{false, true} {
		t.Run(fmt.Sprintf("runtime=%v", closeRuntime), func(t *testing.T) {
			started, done := make(chan struct{}), make(chan struct{})
			client := &http.Client{Transport: uploadTransport(func(r *http.Request) (*http.Response, error) {
				defer close(done)
				defer r.Body.Close()
				close(started)
				_, err := r.Body.Read(make([]byte, 1))
				return nil, err
			})}
			rt := quickjs.New()
			defer rt.Close()
			loop := stdlib.NewLoop(rt)
			if err := stdlib.Install(rt, stdlib.Config{Loop: loop, Fetch: &stdlib.Fetch{Client: client}}); err != nil {
				t.Fatal(err)
			}
			if _, err := rt.Eval(`fetch("http://fixture/",{method:"POST",body:new ReadableStream({pull(){return new Promise(()=>{});}},{highWaterMark:0})}).catch(()=>{});`); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			if err := loop.RunUntil(ctx, started); err != nil {
				t.Fatal(err)
			}
			if closeRuntime {
				rt.Close()
			} else {
				loop.Close()
			}
			// Do not run JS jobs again: closing Go IO cannot depend on them.
			select {
			case <-done:
			case <-ctx.Done():
				t.Fatal("Body.Read outlived its runtime/loop")
			}
		})
	}
}
