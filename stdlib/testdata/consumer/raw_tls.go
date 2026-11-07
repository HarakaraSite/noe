package main

import (
	"context"
	"crypto/tls"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func rawTLS(ctx context.Context, serverTLS, clientTLS *tls.Config) error {
	serverTLS.NextProtos = []string{"noe.raw.v1"}
	clientTLS.NextProtos = []string{"noe.raw.v1"}
	clientTLS.VerifyConnection = func(state tls.ConnectionState) error {
		if state.NegotiatedProtocol != "noe.raw.v1" {
			return fmt.Errorf("raw TLS ALPN was not preserved")
		}
		return nil
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[raw TLS consumer]", Virtual: true, Module: true, Code: `
import {listen,connect} from "noe:sockets";
if(typeof fetch!=="undefined"||typeof WebSocket!=="undefined")throw Error("raw TLS granted Web networking");
if(await import("noe:fs").then(()=>true,()=>false))throw Error("raw TLS granted FS");
await using listener=await listen({network:"tls",host:"127.0.0.1",port:0});
if(listener.address.network!=="tls")throw Error("TLS listener address");
const accepting=listener.accept();
await using client=await connect(listener.address);
await using server=await accepting;
await listener.close();
const response=new Response(client.readable).bytes();
const request=new Response(server.readable).bytes();
const writer=client.writable.getWriter();
await writer.write(new Uint8Array([0,128,255,42]));
await writer.close();writer.releaseLock();
const bytes=await request;
if(bytes.join()!=="0,128,255,42")throw Error("TLS request bytes or close_notify");
const reply=server.writable.getWriter();
await reply.write(bytes);await reply.close();reply.releaseLock();
if((await response).join()!=="0,128,255,42")throw Error("TLS half-close reply");
export default "raw TLS consumer passed";
`}, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true, RawSocketsTLS: &stdlib.SocketTLSOptions{Client: clientTLS, Server: serverTLS}}})
	if err != nil || string(result.Data) != `"raw TLS consumer passed"` {
		return fmt.Errorf("raw TLS consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
