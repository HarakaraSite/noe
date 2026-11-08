package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const streamTeeContract = `
const turns=async()=>{for(let i=0;i<20;i++)await Promise.resolve();};
for(const order of [[0,1],[1,0]]) {
  let finish,received,calls=0;
  const source=new ReadableStream({cancel(reason){calls++;received=reason;return new Promise(resolve=>{finish=resolve;});}},{highWaterMark:0});
  const branches=source.tee(),settled=[false,false],reasons=[{branch:0},{branch:1}];
  const first=branches[order[0]].cancel(reasons[order[0]]).then(()=>settled[order[0]]=true);
  await turns();check(!settled[order[0]] && calls===0,"one cancellation waits for the remaining branch");
  const second=branches[order[1]].cancel(reasons[order[1]]).then(()=>settled[order[1]]=true);
  await turns();check(!settled[0]&&!settled[1]&&calls===1,"both callers wait for source cleanup");
  check(received[0]===reasons[0]&&received[1]===reasons[1],"source reasons use branch order");
  finish();await Promise.all([first,second]);check(settled[0]&&settled[1]&&source.locked,"source cleanup settles both callers");
}
{
  const reason={cleanup:"failed"};let fail;
  const [a,b]=new ReadableStream({cancel(){return new Promise((_,reject)=>{fail=reject;});}},{highWaterMark:0}).tee();
  const first=a.cancel(),second=b.cancel();fail(reason);
  const results=await Promise.allSettled([first,second]);
  check(results.every(result=>result.status==="rejected"&&result.reason===reason),"cleanup rejection reaches both callers");
}
for(const terminal of ["close","error"]) {
  let controller,settled=false,calls=0;
  const [a,b]=new ReadableStream({start(c){controller=c;},cancel(){calls++;}},{highWaterMark:0}).tee();
  const cancelled=a.cancel("unused").then(()=>settled=true);await turns();check(!settled,"one branch is still live");
  const reader=b.getReader(),pending=reader.read(),reason={terminal};
  if(terminal==="close")controller.close();else controller.error(reason);
  if(terminal==="close")check((await pending).done,"live branch reaches EOF");
  else {let error;try{await pending;}catch(value){error=value;}check(error===reason,"live branch sees original error");}
  await cancelled;check(settled&&calls===0,"source terminal state completes one cancelled branch");
}
for(const timing of ["queued","enqueue-error"]) {
  const reason={timing};let controller;
  const source=new ReadableStream({start(c){controller=c;},pull(c){c.enqueue("stale");if(timing==="enqueue-error")c.error(reason);}},{highWaterMark:0});
  const [a,b]=source.tee();await turns();if(timing==="queued")controller.error(reason);await turns();
  for(const branch of [a,b]) {
    let error;try{await branch.getReader().read();}catch(value){error=value;}
    check(error===reason,"source error discards queued tee data");
  }
}
for(const timing of ["sync-error","microtask-error"]) {
  const reason={timing};
  const [a,b]=new ReadableStream({pull(controller){
    controller.enqueue("accepted");
    if(timing==="sync-error")controller.error(reason);
    else Promise.resolve().then(()=>controller.error(reason));
  }},{highWaterMark:0}).tee();
  const ar=a.getReader(),br=b.getReader();
  const results=await Promise.allSettled([ar.read(),ar.read(),br.read(),br.read()]);
  for(const index of [0,2])check(results[index].status==="fulfilled"&&results[index].value.value==="accepted","pending read retains its accepted chunk");
  for(const index of [1,3])check(results[index].status==="rejected"&&results[index].reason===reason,"later pending read sees source error");
}
{
  let pulls=0,active=0,maxActive=0;
  const chunks=Array.from({length:17},(_,index)=>({index}));
  const [fast,slow]=new ReadableStream({async pull(controller){
    active++;maxActive=Math.max(maxActive,active);await Promise.resolve();
    if(pulls===chunks.length)controller.close();else controller.enqueue(chunks[pulls++]);active--;
  }},{highWaterMark:0}).tee();
  const reader=fast.getReader(),reads=Array.from({length:18},()=>reader.read());
  const values=await Promise.all(reads);check(values[17].done&&maxActive===1,"overlapping demand serializes source reads");
  check(values.slice(0,17).every((result,i)=>result.value===chunks[i]),"fast branch order and identity");
  let index=0;for await(const value of slow)check(value===chunks[index++],"slow branch identity");
  check(index===17,"slow branch drains after fast branch EOF");
}
for(const make of [body=>new Response(body),body=>new Request("https://example.test/",{method:"POST",body})]) {
  let controller;const reason={body:"failed"};
  const original=make(new ReadableStream({start(c){controller=c;},pull(c){c.enqueue(new Uint8Array([65]));}},{highWaterMark:0}));
  const copy=original.clone();await turns();controller.error(reason);await turns();
  for(const body of [original,copy]){let error;try{await body.text();}catch(value){error=value;}check(error===reason,"body clone observes source error before queued bytes");}
}
`

func TestStreamTeeLifecycle(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, streamTeeContract)
}
