package stdlib_test

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func websocketHeaderFixture(t *testing.T, secure bool, requests chan<- *http.Request, binary bool) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.Clone(r.Context())
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		accept := sha1.Sum([]byte(r.Header.Get("Sec-WebSocket-Key") + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
		fmt.Fprintf(rw, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\nSec-WebSocket-Protocol: chat.v1\r\n\r\n", base64.StdEncoding.EncodeToString(accept[:]))
		// One message, followed by a normal close frame.
		if binary {
			rw.Write([]byte{0x82, 3, 0, 128, 255})
		} else {
			rw.Write([]byte{0x81, 2, 'o', 'k'})
		}
		rw.Write([]byte{0x88, 2, 3, 232})
		if err := rw.Flush(); err != nil {
			t.Error(err)
		}
		// Finish the peer handshake instead of dropping TCP immediately after
		// sending Close: the client must be able to write its masked response.
		var head [2]byte
		if _, err := io.ReadFull(rw, head[:]); err == nil {
			n := int(head[1] & 0x7f)
			if head[1]&0x80 != 0 {
				n += 4
			}
			_, _ = io.CopyN(io.Discard, rw, int64(n))
		}
	})
	if secure {
		return httptest.NewTLSServer(handler)
	}
	return httptest.NewServer(handler)
}

func TestWebSocketHeadersAcrossProfilesTLSAndWorkers(t *testing.T) {
	for _, test := range []struct {
		name, headers  string
		secure, worker bool
	}{
		{"record", `({Authorization:"Bearer fixture",Origin:"https://app.test",Cookie:"session=fixture","X-Test":"  original\t "})`, false, false},
		{"getter iterator", `({marker:true,get [Symbol.iterator](){delete this[Symbol.iterator];return function*(){if(!this.marker)throw Error("iterator receiver");yield ["Authorization","Bearer fixture"];yield ["Origin","https://app.test"];yield ["Cookie","session=fixture"];yield ["X-Test","original"]}}})`, false, false},
		{"Headers over TLS", `new Headers({Authorization:"Bearer fixture",Origin:"https://app.test",Cookie:"session=fixture","X-Test":"original"})`, true, false},
		{"iterable in Worker", `({*[Symbol.iterator](){yield ["Authorization","Bearer fixture"];yield ["Origin","https://app.test"];yield ["Cookie","session=fixture"];yield ["X-Test","original"];yield ["X-Repeat","one"];yield ["x-repeat","two"]}})`, false, true},
		{"pairs in TLS Worker", `[["Authorization","Bearer fixture"],["Origin","https://app.test"],["Cookie","session=fixture"],["X-Test","original"]]`, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := websocketHeaderFixture(t, test.secure, requests, false)
			defer server.Close()
			endpoint := strings.Replace(server.URL, "http", "ws", 1) + "/chat?room=fixture"
			options := &stdlib.WebSocketsOptions{Allow: func(target *url.URL) error {
				if target.String() != endpoint {
					return errors.New("unexpected target")
				}
				return nil
			}}
			if test.secure {
				options.TLS = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			}
			profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: options}}
			code := fmt.Sprintf(`import {createWebSocket} from "noe:http";

const headers=%s;
const socket=createWebSocket(%q,{protocols:["chat.v1","other"],headers});
if(!(socket instanceof WebSocket)||socket.readyState!==WebSocket.CONNECTING||socket.binaryType!=="blob")throw Error("host factory lifecycle");
if(headers instanceof Headers)headers.set("X-Test","changed");else if(!headers[Symbol.iterator])headers["X-Test"]="changed";
let message;
await new Promise((resolve,reject)=>{
 socket.onmessage=event=>{message=event.data};
 socket.onerror=event=>reject(event.error);
 socket.onclose=event=>event.wasClean&&event.code===1000&&message==="ok"&&socket.protocol==="chat.v1"?resolve():reject(Error("WebSocket close or protocol"));
});
if(typeof fetch!=="undefined")throw Error("WebSockets implicitly enabled fetch");
`, test.headers, endpoint)
			if test.worker {
				profile.Modules.Workers = &stdlib.WorkerOptions{Max: 1}
				worker := `import {parentPort} from "node:worker_threads";` + code + `parentPort.postMessage("ok");parentPort.close();`
				code = fmt.Sprintf(`import {Worker} from "node:worker_threads";
const worker=new Worker(%q,{eval:true});
const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();if(result!=="ok")throw Error("Worker headers");`, worker)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket headers]", Virtual: true, Module: true, Code: code}, profile)
			if err != nil {
				t.Fatal(stdlib.Diagnostic(err))
			}
			select {
			case request := <-requests:
				for name, want := range map[string]string{"Authorization": "Bearer fixture", "Origin": "https://app.test", "Cookie": "session=fixture", "X-Test": "original", "Sec-WebSocket-Protocol": "chat.v1, other", "Upgrade": "websocket", "Connection": "Upgrade"} {
					if request.Header.Get(name) != want {
						t.Errorf("header %s=%q want %q", name, request.Header.Get(name), want)
					}
				}
				if request.Host != strings.TrimPrefix(strings.TrimPrefix(server.URL, "https://"), "http://") || request.URL.RequestURI() != "/chat?room=fixture" || len(request.Header.Values("Sec-WebSocket-Key")) != 1 {
					t.Fatalf("handshake fields changed: host=%s URI=%s key count=%d", request.Host, request.URL.RequestURI(), len(request.Header.Values("Sec-WebSocket-Key")))
				}
				if test.name == "iterable in Worker" && strings.Join(request.Header.Values("X-Repeat"), ",") != "one,two" {
					t.Fatalf("duplicate headers=%v", request.Header.Values("X-Repeat"))
				}
			default:
				t.Fatal("server received no handshake")
			}
		})
	}
}

func TestStandardWebSocketBinaryTypesAndExtraArguments(t *testing.T) {
	for _, mode := range []string{"blob", "arraybuffer"} {
		t.Run(mode, func(t *testing.T) {
			requests := make(chan *http.Request, 1)
			server := websocketHeaderFixture(t, false, requests, true)
			defer server.Close()
			endpoint := strings.Replace(server.URL, "http", "ws", 1)
			source := fmt.Sprintf(`
const socket=new WebSocket(%q,"chat.v1",{get headers(){throw Error("nonstandard constructor options read")}});
if(socket.binaryType!=="blob")throw Error("standard binary default");
socket.binaryType=%q;
const message=new Promise(resolve=>{socket.onmessage=event=>resolve(event.data)});
const closed=new Promise((resolve,reject)=>{socket.onclose=resolve;socket.onerror=event=>reject(event.error)});
const data=await message;
await closed;
if(socket.binaryType==="blob"){
 if(!(data instanceof Blob)||(await data.bytes()).join()!=="0,128,255")throw Error("Blob message");
}else if(!(data instanceof ArrayBuffer)||new Uint8Array(data).join()!=="0,128,255")throw Error("ArrayBuffer message");
`, endpoint, mode)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[standard WebSocket]", Virtual: true, Module: true, Code: source}, stdlib.Profile{
				Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }}},
			})
			if err != nil {
				t.Fatal(stdlib.Diagnostic(err))
			}
			if request := <-requests; request.Header.Get("Authorization") != "" {
				t.Fatal("extra constructor arguments supplied handshake headers")
			}
		})
	}
}

func TestStandardWebSocketBlobEchoAndSendOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket Blob echo]", Virtual: true, Module: true, Code: `
import {serve,upgradeWebSocket} from "noe:http";
const server=serve({port:0},request=>{
 const {socket,response}=upgradeWebSocket(request);
 socket.onmessage=event=>socket.send(event.data);
 return response;
});
const socket=new WebSocket(server.url.replace("http","ws"));
const listenerController=new AbortController();let listenerCalls=0;
const messageListener=()=>listenerCalls++;
socket.addEventListener("message",messageListener);socket.addEventListener("message",messageListener);
socket.addEventListener("message",()=>{throw Error("aborted WebSocket listener");},{signal:listenerController.signal});
listenerController.abort();
try{
 const received=[];
 await new Promise((resolve,reject)=>{
  socket.onopen=()=>{socket.send(new Blob([new Uint8Array([0,128,255])]));socket.send("after");};
  socket.onerror=event=>reject(event.error);
  socket.onmessage=event=>{
   received.push(Promise.resolve(event.data).then(async data=>{
    if(typeof data==="string")return data;
    if(!(data instanceof Blob))throw Error("binary message is not Blob");
    return (await data.bytes()).join(",");
   }));
   if(received.length===2)socket.close();
  };
  socket.onclose=resolve;
 });
 if(listenerCalls!==2||(await Promise.all(received)).join("|")!=="0,128,255|after")throw Error("Blob payload, listener identity or send order changed");
}finally{socket.close();await server.close();}
`}, stdlib.Profile{Modules: stdlib.Modules{
		Serve:      &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error { return nil }},
	}})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestWebSocketHeadersRejectInvalidInputBeforePermissionAndKeepValuesPrivate(t *testing.T) {
	var allowed atomic.Int32
	profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error {
		allowed.Add(1)
		return errors.New("denied fixture target")
	}}}}
	code := `import {createWebSocket} from "noe:http";

const endpoint="ws://127.0.0.1:1/",secret="fixture-secret";
const invalid=[1,{headers:1},{headers:"header"},{headers:["ab"]},{headers:[["X-Test"]]},
 {headers:{"bad name":secret}},{headers:{Authorization:secret+"\r\nInjected: value"}},
 {headers:{Authorization:secret+"\u0000"}},{headers:{Authorization:secret+"\u007f"}}];
for(const name of ["hOsT","Connection","Upgrade","Sec-WebSocket-Key","Sec-WebSocket-Version","Sec-WebSocket-Protocol","Sec-WebSocket-Extensions","Content-Length","Transfer-Encoding","Trailer"]){invalid.push({headers:{[name]:secret}})}
for(const options of invalid){let failure;try{createWebSocket(endpoint,options)}catch(error){failure=error}
 if(!(failure instanceof TypeError)||String(failure).includes(secret))throw Error("invalid headers accepted or value exposed")}
const reason={source:"getter"};
try{createWebSocket(endpoint,{get headers(){throw reason}});throw Error("getter accepted")}catch(error){if(error!==reason)throw error}
let closed=false;
const entries={*[Symbol.iterator](){try{yield ["X-Test"]}finally{closed=true}}};
try{createWebSocket(endpoint,{headers:entries});throw Error("malformed iterable accepted")}catch(error){if(!(error instanceof TypeError)||!closed)throw error}
try{new WebSocket(endpoint,["chat\r\nInjected: value"]);throw Error("protocol accepted")}catch(error){if(!(error instanceof SyntaxError))throw error}
`
	_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "[invalid WebSocket headers]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || allowed.Load() != 0 {
		t.Fatalf("error=%s permission calls=%d", stdlib.Diagnostic(err), allowed.Load())
	}
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "[denied WebSocket headers]", Virtual: true, Module: true, Code: `import {createWebSocket} from "noe:http";
createWebSocket("ws://127.0.0.1:1/",{headers:{Authorization:"Bearer fixture"}});`}, profile)
	if err == nil || !strings.Contains(err.Error(), "denied fixture target") || allowed.Load() != 1 {
		t.Fatalf("valid headers bypassed permission: error=%v calls=%d", err, allowed.Load())
	}
}
