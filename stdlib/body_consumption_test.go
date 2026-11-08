package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

const bodyConsumptionContract = `
async function rejectsTypeError(call) {
  let failure;try{await call();}catch(error){failure=error;}
  check(failure instanceof TypeError,"body operation must reject TypeError");
}
for(const make of [body=>new Response(body),body=>new Request("https://example.test/",{method:"POST",body})]) {
  for(const method of ["arrayBuffer","bytes","text","json","blob","formData"]) {
    const value=make("payload"),stream=value.body,reader=stream.getReader();
    check(!value.bodyUsed && stream.locked,"acquiring a reader only locks the body");
    await rejectsTypeError(()=>value[method]());
    throws("TypeError",()=>value.clone());
    if(value instanceof Request)throws("TypeError",()=>new Request(value));
    check(!value.bodyUsed,"failed locked read leaves body undisturbed");
    reader.releaseLock();
    check(await value.text()==="payload" && value.bodyUsed && value.body===stream && stream.locked,"helpers consume the existing stream");
    await rejectsTypeError(()=>value.text());
    throws("TypeError",()=>value.clone());

    const cancelled=make("cancelled");await cancelled.body.cancel();
    check(cancelled.bodyUsed,"cancellation disturbs a byte body");
    await rejectsTypeError(()=>cancelled[method]());throws("TypeError",()=>cancelled.clone());
  }
  const empty=make(new Uint8Array(0));check(empty.body!==null && !empty.bodyUsed,"empty byte body exists");
  check(await empty.text()==="" && empty.bodyUsed,"empty byte body is consumed once");
  const absent=make(null);check(absent.body===null && await absent.text()==="" && await absent.text()==="" && !absent.bodyUsed,"null body remains absent");
  const original=make("original"),old=original.body,copy=original.clone();
  check(original.body!==old && old.locked && !original.bodyUsed && !copy.bodyUsed,"clone tees even a finite body");
  (await original.bytes())[0]=88;
  check(await copy.text()==="original","bytes result does not change clone data");

  const backing=new Uint8Array([88,65,66,99]),snapshotted=make(backing.subarray(1,3));backing[1]=90;
  check(await snapshotted.text()==="AB","constructors snapshot the exact input view");
  let sliceCalls=0;
  class CustomBytes extends Uint8Array {slice(){sliceCalls++;return this;}}
  const custom=new CustomBytes([65]),customBody=make(custom);custom[0]=66;
  check(await customBody.text()==="A","body extraction copies typed-array storage directly");
  check(await make(ReadableStream.from([new CustomBytes([67])])).text()==="C" && sliceCalls===0,"read-all copies do not invoke user slice methods");
  const partial=make(ReadableStream.from([new Uint8Array([65]),new Uint8Array([66])])),reader=partial.body.getReader();
  await reader.read();reader.releaseLock();check(partial.bodyUsed,"partial reads disturb the body");
  await rejectsTypeError(()=>partial.text());throws("TypeError",()=>partial.clone());

  const invalids=["wrong",[65],new Uint16Array([65]),new DataView(new ArrayBuffer(1)),{},null,undefined];
  for(const chunk of invalids)await rejectsTypeError(()=>make(ReadableStream.from([chunk])).bytes());
  let step=0;const reused=new Uint8Array([65]);
  const source=new ReadableStream({pull(controller){
    if(step===0){step++;controller.enqueue(reused);}
    else if(step===1){step++;reused[0]=66;controller.enqueue(reused);}
    else controller.close();
  }},{highWaterMark:0});
  check(await make(source).text()==="AB","read-all snapshots producer buffer reuse");
  const reason={message:"producer failure"},failed=make(new ReadableStream({start(controller){controller.error(reason);}}));
  let observed;try{await failed.bytes();}catch(error){observed=error;}
  check(observed===reason && failed.bodyUsed,"producer failure identity is retained");
}
const source=new Request("https://example.test/",{method:"POST",body:"moved"}),old=source.body,moved=new Request(source);
check(source.body===old && source.bodyUsed && old.locked && !moved.bodyUsed,"finite Request transfer locks and disturbs the source");
await rejectsTypeError(()=>source.text());check(await moved.text()==="moved","transferred body remains readable");
`

func TestBodyConsumptionContract(t *testing.T) {
	executeWebBasics(t, stdlib.Profile{}, bodyConsumptionContract)
}
