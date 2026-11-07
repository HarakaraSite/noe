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
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type rawTLSMaterial struct {
	certPEM []byte
	keyPEM  []byte
	pair    tls.Certificate
	roots   *x509.CertPool
}

func newRawTLSMaterial(t *testing.T) rawTLSMaterial {
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
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
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
	return rawTLSMaterial{certPEM: certPEM, keyPEM: keyPEM, pair: pair, roots: roots}
}

func executeRawTLS(t *testing.T, code string, profile stdlib.Profile, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), timeout)
	defer cancel()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[raw TLS sockets]", Virtual: true, Module: true, Code: code}, profile)
	if err != nil || result.Code != 0 {
		t.Fatalf("result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
}

func TestRawTLSInlineStreamsAuthenticateAndHalfClose(t *testing.T) {
	material := newRawTLSMaterial(t)
	code := fmt.Sprintf(`
import {connect,listen} from "noe:sockets";
if(typeof fetch!=="undefined")throw Error("raw sockets selected Fetch");
if(typeof WebSocket!=="undefined")throw Error("raw sockets selected WebSockets");
try{await import("noe:fs");throw Error("raw sockets selected filesystem");}catch(error){if(error.message==="raw sockets selected filesystem")throw error;}

async function readAll(stream){
  const reader=stream.getReader(),bytes=[];
  try{for(;;){const item=await reader.read();if(item.done)break;bytes.push(...item.value);}}
  finally{reader.releaseLock();}
  return bytes;
}
const listener=await listen({network:"tls",host:"127.0.0.1",port:0,cert:%q,key:%q});
if(listener.address.network!=="tls")throw Error("TLS listener address lost its network");
const accepting=listener.accept();
const controller=new AbortController();
const client=await connect({network:"tls",host:"127.0.0.1",port:listener.address.port,serverName:"localhost",ca:%q,signal:controller.signal});
controller.abort(new Error("abort after ownership transfer"));
const server=await accepting;
const clientWriter=client.writable.getWriter();
const serverWriter=server.writable.getWriter();
const request=readAll(server.readable);
await clientWriter.write(new Uint8Array([0,255,65]));
await clientWriter.write(new TextEncoder().encode("request text"));
await clientWriter.close();
clientWriter.releaseLock();
const requestBytes=await request;
if(requestBytes.join(",")!=="0,255,65,114,101,113,117,101,115,116,32,116,101,120,116")throw Error("TLS request bytes changed: "+requestBytes);

const response=readAll(client.readable);
await serverWriter.write(new Uint8Array([9,0,128]));
await serverWriter.write(new TextEncoder().encode("response text"));
await serverWriter.close();
serverWriter.releaseLock();
const responseBytes=await response;
if(responseBytes.join(",")!=="9,0,128,114,101,115,112,111,110,115,101,32,116,101,120,116")throw Error("TLS response bytes changed: "+responseBytes);
await listener.close();
await client.close();
await server.close();
`, string(material.certPEM), string(material.keyPEM), string(material.certPEM))
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestRawTLSRejectsUntrustedAndWrongHostnameClients(t *testing.T) {
	material := newRawTLSMaterial(t)
	code := fmt.Sprintf(`
import {connect,listen} from "noe:sockets";
const listener=await listen({network:"tls",host:"127.0.0.1",port:0,cert:%q,key:%q});
async function rejected(label,options){
  const accepting=listener.accept().then(value=>({value}),error=>({error}));
  let connection,error;
  try{connection=await connect(options);}catch(caught){error=caught;}
  if(connection){await connection.close();throw Error(label+" unexpectedly authenticated");}
  if(!error)throw Error(label+" did not reject");
  const server=await accepting;
  if(server.value)await server.value.close();
}
await rejected("untrusted certificate",{network:"tls",host:"127.0.0.1",port:listener.address.port});
await rejected("hostname mismatch",{network:"tls",host:"127.0.0.1",port:listener.address.port,serverName:"wrong.invalid",ca:%q});
const accepting=listener.accept();
const trusted=await connect({network:"tls",host:"127.0.0.1",port:listener.address.port,serverName:"localhost",ca:%q});
const accepted=await accepting;
await trusted.close();
await accepted.close();
await listener.close();
`, string(material.certPEM), string(material.keyPEM), string(material.certPEM), string(material.certPEM))
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestRawTLSOptionValidationPrecedesBindAndDial(t *testing.T) {
	material := newRawTLSMaterial(t)
	other := newRawTLSMaterial(t)
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	code := fmt.Sprintf(`
import {connect,listen} from "noe:sockets";
async function typeError(label,operation,secrets=[]){
  let value,error;
  try{value=await operation();}catch(caught){error=caught;}
  if(value&&typeof value.close==="function")await value.close();
  if(!error||error.name!=="TypeError")throw Error(label+" did not reject with TypeError"+(error?": "+error:""));
  for(const secret of secrets)if(error.message.includes(secret))throw Error(label+" exposed TLS input");
}
const host="127.0.0.1",port=%d;
const cert=%q,key=%q,otherKey=%q;
const certSecret="TLS_CERTIFICATE_SECRET",keySecret="TLS_PRIVATE_KEY_SECRET",caSecret="TLS_CA_SECRET";
await typeError("missing listener identity",()=>listen({network:"tls",host,port}));
await typeError("missing key",()=>listen({network:"tls",host,port,cert}),[cert]);
await typeError("missing certificate",()=>listen({network:"tls",host,port,key}),[key]);
await typeError("malformed identity",()=>listen({network:"tls",host,port,cert:certSecret,key:keySecret}),[certSecret,keySecret]);
await typeError("mismatched identity",()=>listen({network:"tls",host,port,cert,key:otherKey}),[cert,otherKey]);
await typeError("malformed CA",()=>connect({network:"tls",host,port:1,ca:caSecret}),[caSecret]);
await typeError("empty CA",()=>connect({network:"tls",host,port:1,ca:""}));
await typeError("empty server name",()=>connect({network:"tls",host,port:1,serverName:""}));
await typeError("listener CA is client-only",()=>listen({network:"tls",host,port,cert,key,ca:caSecret}),[caSecret]);
await typeError("listener serverName is client-only",()=>listen({network:"tls",host,port,cert,key,serverName:"localhost"}));

for(const network of ["tcp","unix"]){
  const base=network==="tcp"?{network,host,port:1}:{network,path:"/tmp/noe-raw-tls-never-created"};
  for(const [field,value] of [["cert",certSecret],["key",keySecret],["ca",caSecret],["serverName","secret.invalid"]]){
    await typeError(network+" connect "+field,()=>connect({...base,[field]:value}),[value]);
    await typeError(network+" listen "+field,()=>listen({...base,[field]:value}),[value]);
  }
}
`, port, string(material.certPEM), string(material.keyPEM), string(other.keyPEM))
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestRawTLSHostMutualTLSConfigIsAuthoritativeAndSnapshotted(t *testing.T) {
	material := newRawTLSMaterial(t)
	clientConfig := &tls.Config{
		RootCAs:      material.roots,
		ServerName:   "localhost",
		Certificates: []tls.Certificate{material.pair},
		MinVersion:   tls.VersionTLS12,
	}
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{material.pair},
		ClientCAs:    material.roots,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	code := fmt.Sprintf(`
import {connect,listen} from "noe:sockets";
async function typeError(label,operation,secrets){
  let value,error;
  try{value=await operation();}catch(caught){error=caught;}
  if(value&&typeof value.close==="function")await value.close();
  if(!error||error.name!=="TypeError")throw Error(label+" did not reject with TypeError");
  for(const secret of secrets)if(error.message.includes(secret))throw Error(label+" exposed inline TLS input");
}
const host="127.0.0.1";
const listener=await listen({network:"tls",host,port:0});
await typeError("host server configuration",()=>listen({network:"tls",host,port:0,cert:"HOST_SERVER_CERT_SECRET",key:"HOST_SERVER_KEY_SECRET"}),["HOST_SERVER_CERT_SECRET","HOST_SERVER_KEY_SECRET"]);
const accepting=listener.accept();
await typeError("host client configuration",()=>connect({network:"tls",host,port:listener.address.port,cert:"HOST_CLIENT_CERT_SECRET",key:"HOST_CLIENT_KEY_SECRET"}),["HOST_CLIENT_CERT_SECRET","HOST_CLIENT_KEY_SECRET"]);
const client=await connect({network:"tls",host,port:listener.address.port});
const server=await accepting;
const read=server.readable.getReader();
const writer=client.writable.getWriter();
const received=read.read();
await writer.write(new Uint8Array([77,84,76,83]));
await writer.close();
writer.releaseLock();
const chunk=await received;
if(chunk.done||Array.from(chunk.value).join(",")!=="77,84,76,83")throw Error("mutual TLS stream failed");
await read.cancel();
read.releaseLock();
await listener.close();
await client.close();
await server.close();
`)
	profile := stdlib.Profile{
		Modules: stdlib.Modules{
			RawSockets: true,
			RawSocketsTLS: &stdlib.SocketTLSOptions{
				Client: clientConfig,
				Server: serverConfig,
			},
		},
		Initialize: func(context.Context, *stdlib.Bindings) (func(), error) {
			// These changes happen after the profile snapshot. The active execution
			// must keep the original mutual-TLS identity and verification policy.
			clientConfig.ServerName = "changed.invalid"
			clientConfig.Certificates = nil
			clientConfig.RootCAs = x509.NewCertPool()
			serverConfig.Certificates = nil
			serverConfig.ClientAuth = tls.NoClientCert
			serverConfig.ClientCAs = x509.NewCertPool()
			return nil, nil
		},
	}
	executeRawTLS(t, code, profile, 6*time.Second)
}

func TestRawTLSWorkerTerminationReclaimsInheritedHandshakeAndListener(t *testing.T) {
	material := newRawTLSMaterial(t)
	handshakeStarted := make(chan struct{})
	handshakeCanceled := make(chan struct{})
	var startedOnce, canceledOnce sync.Once
	serverConfig := &tls.Config{
		Certificates: []tls.Certificate{material.pair},
		GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
			startedOnce.Do(func() { close(handshakeStarted) })
			<-hello.Context().Done()
			canceledOnce.Do(func() { close(handshakeCanceled) })
			return nil, hello.Context().Err()
		},
	}
	clientConfig := &tls.Config{RootCAs: material.roots, ServerName: "localhost"}
	workerSource := `
import {parentPort} from "node:worker_threads";
import {listen} from "noe:sockets";
const listener=await listen({network:"tls",host:"127.0.0.1",port:0});
parentPort.postMessage(listener.address.port);
parentPort.on("message",value=>{
  if(value!=="accept")return;
  const pending=listener.accept();
  parentPort.postMessage("accept-started");
  pending.then(async connection=>{await connection.close();parentPort.postMessage("unexpected-accept");},()=>parentPort.postMessage("accept-rejected"));
});
await new Promise(()=>{});
`
	code := fmt.Sprintf(`
import {Worker} from "node:worker_threads";
import {connect,listen} from "noe:sockets";
const worker=new Worker(%q,{eval:true,type:"module"});
const port=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});
const pending=connect({network:"tls",host:"127.0.0.1",port}).then(connection=>({connection}),error=>({error}));
const accepting=new Promise((resolve,reject)=>{
  const onMessage=value=>{if(value==="accept-started")resolve();else if(value==="unexpected-accept")reject(Error("handshake unexpectedly completed"));};
  worker.on("message",onMessage);
  worker.once("error",reject);
});
worker.postMessage("accept");
await accepting;
pauseForHandshakeCleanup();
await worker.terminate();
const outcome=await pending;
if(outcome.connection){await outcome.connection.close();throw Error("connect survived Worker termination");}
const replacement=await listen({network:"tls",host:"127.0.0.1",port});
await replacement.close();
`, workerSource)
	profile := stdlib.Profile{
		Modules: stdlib.Modules{
			RawSockets: true,
			RawSocketsTLS: &stdlib.SocketTLSOptions{
				Client: clientConfig,
				Server: serverConfig,
				// Keep the callback stalled until Worker cancellation exercises
				// the handshake's owner context.
				HandshakeTimeout: -1,
			},
			Workers: &stdlib.WorkerOptions{Max: 1},
		},
		Initialize: func(ctx context.Context, bindings *stdlib.Bindings) (func(), error) {
			if bindings.Worker() {
				return nil, nil
			}
			return nil, bindings.Runtime().Set("pauseForHandshakeCleanup", func() {
				select {
				case <-handshakeStarted:
				case <-ctx.Done():
				}
			})
		},
	}
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	done := make(chan struct{})
	var result stdlib.Result
	var executionErr error
	go func() {
		result, executionErr = stdlib.Execute(ctx, stdlib.Source{Name: "[raw TLS Worker cleanup]", Virtual: true, Module: true, Code: code}, profile)
		close(done)
	}()
	select {
	case <-handshakeStarted:
	case <-done:
		t.Fatalf("execution ended before a Worker TLS handshake: result=%+v error=%s", result, stdlib.Diagnostic(executionErr))
	case <-ctx.Done():
		t.Fatal("Worker TLS handshake did not start before the deadline")
	}
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatalf("Worker termination left execution blocked: result=%+v error=%s", result, stdlib.Diagnostic(executionErr))
	}
	if executionErr != nil || result.Code != 0 {
		t.Fatalf("Worker TLS cleanup result=%+v error=%s", result, stdlib.Diagnostic(executionErr))
	}
	select {
	case <-handshakeCanceled:
	case <-ctx.Done():
		t.Fatal("Worker termination did not cancel the TLS handshake context")
	}
}

func TestRawTLSListenerCloseAbortsPendingWorkButKeepsEstablishedConnections(t *testing.T) {
	material := newRawTLSMaterial(t)
	code := fmt.Sprintf(`
import {connect,listen} from "noe:sockets";
const listener=await listen({network:"tls",host:"127.0.0.1",port:0,cert:%q,key:%q});
const accepting=listener.accept();
const client=await connect({network:"tls",host:"127.0.0.1",port:listener.address.port,serverName:"localhost",ca:%q});
const server=await accepting;
function settled(promise){return promise.then(value=>({value}),error=>({error}));}
const waitingAccept=settled(listener.accept());
const stalledHandshake=settled(listener.accept());
const raw=await connect({network:"tcp",host:"127.0.0.1",port:listener.address.port});
await listener.close();
const [waiting,handshake]=await Promise.all([waitingAccept,stalledHandshake]);
if(!waiting.error||!handshake.error)throw Error("listener close did not reject pending accepts/handshake");
await raw.close();

const serverReader=server.readable.getReader();
const clientWriter=client.writable.getWriter();
const received=serverReader.read();
await clientWriter.write(new Uint8Array([1,2,3,255]));
const chunk=await received;
if(chunk.done||Array.from(chunk.value).join(",")!=="1,2,3,255")throw Error("established TLS connection did not survive listener close");
const serverWriter=server.writable.getWriter();
const clientReader=client.readable.getReader();
const response=clientReader.read();
await serverWriter.write(new TextEncoder().encode("still open"));
if(new TextDecoder().decode((await response).value)!=="still open")throw Error("established TLS response failed after listener close");
await serverReader.cancel();
await clientReader.cancel();
serverReader.releaseLock();
clientReader.releaseLock();
clientWriter.releaseLock();
await client.close();
await server.close();
`, string(material.certPEM), string(material.keyPEM), string(material.certPEM))
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}}, 6*time.Second)
}

func TestRawTLSRuntimeCancellationInterruptsStalledConnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()
	address := listener.Addr().(*net.TCPAddr)
	code := fmt.Sprintf(`
import {connect} from "noe:sockets";
await connect({network:"tls",host:"127.0.0.1",port:%d});
`, address.Port)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	var result stdlib.Result
	var executionErr error
	go func() {
		result, executionErr = stdlib.Execute(ctx, stdlib.Source{Name: "[stalled raw TLS connect]", Virtual: true, Module: true, Code: code}, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}})
		close(done)
	}()
	var peer net.Conn
	select {
	case peer = <-accepted:
		defer peer.Close()
	case err := <-acceptErr:
		cancel()
		t.Fatalf("accept stalled TLS connect: %v", err)
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("TLS connect did not establish its TCP transport")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runtime cancellation did not interrupt the TLS handshake")
	}
	if executionErr == nil && result.Code == 0 {
		t.Fatalf("stalled TLS connect unexpectedly succeeded: %+v", result)
	}
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 2048)
	for {
		_, err := peer.Read(buffer)
		if err == nil {
			continue // The client may have sent its TLS ClientHello before cancellation.
		}
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("runtime cancellation left the TLS transport open")
		}
		break
	}
}

func TestRawTLSOptionsAloneDoNotSelectSocketsOrOtherModules(t *testing.T) {
	code := `
if(typeof fetch!=="undefined")throw Error("RawSocketsTLS selected Fetch");
if(typeof WebSocket!=="undefined")throw Error("RawSocketsTLS selected WebSockets");
async function absent(specifier){let missing=false;try{await import(specifier);}catch{missing=true;}if(!missing)throw Error(specifier+" was selected implicitly");}
await absent("noe:sockets");
await absent("noe:fs");
`
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSocketsTLS: &stdlib.SocketTLSOptions{}}}, 4*time.Second)
}

func TestRawTLSConnectAbortSignalAndHandshakeTimeout(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	hello := make(chan struct{}, 2)
	closed := make(chan error, 2)
	go func() {
		for range 2 {
			conn, err := listener.Accept()
			if err != nil {
				closed <- err
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := io.ReadFull(conn, make([]byte, 5)); err != nil {
					closed <- err
					return
				}
				hello <- struct{}{}
				_, err := io.Copy(io.Discard, conn)
				closed <- err
			}()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	code := fmt.Sprintf(`
import {connect} from "noe:sockets";
const controller=new AbortController(),reason={kind:"TLS connect canceled"};
const pending=connect({network:"tls",host:"127.0.0.1",port:%d,signal:controller.signal}).then(connection=>({connection}),error=>({error}));
waitForClientHello();
controller.abort(reason);
const outcome=await pending;
if(outcome.connection){await outcome.connection.close();throw Error("aborted TLS connect succeeded");}
if(outcome.error!==reason)throw Error("AbortSignal reason was not preserved");
`, port)
	executeRawTLS(t, code, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}, Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
		return nil, b.Runtime().Set("waitForClientHello", func() {
			select {
			case <-hello:
			case <-ctx.Done():
			}
		})
	}}, 3*time.Second)
	timeoutCode := fmt.Sprintf(`
import {connect} from "noe:sockets";
let error;
try{const connection=await connect({network:"tls",host:"127.0.0.1",port:%d});await connection.close();}
catch(caught){error=caught;}
if(!error||!error.message.includes("deadline exceeded"))throw Error("handshake timeout did not reject the pending connection");
`, port)
	executeRawTLS(t, timeoutCode, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true, RawSocketsTLS: &stdlib.SocketTLSOptions{HandshakeTimeout: 50 * time.Millisecond}}}, 3*time.Second)
	for range 2 {
		select {
		case err := <-closed:
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("TLS abort/timeout left its TCP transport open")
			}
		case <-time.After(time.Second):
			t.Fatal("TLS abort/timeout did not close its TCP transport")
		}
	}
}
