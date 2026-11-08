package stdlib_test

import (
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const bodyCloneBytesContract = `
for(const make of [body=>new Response(body),body=>new Request("https://example.test/",{method:"POST",body})]) {
  for(const streamInput of [false,true])for(const changed of [0,1]) {
    const input=new Uint8Array([88,65,66,99]);
    const original=make(streamInput?ReadableStream.from([input.subarray(1,3)]):input.subarray(1,3)),copy=original.clone();
    const values=[original,copy],reader=values[changed].body.getReader();
    const {value}=await reader.read();value[0]=90;
    check(await values[1-changed].text()==="AB","byte mutation stays in its clone branch");
    reader.releaseLock();
  }
  for(const moved of [0,1]) {
    const original=make(ReadableStream.from([new Uint8Array([67,68])])),copy=original.clone(),values=[original,copy];
    const reader=values[moved].body.getReader(),chunk=await reader.read();
    structuredClone(chunk.value.buffer,{transfer:[chunk.value.buffer]});
    check(chunk.value.byteLength===0 && await values[1-moved].text()==="CD","buffer transfer does not detach the other body");
    reader.releaseLock();
  }
  const storage=new Uint8Array([88,65,66,99]),original=make(ReadableStream.from([storage.subarray(1,3)])),copy=original.clone();
  const ar=original.body.getReader(),br=copy.body.getReader();
  const [a,b]=await Promise.all([ar.read(),br.read()]);
  check(a.value.buffer!==b.value.buffer && b.value.byteOffset===1 && b.value.byteLength===2 && b.value.buffer.byteLength===4,"stream chunk view metadata and separate backing buffer");
  check(new Uint8Array(b.value.buffer).join()==="88,65,66,99","backing buffer contents are preserved");
  ar.releaseLock();br.releaseLock();

  const multi=make("ef"),first=multi.clone(),second=multi.clone();
  const reader=multi.body.getReader(),chunk=await reader.read();chunk.value.fill(90);reader.releaseLock();
  check((await Promise.all([first.text(),second.text()])).join()==="ef,ef","repeated clones retain original bytes");
}
{
  const [a,b]=ReadableStream.from([new Uint8Array([65])]).tee(true);
  const first=await a.getReader().read();first.value[0]=90;
  check((await b.getReader().read()).value===first.value,"public tee keeps shared chunk identity even with an extra argument");
}
{
  const original=new Response(ReadableStream.from([new Uint8Array(new SharedArrayBuffer(1))])),copy=original.clone();
  const a=await original.body.getReader().read(),b=await copy.body.getReader().read();a.value[0]=91;
  check(a.value!==b.value && b.value[0]===91,"shared memory remains shared while view objects are distinct");
}
{
  const value=new Response(ReadableStream.from(["invalid byte chunk"])),copy=value.clone();
  for(const body of [value,copy]){let error;try{await body.bytes();}catch(failure){error=failure;}check(error instanceof TypeError,"existing non-byte body rejection");}
}
{
  let finish,reason,calls=0,settled=false;
  const bytes=new Uint8Array([65]);
  const original=new Response(new ReadableStream({pull(controller){
    controller.enqueue(bytes);structuredClone(bytes.buffer,{transfer:[bytes.buffer]});
  },cancel(error){calls++;reason=error;return new Promise(resolve=>{finish=resolve;});}},{highWaterMark:0}));
  const copy=original.clone(),cancelled=original.body.cancel("unused").then(()=>settled=true);
  let error;try{await copy.bytes();}catch(failure){error=failure;}
  check(error && error===reason && calls===1 && !settled,"copy failure adopts source cleanup before completing cancellation");
  finish();await cancelled;check(settled,"copy failure cleanup completes pending cancellation");
}
`

func TestBodyCloneByteIsolation(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, bodyCloneBytesContract)
}

func TestBodyCloneWorkerBufferTransfer(t *testing.T) {
	child := `import {parentPort} from "node:worker_threads";
const original=new Response(new Uint8Array([65,66])),copy=original.clone();
const chunk=await original.body.getReader().read();
parentPort.postMessage({stage:"bytes",value:chunk.value},[chunk.value.buffer]);
if(chunk.value.byteLength!==0)throw Error("worker source buffer not transferred");
parentPort.postMessage({stage:"done",value:await copy.text()});parentPort.close();`
	source := fmt.Sprintf(`import {Worker} from "node:worker_threads";
const worker=new Worker(%q,{eval:true,type:"module"});let bytes;
const text=await new Promise((resolve,reject)=>{
 worker.on("message",message=>{if(message.stage==="bytes")bytes=message.value;else if(message.stage==="done")resolve(message.value);});
 worker.once("error",reject);
});
await worker.terminate();
check(bytes instanceof Uint8Array&&bytes.join()==="65,66"&&text==="AB","Worker transfer leaves the sibling clone readable");`, child)
	executeWebBasics(t, stdlib.Profile{Modules: stdlib.Modules{Workers: &stdlib.WorkerOptions{Max: 1}}}, source)
}
