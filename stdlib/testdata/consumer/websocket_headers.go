package main

import (
	"context"
	"fmt"
	"net/url"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func websocketHeaders(ctx context.Context) error {
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket consumer]", Virtual: true, Module: true, Code: `import {createWebSocket} from "noe:http";
import {serve,upgradeWebSocket} from "noe:http";

const server=serve({hostname:"127.0.0.1",port:0},request=>{
 if(request.headers.get("authorization")!=="Bearer fixture")return new Response("unauthorized",{status:401});
 const {socket,response}=upgradeWebSocket(request,{protocol:"chat.v1"});
 socket.onopen=()=>socket.send("ok");
 socket.onmessage=event=>{if(event.data instanceof Blob)socket.send(event.data);else socket.close(1000,"done")};
 return response;
});
try{
 const socket=createWebSocket(server.url.replace("http","ws"),{protocols:"chat.v1",headers:{Authorization:"Bearer fixture"}});
 let received=false,binary=false;
 await new Promise((resolve,reject)=>{
  socket.onmessage=event=>{
   if(event.data==="ok"){received=true;socket.send(new Blob([new Uint8Array([0,128,255])]));}
   else if(event.data instanceof Blob)event.data.bytes().then(bytes=>{
    if(bytes.join()!=="0,128,255"){reject(Error("Blob consumer bytes"));return;}
    binary=true;socket.send("done");
   },reject);
   else reject(Error("Blob consumer type"));
  };
  socket.onerror=event=>reject(event.error);
  socket.onclose=event=>received&&binary&&event.wasClean&&event.code===1000&&socket.protocol==="chat.v1"?resolve():reject(Error("WebSocket consumer failed"));
 });
}finally{await server.close()}
if(typeof fetch!=="undefined")throw Error("WebSocket consumer enabled fetch");
export default "ok";`}, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{Allow: func(string) error { return nil }},
		WebSockets: &stdlib.WebSocketsOptions{Allow: func(target *url.URL) error {
			if target.Scheme != "ws" || target.Hostname() != "127.0.0.1" {
				return fmt.Errorf("unexpected WebSocket target")
			}
			return nil
		}},
	}})
	if err != nil || string(result.Data) != `"ok"` {
		return fmt.Errorf("WebSocket consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
