package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"time"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func https(ctx context.Context) error {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "NOE consumer"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		return err
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		return err
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	serverTLS := &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: private}}, NextProtos: []string{"h2", "http/1.1"}}
	clientTLS := &tls.Config{RootCAs: roots}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: clientTLS.Clone()}, Timeout: 5 * time.Second}
	defer client.CloseIdleConnections()
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[HTTPS consumer]", Virtual: true, Module: true, Code: `
import {serve, upgradeWebSocket} from "noe:http";
if(await import("noe:fs").then(()=>true,()=>false))throw Error("HTTPS installed FS");
await using server=serve({port:0},async request=>{
 if(request.headers.get("upgrade")==="websocket"){
  const {socket,response}=upgradeWebSocket(request);
  socket.onmessage=event=>socket.send(event.data);
  return response;
 }
 return Response.json({url:request.url,body:await request.text()});
});
if(!server.url.startsWith("https://"))throw Error("HTTPS server URL");
const response=await fetch(server.url+"/echo",{method:"POST",body:"encrypted"});
const record=await response.json();
if(record.url!==server.url+"/echo"||record.body!=="encrypted")throw Error("HTTPS request");
const socket=new WebSocket(server.url.replace(/^https:/,"wss:")+"/ws");
await new Promise((resolve,reject)=>{
 socket.onopen=()=>socket.send(new Blob([new Uint8Array([0,128,255])]));
 socket.onmessage=event=>event.data.bytes().then(bytes=>{
  if(bytes.join()!=="0,128,255"){reject(Error("WSS payload"));return;}
  socket.close(1000,"done");
 },reject);
 socket.onerror=event=>reject(event.error);
 socket.onclose=event=>event.wasClean&&event.code===1000&&event.reason==="done"?resolve():reject(Error("WSS close"));
});
if(await server.close()!==undefined)throw Error("TLS close result");
export default "ok";
`}, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }, TLS: serverTLS},
		Fetch: &stdlib.FetchOptions{Client: client},
		WebSockets: &stdlib.WebSocketsOptions{TLS: clientTLS, Allow: func(target *url.URL) error {
			if target.Scheme != "wss" || target.Hostname() != "127.0.0.1" {
				return fmt.Errorf("unexpected WSS address")
			}
			return nil
		}},
	}})
	if err != nil || string(result.Data) != `"ok"` {
		return fmt.Errorf("HTTPS consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	if len(serverTLS.NextProtos) != 2 || serverTLS.NextProtos[0] != "h2" || serverTLS.NextProtos[1] != "http/1.1" {
		return fmt.Errorf("HTTPS consumer mutated borrowed TLS configuration")
	}
	return rawTLS(ctx, serverTLS.Clone(), clientTLS.Clone())
}
