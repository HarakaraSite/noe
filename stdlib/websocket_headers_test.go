package stdlib_test

import (
	"context"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func websocketHeaderFixture(t *testing.T, secure bool, requests chan<- *http.Request) *httptest.Server {
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
		// One text message, followed by a normal close frame.
		rw.Write([]byte{0x81, 2, 'o', 'k', 0x88, 2, 3, 232})
		if err := rw.Flush(); err != nil {
			t.Error(err)
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
			server := websocketHeaderFixture(t, test.secure, requests)
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
			code := fmt.Sprintf(`
const headers=%s;
const socket=new WebSocket(%q,["chat.v1","other"],{headers});
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

func TestWebSocketHeadersRejectInvalidInputBeforePermissionAndKeepValuesPrivate(t *testing.T) {
	var allowed atomic.Int32
	profile := stdlib.Profile{Modules: stdlib.Modules{WebSockets: &stdlib.WebSocketsOptions{Allow: func(*url.URL) error {
		allowed.Add(1)
		return errors.New("denied fixture target")
	}}}}
	code := `
const endpoint="ws://127.0.0.1:1/",secret="fixture-secret";
const invalid=[1,{headers:1},{headers:"header"},{headers:["ab"]},{headers:[["X-Test"]]},
 {headers:{"bad name":secret}},{headers:{Authorization:secret+"\r\nInjected: value"}},
 {headers:{Authorization:secret+"\u0000"}},{headers:{Authorization:secret+"\u007f"}}];
for(const name of ["hOsT","Connection","Upgrade","Sec-WebSocket-Key","Sec-WebSocket-Version","Sec-WebSocket-Protocol","Sec-WebSocket-Extensions","Content-Length","Transfer-Encoding","Trailer"]){invalid.push({headers:{[name]:secret}})}
for(const options of invalid){let failure;try{new WebSocket(endpoint,undefined,options)}catch(error){failure=error}
 if(!(failure instanceof TypeError)||String(failure).includes(secret))throw Error("invalid headers accepted or value exposed")}
const reason={source:"getter"};
try{new WebSocket(endpoint,undefined,{get headers(){throw reason}});throw Error("getter accepted")}catch(error){if(error!==reason)throw error}
let closed=false;
const entries={*[Symbol.iterator](){try{yield ["X-Test"]}finally{closed=true}}};
try{new WebSocket(endpoint,undefined,{headers:entries});throw Error("malformed iterable accepted")}catch(error){if(!(error instanceof TypeError)||!closed)throw error}
try{new WebSocket(endpoint,["chat\r\nInjected: value"]);throw Error("protocol accepted")}catch(error){if(!(error instanceof SyntaxError))throw error}
`
	_, err := stdlib.Execute(t.Context(), stdlib.Source{Name: "[invalid WebSocket headers]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || allowed.Load() != 0 {
		t.Fatalf("error=%s permission calls=%d", stdlib.Diagnostic(err), allowed.Load())
	}
	_, err = stdlib.Execute(t.Context(), stdlib.Source{Name: "[denied WebSocket headers]", Virtual: true, Module: true, Code: `new WebSocket("ws://127.0.0.1:1/",undefined,{headers:{Authorization:"Bearer fixture"}});`}, profile)
	if err == nil || !strings.Contains(err.Error(), "denied fixture target") || allowed.Load() != 1 {
		t.Fatalf("valid headers bypassed permission: error=%v calls=%d", err, allowed.Load())
	}
}
