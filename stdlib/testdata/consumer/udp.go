package main

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func udp(ctx context.Context) error {
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[UDP consumer]", Virtual: true, Module: true, Code: `
import {bind} from "noe:sockets";
if(typeof fetch!=="undefined"||typeof WebSocket!=="undefined")throw Error("UDP selected Web networking");
if(await import("noe:fs").then(()=>true,()=>false))throw Error("UDP selected FS");
await using first=await bind({network:"udp",host:"127.0.0.1",port:0});
await using second=await bind({network:"udp",host:"127.0.0.1",port:0});
for(const data of [new Uint8Array([0,128,255,42]),new Uint8Array(),new TextEncoder().encode("UDP consumer")]){
 const received=second.receive();
 const sent=await first.send(data,second.address);
 const packet=await received;
 if(sent!==data.length||packet.data.join()!==data.join())throw Error("UDP packet boundary or bytes");
 if(packet.remoteAddress.port!==first.address.port||packet.remoteAddress.host!==first.address.host||packet.remoteAddress.network!=="udp")throw Error("UDP source address");
 const reply=first.receive();
 await second.send(packet.data,packet.remoteAddress);
 if((await reply).data.join()!==data.join())throw Error("UDP reply");
}
const controller=new AbortController(),reason={kind:"cancel pending UDP receive"};
const pending=second.receive({signal:controller.signal}).then(()=>{throw Error("aborted receive succeeded");},error=>error);
controller.abort(reason);
if(await pending!==reason)throw Error("UDP abort reason");
const next=second.receive();await first.send(new Uint8Array([77]),second.address);
if((await next).data[0]!==77)throw Error("UDP canceled deadline poisoned next receive");
const closing=second.close();if(closing!==second.close()||await closing!==undefined)throw Error("UDP shared close");
export default "UDP consumer passed";
`}, stdlib.Profile{Modules: stdlib.Modules{RawSockets: true}})
	if err != nil || string(result.Data) != `"UDP consumer passed"` {
		return fmt.Errorf("UDP consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
