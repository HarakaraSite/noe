package stdlib_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func executeEventSource(t *testing.T, profile stdlib.Profile, code string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[EventSource]", Module: true, Virtual: true, Code: code}, profile)
	if err != nil {
		t.Fatal(stdlib.Diagnostic(err))
	}
}

func TestEventSourceCapabilityAndConstructor(t *testing.T) {
	executeEventSource(t, stdlib.Profile{}, `if(typeof EventSource!=="undefined")throw Error("ambient network authority");`)
	var allowed atomic.Int32
	profile := stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Allow: func(*http.Request) error {
		allowed.Add(1)
		return errors.New("fixture policy refusal")
	}}}}
	executeEventSource(t, profile, `
for(const input of ["relative","://"]){try{new EventSource(input);throw Error("accepted invalid URL")}catch(e){if(!(e instanceof DOMException)||e.name!=="SyntaxError")throw e}}
try{new EventSource();throw Error("missing URL accepted")}catch(e){if(!(e instanceof TypeError))throw e}
for(const operation of [()=>new EventSource(Symbol()),()=>new EventSource("https://example.invalid",42)]){try{operation();throw Error("invalid conversion accepted")}catch(e){if(!(e instanceof TypeError))throw e}}
const source=new EventSource("https://example.invalid/feed#fragment",{withCredentials:true});
if(!(source instanceof EventTarget)||source.url!=="https://example.invalid/feed#fragment"||!source.withCredentials||source.readyState!==0)throw Error("constructor shape");
if(EventSource.CONNECTING!==0||source.OPEN!==1||source.CLOSED!==2)throw Error("constants");
await new Promise(resolve=>{source.onerror=event=>{if(!(event instanceof Event)||source.readyState!==2)throw Error("policy must be terminal");resolve()}});
source.close();source.close();if(source.readyState!==2)throw Error("close state");
const order=[];
source.addEventListener("message",()=>order.push("before"));
const handler=()=>{if(source.onmessage!==handler)throw Error("handler getter changed during dispatch");order.push("handler")};
source.onmessage=handler;
source.addEventListener("message",()=>order.push("after"));
source.dispatchEvent(new MessageEvent("message",{data:"manual"}));
if(order.join(",")!=="before,handler,after")throw Error("handler order or duplicate invocation: "+order);
source.onmessage=null;source.dispatchEvent(new MessageEvent("message"));
if(order.join(",")!=="before,handler,after,before,after")throw Error("handler removal");
`)
	if allowed.Load() != 1 {
		t.Fatalf("policy calls=%d", allowed.Load())
	}
}

func TestEventSourceParsingReconnectionAndEventTasks(t *testing.T) {
	var attempts atomic.Int32
	requests := make(chan *http.Request, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/events", http.StatusTemporaryRedirect)
			return
		}
		requests <- r.Clone(r.Context())
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		var data string
		if attempts.Add(1) == 1 {
			data = "\ufeff: heartbeat\r\nretry: 1\r\nid: alpha\revent: update\ndata: 日本語\r\ndata: second\r\n\r\nid: ignored\x00value\ndata:\n\nid:\n\nid: beta\n\ndata: discarded at EOF"
		} else {
			data = "id: gamma\ndata: done\n\ndata: must not dispatch after close\n\n"
		}
		// Deliberately split UTF-8, BOM and line separators over writes.
		for _, b := range []byte(data) {
			if _, err := w.Write([]byte{b}); err != nil {
				return
			}
			w.(http.Flusher).Flush()
		}
	}))
	defer server.Close()
	profile := stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}
	executeEventSource(t, profile, fmt.Sprintf(`
const source=new EventSource(%q),messages=[],order=[];let opens=0,errors=0,microtask=false;
const listenerController=new AbortController();
source.addEventListener("message",()=>{throw Error("aborted SSE listener");},{signal:listenerController.signal});
listenerController.abort();
let namedCalls=0;const namedListener=()=>namedCalls++;
source.addEventListener("update",namedListener);source.addEventListener("update",namedListener,{once:true});
await new Promise((resolve,reject)=>{
 source.onopen=()=>{opens++;if(source.readyState!==1)reject(Error("open state"));};
 source.onerror=()=>{errors++;if(source.readyState!==0)reject(Error("retry state"));};
 source.addEventListener("update",event=>{
  if(!(event instanceof MessageEvent)||event.origin!==%q||event.data!=="日本語\nsecond"||event.lastEventId!=="alpha")reject(Error("named event "+JSON.stringify({data:event.data,id:event.lastEventId,origin:event.origin})));
  messages.push(event.data);queueMicrotask(()=>{microtask=true});
 });
 source.onmessage=event=>{
  if(!microtask)reject(Error("missing microtask checkpoint"));
  messages.push(event.data);
  if(event.data===""){if(event.lastEventId!=="alpha")reject(Error("NUL id was accepted"));return}
  if(event.data!=="done"||event.lastEventId!=="gamma")reject(Error("unexpected event "+event.data));
  source.close();if(source.readyState!==2)reject(Error("close not synchronous"));resolve();
 };
});
if(namedCalls!==1||opens!==2||errors!==1||JSON.stringify(messages)!==JSON.stringify(["日本語\nsecond","","done"]))throw Error("event sequence "+JSON.stringify({namedCalls,opens,errors,messages}));
`, server.URL+"/redirect", server.URL))
	if attempts.Load() != 2 {
		t.Fatalf("connections=%d", attempts.Load())
	}
	for i, wantID := range []string{"", "beta"} {
		request := <-requests
		if request.Method != "GET" || request.URL.Path != "/events" || request.Header.Get("Accept") != "text/event-stream" || request.Header.Get("Last-Event-ID") != wantID {
			t.Errorf("attempt %d: method=%s path=%s headers=%v", i, request.Method, request.URL.Path, request.Header)
		}
	}
}

func TestEventSourceTerminalResponsesAndCloseReleaseBody(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		mime        string
		closeOnOpen bool
	}{
		{"204", 204, "", false},
		{"500", 500, "text/event-stream", false},
		{"wrong MIME", 200, "text/plain", false},
		{"missing MIME", 200, "", false},
		{"close on open", 200, "text/event-stream", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var attempts atomic.Int32
			closed := make(chan struct{}, 2)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attempts.Add(1)
				if test.mime != "" {
					w.Header().Set("Content-Type", test.mime)
				}
				w.WriteHeader(test.status)
				w.(http.Flusher).Flush()
				// A 204 response has no body to cancel. The exchange is already
				// complete, and its idle HTTP connection may be reused.
				if test.status == http.StatusNoContent {
					closed <- struct{}{}
					return
				}
				<-r.Context().Done()
				closed <- struct{}{}
			}))
			defer server.Close()
			event := "error"
			if test.closeOnOpen {
				event = "open"
			}
			executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, fmt.Sprintf(`
const source=new EventSource(%q);
await new Promise((resolve,reject)=>{
 source.onmessage=()=>reject(Error("unexpected message"));
 source.addEventListener(%q,()=>{source.close();source.close();resolve()},{once:true});
});if(source.readyState!==2)throw Error("not closed");`, server.URL, event))
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("response body left open")
			}
			if attempts.Load() != 1 {
				t.Fatalf("attempts=%d", attempts.Load())
			}
		})
	}
}

func TestEventSourceTLSCookiesAndWorkerInheritance(t *testing.T) {
	for _, worker := range []bool{false, true} {
		t.Run(fmt.Sprintf("worker=%t", worker), func(t *testing.T) {
			requests := make(chan *http.Request, 2)
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.Clone(r.Context())
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: authenticated\n\n")
			}))
			defer server.Close()
			client := server.Client()
			jar, _ := cookiejar.New(nil)
			target, _ := url.Parse(server.URL)
			jar.SetCookies(target, []*http.Cookie{{Name: "session", Value: "fixture"}})
			client.Jar = jar
			var allowed atomic.Int32
			profile := stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Client: client, Allow: func(r *http.Request) error {
				allowed.Add(1)
				if r.URL.Host != target.Host {
					return errors.New("unexpected target")
				}
				return nil
			}}}}
			code := fmt.Sprintf(`for(const credentials of [false,true]){const source=new EventSource(%q,{withCredentials:credentials});await new Promise((resolve,reject)=>{source.onerror=()=>{source.close();reject(Error("TLS connection failed"))};source.onmessage=e=>{if(e.data!=="authenticated")reject(Error("message"));source.close();resolve()}})}`, server.URL)
			if worker {
				profile.Modules.Workers = &stdlib.WorkerOptions{Max: 1}
				code = fmt.Sprintf(`import {Worker} from "node:worker_threads";const worker=new Worker(%q,{eval:true});const value=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});await worker.terminate();if(value!=="ok")throw Error("worker");`, `import {parentPort} from "node:worker_threads";`+code+`parentPort.postMessage("ok");parentPort.close();`)
			}
			executeEventSource(t, profile, code)
			for _, want := range []string{"", "session=fixture"} {
				if got := (<-requests).Header.Get("Cookie"); got != want {
					t.Fatalf("Cookie=%q want %q", got, want)
				}
			}
			if allowed.Load() != 2 || client.Jar != jar || client.Timeout != 0 {
				t.Fatal("client changed or permission bypassed")
			}
		})
	}
}

func TestEventSourceRedirectPermissionAndCancellation(t *testing.T) {
	for _, mode := range []string{"headers", "body", "redirect refusal"} {
		t.Run(mode, func(t *testing.T) {
			started, stopped := make(chan struct{}), make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect refusal" {
					http.Redirect(w, r, "http://forbidden.invalid/events", 307)
					return
				}
				if mode == "body" {
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
				}
				close(started)
				<-r.Context().Done()
				close(stopped)
			}))
			defer server.Close()
			var refused atomic.Int32
			profile := stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{Allow: func(r *http.Request) error {
				if r.URL.Host == "forbidden.invalid" {
					refused.Add(1)
					return errors.New("fixture denial")
				}
				return nil
			}}}}
			if mode == "redirect refusal" {
				executeEventSource(t, profile, fmt.Sprintf(`const s=new EventSource(%q);await new Promise(resolve=>{s.onerror=()=>{if(s.readyState!==2)throw Error("retrying refused redirect");resolve()}});`, server.URL))
				if refused.Load() != 1 {
					t.Fatalf("redirect refusals=%d", refused.Load())
				}
				return
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			runtime, err := stdlib.NewRuntime(ctx, profile)
			if err != nil {
				t.Fatal(err)
			}
			defer runtime.Close()
			go func() {
				select {
				case <-started:
					runtime.Cancel()
				case <-ctx.Done():
				}
			}()
			_, err = runtime.Run(stdlib.Source{Name: "[EventSource cancellation]", Virtual: true, Module: true, Code: fmt.Sprintf(`new EventSource(%q);`, server.URL)})
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			if err := runtime.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-stopped:
			case <-time.After(time.Second):
				t.Fatal("request survived execution close")
			}
		})
	}
}

// A truncated response exercises transport recovery, not just clean EOF.
func TestEventSourceTransportRecoveryAndBufferLimit(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if attempts.Add(1) == 1 {
			w.Header().Set("Content-Length", "1000")
			io.WriteString(w, "retry: 1\n\ndata: partial")
			return
		}
		io.WriteString(w, "data: recovered\n\n")
	}))
	defer server.Close()
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, fmt.Sprintf(`const s=new EventSource(%q);await new Promise((resolve,reject)=>{s.onmessage=e=>{s.close();e.data==="recovered"?resolve():reject(Error(e.data))}});`, server.URL))
	if attempts.Load() != 2 {
		t.Fatalf("recovery attempts=%d", attempts.Load())
	}
	large := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: "+strings.Repeat("x", 100)+"\n\n")
	}))
	defer large.Close()
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{MaxBodyBytes: 32}}}, fmt.Sprintf(`const s=new EventSource(%q);await new Promise((resolve,reject)=>{s.onmessage=()=>reject(Error("buffer bound ignored"));s.onerror=()=>{if(s.readyState!==2)reject(Error("buffer error not terminal"));s.close();resolve()}});`, large.URL))
}

func TestEventSourceWorkerTerminationCancelsSubscription(t *testing.T) {
	closed := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(closed)
	}))
	defer server.Close()
	worker := fmt.Sprintf(`import {parentPort} from "node:worker_threads";const source=new EventSource(%q);source.onopen=()=>parentPort.postMessage("connected");`, server.URL)
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}, Workers: &stdlib.WorkerOptions{Max: 1}}}, fmt.Sprintf(`
import {Worker} from "node:worker_threads";
const worker=new Worker(%q,{eval:true});
if(await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)})!=="connected")throw Error("Worker did not connect");
await worker.terminate();`, worker))
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("EventSource survived Worker termination")
	}
}

func TestEventSourceCloseFromErrorStopsReconnection(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "retry: 1\n\n")
	}))
	defer server.Close()
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, fmt.Sprintf(`
const source=new EventSource(%q);
await new Promise(resolve=>{source.onerror=()=>{if(source.readyState!==0)throw Error("not reconnecting");source.close();resolve()}});`, server.URL))
	if attempts.Load() != 1 {
		t.Fatalf("reconnected after close: attempts=%d", attempts.Load())
	}
}

func TestEventSourcePreservesEventIDUntilExplicitReset(t *testing.T) {
	var attempts atomic.Int32
	ids := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids <- r.Header.Get("Last-Event-ID")
		w.Header().Set("Content-Type", "text/event-stream")
		switch attempts.Add(1) {
		case 1:
			fmt.Fprint(w, "retry: 1\nid: persisted\ndata: first\n\n")
		case 2:
			fmt.Fprint(w, "data: second\n\n")
		case 3:
			fmt.Fprint(w, "id:\ndata: third\n\n")
		default:
			fmt.Fprint(w, "data: end\n\n")
		}
	}))
	defer server.Close()
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{}}}, fmt.Sprintf(`
const source=new EventSource(%q),ids=[];
await new Promise(resolve=>{source.onmessage=e=>{ids.push(e.lastEventId);if(e.data==="end"){source.close();resolve()}}});
if(JSON.stringify(ids)!==JSON.stringify(["persisted","persisted","",""]))throw Error("event IDs "+JSON.stringify(ids));`, server.URL))
	if attempts.Load() != 4 {
		t.Fatalf("attempts=%d", attempts.Load())
	}
	for i, want := range []string{"", "persisted", "persisted", ""} {
		if got := <-ids; got != want {
			t.Errorf("attempt %d Last-Event-ID=%q want %q", i, got, want)
		}
	}
}

func TestEventSourceBufferLimitDoesNotLimitSubscriptionLength(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for i := 0; i < 100; i++ {
			fmt.Fprint(w, "data: x\n\n")
		}
		fmt.Fprint(w, "data: end\n\n")
	}))
	defer server.Close()
	executeEventSource(t, stdlib.Profile{Modules: stdlib.Modules{Fetch: &stdlib.FetchOptions{MaxBodyBytes: 32}}}, fmt.Sprintf(`
const source=new EventSource(%q);let count=0;
await new Promise((resolve,reject)=>{
 source.onmessage=e=>{if(e.data==="end"){source.close();resolve()}else{count++}};
 source.onerror=()=>{source.close();reject(Error("small events exceeded lifetime limit"))};
});if(count!==100)throw Error("events lost: "+count);`, server.URL))
}
