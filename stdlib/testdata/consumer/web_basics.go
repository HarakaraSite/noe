package main

import (
	"context"
	"fmt"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func webBasics(ctx context.Context) error {
	child := `import {parentPort} from "node:worker_threads";
if(performance.getEntries().length!==0)throw Error("inherited timeline");
const bytes=new Uint8Array(1), encoded=new TextEncoder().encodeInto("あ",bytes);
if(encoded.read!==0||encoded.written!==0)throw Error("partial UTF-8 write");
const target=new EventTarget();let detail;
target.addEventListener("value",event=>{detail=event.detail;});
target.dispatchEvent(new CustomEvent("value",{detail:"worker"}));
const decoder=new TextDecoder();
if(decoder.decode(new Uint8Array([239]),{stream:true})!==""||decoder.decode(new Uint8Array([187,191,192,128]))!=="��")throw Error("streaming UTF-8");
const controller=new AbortController();let calls=0;const callback=()=>calls++;
target.addEventListener("bound",callback,{signal:controller.signal});
target.addEventListener("bound",callback);target.dispatchEvent(new Event("bound"));
controller.abort();target.dispatchEvent(new Event("bound"));if(calls!==1)throw Error("listener identity/abort");
if(!(controller.signal.reason instanceof DOMException)||controller.signal.reason.name!=="AbortError")throw Error("standard abort reason");
const codec=new TextDecoderStream("utf-8",{fatal:true,ignoreBOM:true});
if(!codec.fatal||!codec.ignoreBOM)throw Error("decoder stream metadata");
let decoded="";for await(const chunk of ReadableStream.from(["\ud83d","","\ude00!"]).pipeThrough(new TextEncoderStream()).pipeThrough(codec))decoded+=chunk;
if(decoded!=="😀!")throw Error("split UTF-16 stream");
const timeout=AbortSignal.timeout(0);await new Promise(resolve=>timeout.addEventListener("abort",resolve,{once:true}));
if(timeout.reason.name!=="TimeoutError"||!(timeout.reason instanceof DOMException))throw Error("worker timeout reason");
const baseURL="https://example.test/api/", route=new URLPattern({pathname:"books/:id",baseURL});
const routed=route.exec({pathname:"books/42",baseURL});if(routed?.pathname.groups.id!=="42")throw Error("worker URLPattern inputs");
const queryURL=new URL("https://EXAMPLE.test:00443/a/%2e%2e/☃?q=old"),queryParams=queryURL.searchParams;
queryURL.search="?q=%E0%A4%A&space=+";
if(queryParams!==queryURL.searchParams||queryParams.get("q")!=="�%A"||queryURL.pathname!=="/%E2%98%83"||queryURL.port!=="")throw Error("worker URL normalization/query synchronization");
const queryIterator=queryParams.entries();queryIterator.next();queryParams.append("live","yes");
if(!Array.from(queryIterator).some(pair=>pair[0]==="live"))throw Error("worker live parameter iterator");
const formRequest=new Request(queryURL,{method:"POST",body:queryParams});
if(!(await formRequest.text()).includes("q=%EF%BF%BD%25A"))throw Error("worker Fetch form encoding");
const normalizedRequest=new Request("https://EXAMPLE.test:00443/☃",{method:"patch"});
if(normalizedRequest.url!=="https://example.test/%E2%98%83"||normalizedRequest.method!=="patch"||Reflect.set(normalizedRequest,"method","POST"))throw Error("worker Request inputs");
try{new Request("https://example.test/",{method:"TRACE"});throw Error("forbidden Request method accepted");}catch(error){if(error.name!=="TypeError")throw error;}
for(const format of ["gzip","deflate","deflate-raw"]){
 const plain="book="+routed.pathname.groups.id+" 😀", data=new TextEncoder().encode(plain);
 const compressed=await new Response(ReadableStream.from([data]).pipeThrough(new CompressionStream(format))).bytes();
 const restored=await new Response(ReadableStream.from([compressed]).pipeThrough(new DecompressionStream(format))).text();
 if(restored!==plain)throw Error("worker compression contract");
}
const mark=performance.mark("child",{startTime:1});
parentPort.postMessage({detail,type:mark.entryType,entries:performance.getEntries().length});parentPort.close();`
	code := fmt.Sprintf(`import {Worker} from "node:worker_threads";
const input={value:1};const mark=performance.mark("parent",{startTime:2,detail:input});input.value=9;
const measured=performance.measure("duration",{start:"parent",end:5});
if(!(mark instanceof PerformanceMark)||mark.detail.value!==1||measured.duration!==3)throw Error("User Timing contract");
const defaultAbort=AbortSignal.abort();if(defaultAbort.reason.name!=="AbortError")throw Error("main abort reason");
if(!new URLPattern({pathname:"/books/:id"}).test({pathname:"/books/1"}))throw Error("main URLPattern object input");
const queryURL=new URL("https://EXAMPLE.test:00443/a/../b?q=old"),queryParams=queryURL.searchParams;
queryURL.search="?q=new";if(queryParams!==queryURL.searchParams||queryParams.get("q")!=="new"||queryURL.pathname!=="/b"||queryURL.port!=="")throw Error("main URL normalization/query synchronization");
if(new Request("https://EXAMPLE.test:00443/a/../b",{method:"patch"}).method!=="patch")throw Error("main Request inputs");
const worker=new Worker(%q,{eval:true,type:"module"});
const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});
await worker.terminate();
if(result.detail!=="worker"||result.type!=="mark"||result.entries!==1||performance.getEntriesByName("child").length!==0)throw Error("Worker Web APIs");`, child)
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[public Web API basics consumer]", Virtual: true, Module: true, Code: code},
		stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}})
	if err != nil {
		return fmt.Errorf("Web API basics consumer: %s", stdlib.Diagnostic(err))
	}
	if result.Code != 0 {
		return fmt.Errorf("Web API basics consumer exited %d: %+v", result.Code, result.Diagnostics)
	}
	return nil
}
