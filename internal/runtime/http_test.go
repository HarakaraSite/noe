package runtime

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const httpExercise = `import * as http from "noe:http";
import * as oldHTTP from "node:http";
import * as bareHTTP from "http";
import {docs} from "noe:docs";
export async function exerciseHTTP() {
 if(Object.keys(http).sort().join()!=="fetch,serve")throw Error("HTTP exports");
 if(http.fetch!==globalThis.fetch||http.serve!==globalThis.serve||http.serve!==oldHTTP.serve||oldHTTP.serve!==bareHTTP.default.serve)throw Error("HTTP identity");
 const server=http.serve({port:0},async request=>{
  if(!(request instanceof Request)||!(request.headers instanceof Headers))throw Error("request types");
  const path=new URL(request.url).pathname;
  if(path==="/json")return Response.json({value:(await request.json()).value+1},{status:201});
  if(path==="/binary")return new Response(ReadableStream.from([new Uint8Array(await request.arrayBuffer())]));
  if(path==="/ws"){
   const {socket,response}=upgradeWebSocket(request);
   socket.onmessage=e=>socket.send(e.data);
   return response;
  }
  return new Response("missing",{status:404});
 });
 try {
  const json=await http.fetch(new Request(server.url+"/json",{method:"POST",headers:{"content-type":"application/json"},body:JSON.stringify({value:41})}));
  if(!(json instanceof Response)||!(json.headers instanceof Headers)||json.status!==201||(await json.json()).value!==42)throw Error("JSON roundtrip");
  const binary=await http.fetch(server.url+"/binary",{method:"POST",body:ReadableStream.from([new Uint8Array([0,255]),new Uint8Array([120])])});
  if(!(binary.body instanceof ReadableStream)||(await binary.bytes()).join()!=="0,255,120"||!binary.bodyUsed)throw Error("binary streams");
  let consumed=false;try{await binary.text();}catch{consumed=true;}if(!consumed)throw Error("body reused");
  const missing=await http.fetch(server.url+"/missing");
  if(missing.ok||missing.status!==404||await missing.text()!=="missing")throw Error("HTTP status");
  const socket=new WebSocket(server.url.replace("http:","ws:")+"/ws");
  try {
   await new Promise((resolve,reject)=>{
    socket.onopen=()=>socket.send("shared upgrade");socket.onerror=reject;
    socket.onmessage=e=>{if(e.data!=="shared upgrade")reject(Error("upgrade response"));socket.close();};
    socket.onclose=resolve;
   });
  } finally {socket.close();}
 } finally {server.close();server.close();}
 return [docs("noe:http"),docs("noe:http","fetch"),docs("noe:http","serve")];
}`

func TestNoeHTTPInMainWorkersAndNestedWorkers(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"exercise.mjs": httpExercise,
		"node-worker.mjs": `import {parentPort,workerData,Worker} from "node:worker_threads";
import {exerciseHTTP} from "./exercise.mjs";
const own=await exerciseHTTP();let child=null;
if(workerData.nested){
 const worker=new Worker("./node-worker.mjs",{workerData:{nested:false}});
 try{child=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});}
 finally{await worker.terminate();}
}
parentPort.postMessage({own,child});`,
		"web-worker.mjs": `import {exerciseHTTP} from "./exercise.mjs";postMessage(await exerciseHTTP());`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil {
			t.Fatal(err)
		}
	}
	source := fmt.Sprintf(`import {exerciseHTTP} from %q;
import {Worker} from "node:worker_threads";
const expected=JSON.stringify(await exerciseHTTP());
const node=new Worker("./node-worker.mjs",{workerData:{nested:true}});
try{
 const r=await new Promise((resolve,reject)=>{node.once("message",resolve);node.once("error",reject);});
 if(JSON.stringify(r.own)!==expected||JSON.stringify(r.child.own)!==expected)throw Error("Node Worker docs differ");
}finally{await node.terminate();}
const web=new globalThis.Worker("./web-worker.mjs",{type:"module"});
try{
 const r=await new Promise((resolve,reject)=>{web.onmessage=e=>resolve(e.data);web.onerror=reject;});
 if(JSON.stringify(r)!==expected)throw Error("Web Worker docs differ");
}finally{await web.terminate();}
console.log("HTTP ready in four modes");`, filepath.Join(dir, "exercise.mjs"))
	if out := runStdlib(t, source, Options{Cwd: dir}); out != "HTTP ready in four modes\n" {
		t.Fatal(out)
	}
}

func TestNoeHTTPFetchAbort(t *testing.T) {
	cancelled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "first")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(cancelled)
	}))
	defer server.Close()
	source := fmt.Sprintf(`import {fetch} from "noe:http";
const controller=new AbortController(),reason=new Error("stop HTTP body");
const response=await fetch(%q,{signal:controller.signal});
const reader=response.body.getReader();
try{
 let first="";while(first.length<5){const chunk=await reader.read();if(chunk.done)throw Error("early EOF");first+=new TextDecoder().decode(chunk.value);}
 if(first!=="first")throw Error("response bytes");
 controller.abort(reason);
 let rejected=false;try{await reader.read();}catch{rejected=true;}
 if(!rejected)throw Error("abort ignored");
}finally{reader.releaseLock();}
console.log("HTTP body aborted");`, server.URL)
	if out := runStdlib(t, source, Options{}); out != "HTTP body aborted\n" {
		t.Fatal(out)
	}
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("HTTP request was not cancelled")
	}
}

func TestNoeHTTPWorkerTerminationClosesListener(t *testing.T) {
	source := `import {Worker} from "node:worker_threads";import {fetch} from "noe:http";
const worker=new Worker('import {parentPort} from "node:worker_threads";import {serve} from "noe:http";const server=serve({port:0},()=>new Response("owned"));parentPort.postMessage(server.url);',{eval:true});
let url;
try{
 url=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject);});
 if(await(await fetch(url)).text()!=="owned")throw Error("worker server");
}finally{await worker.terminate();}
console.log(url);`
	url := strings.TrimSpace(runStdlib(t, source, Options{}))
	conn, err := net.DialTimeout("tcp", strings.TrimPrefix(url, "http://"), time.Second)
	if err == nil {
		conn.Close()
		t.Fatal("terminated Worker retained its listener")
	}
}
