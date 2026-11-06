package stdlib_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func executeServeLifecycle(t *testing.T, code string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[HTTP server lifecycle]", Virtual: true, Module: true, Code: code}, stdlib.Profile{
		Modules: stdlib.Modules{
			Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }},
			Fetch: &stdlib.FetchOptions{},
		},
	})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestHTTPServerCloseAllowsActiveHandlersAndTimers(t *testing.T) {
	executeServeLifecycle(t, `
import {serve} from "noe:http";
let entered,finish;
const arrived=new Promise(resolve=>{entered=resolve});
const gate=new Promise(resolve=>{finish=resolve});
const server=serve({port:0},async request=>{entered();await gate;return new Response("finished");});
const response=fetch(server.url).then(response=>response.text());
await arrived;
const closing=server.close();
if(!(closing instanceof Promise)||closing!==server.close())throw Error("close is not one shared promise");
if(closing!==server[Symbol.asyncDispose]())throw Error("disposal did not delegate to close");
let timer=false;
setTimeout(()=>{timer=true;finish();},10);
if(await response!=="finished")throw Error("graceful close discarded active response");
if(await closing!==undefined||!timer)throw Error("close blocked JS or returned a value");
const replacement=serve({port:server.port},()=>new Response("replacement"));
await replacement.close();
`, 5*time.Second)
}

func TestHTTPServerAsyncDisposeOnNormalAndExceptionalExit(t *testing.T) {
	executeServeLifecycle(t, `
import {serve} from "noe:http";
let normal,failed;
{
 await using server=serve({port:0},()=>new Response("normal"));
 normal=server;
}
const firstReplacement=serve({port:normal.port},()=>new Response("reused"));
await firstReplacement.close();
const reason={scope:"expected failure"};
try{
 await using server=serve({port:0},()=>new Response("exception"));
 failed=server;
 throw reason;
}catch(error){if(error!==reason)throw error;}
const secondReplacement=serve({port:failed.port},()=>new Response("reused"));
await secondReplacement.close();
await failed.close();
`, 5*time.Second)
}

func TestHTTPServerCloseInitiatedByHandlerCanRespond(t *testing.T) {
	executeServeLifecycle(t, `
import {serve} from "noe:http";
let closing;
const server=serve({port:0},async request=>{
 closing=server.close();
 await Promise.resolve();
 return new Response("goodbye");
});
if(await(await fetch(server.url)).text()!=="goodbye")throw Error("shutdown handler lost response");
await closing;
`, 5*time.Second)
}

func TestHTTPServerCloseGraceDeadlineForcesActiveHandler(t *testing.T) {
	executeServeLifecycle(t, `
import {serve} from "noe:http";
let entered;
const arrived=new Promise(resolve=>{entered=resolve});
const server=serve({port:0},request=>{entered();return new Promise(()=>{});});
const response=fetch(server.url).then(()=>false,()=>true);
await arrived;
await server.close();
if(!await response)throw Error("stalled request survived forced shutdown");
const replacement=serve({port:server.port},()=>new Response("reused"));
await replacement.close();
`, 8*time.Second)
}

func TestHTTPServerRuntimeCancelInterruptsGracefulClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	address := make(chan string, 1)
	closing := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[cancel HTTP close]", Virtual: true, Module: true, Code: `
import {serve} from "noe:http";
const server=serve({port:0},request=>{
 server.close();
 notifyClose();
 return new Promise(()=>{});
});
notifyAddress(server.url);
await new Promise(()=>{});
`}, stdlib.Profile{
			Modules: stdlib.Modules{Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }}},
			Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
				if err := bindings.Runtime().Set("notifyAddress", func(url string) { address <- url }); err != nil {
					return nil, err
				}
				return nil, bindings.Runtime().Set("notifyClose", func() { closing <- struct{}{} })
			},
		})
		done <- err
	}()
	var url string
	select {
	case url = <-address:
	case err := <-done:
		t.Fatalf("server exited before startup: %s", stdlib.Diagnostic(err))
	case <-ctx.Done():
		t.Fatal("server did not start")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientDone := make(chan struct{})
	go func() {
		defer close(clientDone)
		if response, err := http.DefaultClient.Do(request); err == nil {
			response.Body.Close()
		}
	}()
	select {
	case <-closing:
	case <-ctx.Done():
		t.Fatal("graceful shutdown did not start")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation waited for the graceful deadline")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("forced shutdown took %s", elapsed)
	}
	select {
	case <-clientDone:
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation left the active connection open")
	}
}
