package stdlib_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type serveTLSMaterial struct {
	certPEM []byte
	keyPEM  []byte
	pair    tls.Certificate
	roots   *x509.CertPool
}

func newServeTLSMaterial(t *testing.T) serveTLSMaterial {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             now.Add(-time.Minute),
		NotAfter:              now.Add(time.Hour),
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	pair, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(leaf)
	return serveTLSMaterial{certPEM: certPEM, keyPEM: keyPEM, pair: pair, roots: roots}
}

func serveTLSFetchClient(t *testing.T, roots *x509.CertPool) *http.Client {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}}}
	t.Cleanup(client.CloseIdleConnections)
	return client
}

func executeServeTLS(t *testing.T, code string, profile stdlib.Profile, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[HTTPS listener]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestServeInlineHTTPSAndHTTPWithAsyncDisposal(t *testing.T) {
	material := newServeTLSMaterial(t)
	client := serveTLSFetchClient(t, material.roots)
	executeServeTLS(t, fmt.Sprintf(`
import {serve} from "noe:http";
let arrive,finish;
const arrived=new Promise(resolve=>{arrive=resolve});
const gate=new Promise(resolve=>{finish=resolve});
let timerProgress=false,port,result;
{
  await using server=serve({hostname:"127.0.0.1",port:0,cert:%q,key:%q},async request=>{
    arrive();
    await gate;
    return new Response(request.url+"|"+await request.text());
  });
  if(!server.url.startsWith("https://"))throw Error("TLS listener URL scheme");
  port=server.port;
  result=fetch(server.url+"/secure?query=1",{method:"POST",body:"payload"}).then(response=>response.text());
  await arrived;
  setTimeout(()=>{timerProgress=true;finish();},0);
}
if(await result!=="https://127.0.0.1:"+port+"/secure?query=1|payload")throw Error("HTTPS Request URL or body");
if(!timerProgress)throw Error("async disposal blocked the runtime timer");
const replacement=serve({hostname:"127.0.0.1",port,cert:%q,key:%q},()=>new Response("reused"));
await replacement.close();

await using plain=serve({port:0},request=>new Response(request.url));
if(!plain.url.startsWith("http://"))throw Error("plain listener URL scheme changed");
if(await(await fetch(plain.url+"/plain")).text()!==plain.url+"/plain")throw Error("plain HTTP regression");
if(typeof WebSocket!=="undefined")throw Error("inline TLS enabled WebSockets");
try{await import("noe:fs");throw Error("inline TLS enabled filesystem");}catch(error){if(error.message==="inline TLS enabled filesystem")throw error;}
`, string(material.certPEM), string(material.keyPEM), string(material.certPEM), string(material.keyPEM)), stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		Fetch: &stdlib.FetchOptions{Client: client},
	}}, 8*time.Second)
}

func TestServeHTTPSConfigurationFailsBeforeAuthorizationAndBind(t *testing.T) {
	material := newServeTLSMaterial(t)
	other := newServeTLSMaterial(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	var allowCalls atomic.Int32
	code := fmt.Sprintf(`
import {serve} from "noe:http";
const validCert=%q,validKey=%q,otherKey=%q;
const badCert="CERTIFICATE_SECRET_FIXTURE",badKey="PRIVATE_KEY_SECRET_FIXTURE";
function rejects(label,options,secrets){
  let error;
  try{serve({...options,hostname:"127.0.0.1",port:%d},()=>new Response("unexpected"));}
  catch(caught){error=caught;}
  if(!error||error.name!=="TypeError")throw Error(label+" did not throw TypeError before bind");
  for(const secret of secrets)if(secret&&error.message.includes(secret))throw Error(label+" exposed PEM input");
}
rejects("missing cert",{key:validKey},[validKey]);
rejects("missing key",{cert:validCert},[validCert]);
rejects("null cert",{cert:null,key:validKey},[validKey]);
rejects("non-string key",{cert:validCert,key:1},[validCert]);
rejects("empty cert",{cert:"",key:validKey},[validKey]);
rejects("malformed PEM",{cert:badCert,key:badKey},[badCert,badKey]);
rejects("mismatched pair",{cert:validCert,key:otherKey},[validCert,otherKey]);
`, string(material.certPEM), string(material.keyPEM), string(other.keyPEM), port)
	executeServeTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { allowCalls.Add(1); return nil }},
	}}, 5*time.Second)
	if got := allowCalls.Load(); got != 0 {
		t.Fatalf("invalid TLS configuration reached Serve.Allow %d times", got)
	}

	emptyGoTLS := fmt.Sprintf(`
import {serve} from "noe:http";
let error;
try{serve({hostname:"127.0.0.1",port:%d},()=>new Response("unexpected"));}
catch(caught){error=caught;}
if(!error||error.name!=="TypeError")throw Error("empty Go TLS config did not throw TypeError before bind");
`, port)
	executeServeTLS(t, emptyGoTLS, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { allowCalls.Add(1); return nil }, TLS: &tls.Config{}},
	}}, 5*time.Second)
	if got := allowCalls.Load(); got != 0 {
		t.Fatalf("empty Go TLS configuration reached Serve.Allow %d times", got)
	}
}

func TestServeHostTLSConfigAndWorkerInheritance(t *testing.T) {
	material := newServeTLSMaterial(t)
	client := serveTLSFetchClient(t, material.roots)
	original := &tls.Config{
		Certificates: []tls.Certificate{material.pair},
		NextProtos:   []string{"h2"},
		MinVersion:   tls.VersionTLS12,
	}
	var allowCalls atomic.Int32
	workerSource := `import {parentPort} from "node:worker_threads";
import {serve} from "noe:http";
const server=serve({port:0},request=>new Response(request.url));
parentPort.postMessage(server.port);
await new Promise(()=>{});`
	code := fmt.Sprintf(`
import {serve} from "noe:http";
import {Worker} from "node:worker_threads";
let conflict,created;
try{created=serve({port:0,cert:%q,key:%q},()=>new Response("unexpected"));}
catch(error){conflict=error;}
if(created){await created.close();throw Error("inline PEM overrode host TLS");}
if(!conflict||conflict.name!=="TypeError")throw Error("host TLS conflict did not throw TypeError");
if(conflict.message.includes(%q)||conflict.message.includes(%q))throw Error("host TLS conflict exposed PEM input");

await using server=serve({port:0},request=>new Response(request.url));
if(!server.url.startsWith("https://"))throw Error("Go TLS did not select HTTPS by default");
if(await(await fetch(server.url+"/host")).text()!==server.url+"/host")throw Error("host TLS request failed");

const worker=new Worker(%q,{eval:true,type:"module"});
const port=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});
const workerURL="https://127.0.0.1:"+port;
if(await(await fetch(workerURL+"/worker")).text()!==workerURL+"/worker")throw Error("Worker did not inherit host TLS configuration");
await worker.terminate();
const replacement=serve({port},()=>new Response("reused"));
await replacement.close();
`, string(material.certPEM), string(material.keyPEM), string(material.certPEM), string(material.keyPEM), workerSource)
	executeServeTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{
		Serve:   &stdlib.ServeOptions{Allow: func(string) error { allowCalls.Add(1); return nil }, TLS: original},
		Fetch:   &stdlib.FetchOptions{Client: client},
		Workers: &stdlib.WorkerOptions{Max: 1},
	}}, 8*time.Second)
	if got := allowCalls.Load(); got != 3 {
		t.Fatalf("Serve.Allow calls=%d, want only the three valid listeners", got)
	}
	if !reflect.DeepEqual(original.NextProtos, []string{"h2"}) || original.MinVersion != tls.VersionTLS12 {
		t.Fatalf("serving mutated the caller TLS config: NextProtos=%v MinVersion=%d", original.NextProtos, original.MinVersion)
	}
}

func TestServeTLSCallbackConfigNormalizesALPNWithoutMutation(t *testing.T) {
	material := newServeTLSMaterial(t)
	var configCalls, certificateCalls atomic.Int32
	selected := &tls.Config{
		NextProtos: []string{"h2"},
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			certificateCalls.Add(1)
			return &material.pair, nil
		},
	}
	original := &tls.Config{
		NextProtos: []string{"h2"},
		GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) {
			configCalls.Add(1)
			return selected, nil
		},
	}
	negotiated := make(chan string, 1)
	client := &http.Client{Transport: &http.Transport{
		ForceAttemptHTTP2: true,
		TLSClientConfig: &tls.Config{
			RootCAs:    material.roots,
			MinVersion: tls.VersionTLS12,
			VerifyConnection: func(state tls.ConnectionState) error {
				select {
				case negotiated <- state.NegotiatedProtocol:
				default:
				}
				return nil
			},
		},
	}}
	t.Cleanup(client.CloseIdleConnections)
	executeServeTLS(t, `
import {serve} from "noe:http";
await using server=serve({port:0},()=>new Response("callback TLS"));
if(await(await fetch(server.url)).text()!=="callback TLS")throw Error("TLS callback configuration failed");
`, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }, TLS: original},
		Fetch: &stdlib.FetchOptions{Client: client},
	}}, 5*time.Second)
	if configCalls.Load() == 0 || certificateCalls.Load() == 0 {
		t.Fatalf("TLS callbacks not called: GetConfigForClient=%d GetCertificate=%d", configCalls.Load(), certificateCalls.Load())
	}
	select {
	case protocol := <-negotiated:
		if protocol != "http/1.1" {
			t.Fatalf("negotiated ALPN=%q, want http/1.1 after HTTP/2 attempt", protocol)
		}
	default:
		t.Fatal("client TLS verification callback did not run")
	}
	if !reflect.DeepEqual(original.NextProtos, []string{"h2"}) || !reflect.DeepEqual(selected.NextProtos, []string{"h2"}) {
		t.Fatalf("TLS callback configs were mutated: root NextProtos=%v selected NextProtos=%v", original.NextProtos, selected.NextProtos)
	}
}

func TestServeTLSConfigIsSnapshottedWhenRuntimeIsCreated(t *testing.T) {
	material := newServeTLSMaterial(t)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    material.roots,
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS12,
	}}}
	t.Cleanup(client.CloseIdleConnections)
	original := &tls.Config{
		Certificates: []tls.Certificate{material.pair},
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	runtime, err := stdlib.NewRuntime(ctx, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }, TLS: original},
		Fetch: &stdlib.FetchOptions{Client: client},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := runtime.Close(); err != nil {
			t.Errorf("close runtime: %v", err)
		}
	}()

	// NewRuntime snapshots profile TLS settings. The caller can change its
	// configuration afterwards without changing the listener already selected.
	original.MinVersion = tls.VersionTLS13
	original.MaxVersion = tls.VersionTLS13
	result, err := runtime.Run(stdlib.Source{Name: "[HTTPS TLS config snapshot]", Virtual: true, Module: true, Code: `
import {serve} from "noe:http";
await using server=serve({port:0},()=>new Response("TLS 1.2 snapshot"));
if(await(await fetch(server.url)).text()!=="TLS 1.2 snapshot")throw Error("runtime TLS snapshot changed");
`})
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestServeWSSUpgradeEchoesBlobAndTextAndClosesCleanly(t *testing.T) {
	material := newServeTLSMaterial(t)
	code := fmt.Sprintf(`
import {serve,upgradeWebSocket} from "noe:http";
await using server=serve({port:0,cert:%q,key:%q},request=>{
  const {socket,response}=upgradeWebSocket(request);
  socket.addEventListener("message",event=>socket.send(event.data));
  return response;
});
const socket=new WebSocket(server.url.replace(/^https:/,"wss:"));
socket.binaryType="blob";
let binary=false,text=false;
await new Promise((resolve,reject)=>{
  socket.addEventListener("open",()=>{
    socket.send(new Blob([new Uint8Array([0,1,255,42])],{type:"application/octet-stream"}));
    socket.send("text payload");
  });
  socket.addEventListener("error",()=>reject(Error("WSS connection failed")));
  socket.addEventListener("message",async event=>{
    if(event.data instanceof Blob){
      const bytes=[...new Uint8Array(await event.data.arrayBuffer())];
      if(bytes.join(",")!=="0,1,255,42")return reject(Error("WSS binary Blob changed"));
      binary=true;
    }else if(event.data==="text payload")text=true;
    else return reject(Error("unexpected WSS message"));
    if(binary&&text)socket.close(1000,"done");
  });
  socket.addEventListener("close",event=>{
    if(!event.wasClean||event.code!==1000||event.reason!=="done")return reject(Error("WSS close was not clean"));
    resolve();
  });
});
if(!binary||!text)throw Error("WSS echo messages missing");
`, string(material.certPEM), string(material.keyPEM))
	executeServeTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		WebSockets: &stdlib.WebSocketsOptions{
			Allow: func(target *url.URL) error {
				if target.Scheme != "wss" || target.Host == "" {
					return fmt.Errorf("unexpected WSS target %q", target)
				}
				return nil
			},
			TLS: &tls.Config{RootCAs: material.roots},
		},
	}}, 8*time.Second)
}

func TestServeRuntimeCancellationClosesStalledTLSHandshake(t *testing.T) {
	material := newServeTLSMaterial(t)
	ctx, cancel := context.WithCancel(t.Context())
	address := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[cancel HTTPS listener]", Virtual: true, Module: true, Code: `
import {serve} from "noe:http";
const server=serve({port:0},()=>new Response("unexpected"));
notifyAddress(server.url);
await new Promise(()=>{});
`}, stdlib.Profile{
			Modules: stdlib.Modules{Serve: &stdlib.ServeOptions{
				Allow: func(string) error { return nil },
				TLS:   &tls.Config{Certificates: []tls.Certificate{material.pair}},
			}},
			Initialize: func(_ context.Context, bindings *stdlib.Bindings) (func(), error) {
				return nil, bindings.Runtime().Set("notifyAddress", func(value string) { address <- value })
			},
		})
		done <- err
	}()
	defer cancel()

	var serverURL string
	select {
	case serverURL = <-address:
	case err := <-done:
		t.Fatalf("server exited before startup: %s", stdlib.Diagnostic(err))
	case <-time.After(2 * time.Second):
		t.Fatal("HTTPS listener did not start")
	}
	target, err := url.Parse(serverURL)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", target.Host, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	// A TLS record header without its payload leaves the accepted connection
	// waiting inside the handshake until execution cleanup closes the listener.
	if _, err := conn.Write([]byte{0x16, 0x03, 0x01, 0x00, 0x10}); err != nil {
		t.Fatalf("start partial TLS handshake: %v", err)
	}
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancellation unexpectedly succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("runtime cancellation left the TLS handshake waiting")
	}
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Read(make([]byte, 1)); err == nil {
		t.Fatal("runtime cancellation left the TLS peer open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("TLS peer remained blocked after runtime cancellation")
	}
	listener, err := net.Listen("tcp", target.Host)
	if err != nil {
		t.Fatalf("runtime cancellation did not release listener %s: %v", target.Host, err)
	}
	listener.Close()
}
