package main

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func serveLifecycle(ctx context.Context) error {
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[HTTP lifecycle consumer]", Virtual: true, Module: true, Code: `
import {serve} from "noe:http";
let closing;
await using server=serve({port:0},async request=>{
 if(new URL(request.url).pathname==="/close"){
  closing=server.close();await Promise.resolve();return new Response("bye");
 }
 return new Response(await request.text());
});
const exact=await fetch(server.url,{method:"POST",body:"abcde"});
if(exact.status!==200 || await exact.text()!=="abcde")throw Error("exact request cap");
const large=await fetch(server.url,{method:"POST",body:"abcdef"});
if(large.status!==413)throw Error("request overflow accepted");
await large.text();
if(await(await fetch(server.url+"/close")).text()!=="bye")throw Error("shutdown lost response");
if(closing!==server.close() || await closing!==undefined)throw Error("close promise contract");
export default "ok";
`}, stdlib.Profile{Modules: stdlib.Modules{
		Serve: &stdlib.ServeOptions{MaxBodyBytes: 5, Allow: func(string) error { return nil }},
		Fetch: &stdlib.FetchOptions{},
	}})
	if err != nil || string(result.Data) != `"ok"` {
		return fmt.Errorf("HTTP lifecycle consumer result=%+v error=%s", result, stdlib.Diagnostic(err))
	}
	return nil
}
