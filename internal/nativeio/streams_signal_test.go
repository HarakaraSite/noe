package nativeio

import "testing"

func TestPipeInvalidSignalDoesNotAcquireStreams(t *testing.T) {
	for _, signal := range []string{
		"{}", "true", "false", "null", "0", "NaN", "''", "0n", "1", "'signal'", "Symbol('signal')",
		"{addEventListener(){}}", "{removeEventListener(){}}",
		"{addEventListener:1,removeEventListener(){}}",
		"{addEventListener(){},removeEventListener:1}",
	} {
		t.Run(signal, func(t *testing.T) {
			rt, _, loop := streamRuntime(t, Options{})
			evalStreams(t, rt, loop, `
const calls={reader:0,writer:0,read:0,write:0,cancel:0,abort:0,close:0};
const source=new ReadableStream({
  pull(c){calls.read++;c.close();},cancel(){calls.cancel++;}
},{highWaterMark:0});
const destination=new WritableStream({
  write(){calls.write++;},abort(){calls.abort++;},close(){calls.close++;}
});
const getReader=source.getReader.bind(source),getWriter=destination.getWriter.bind(destination);
source.getReader=()=>{calls.reader++;return getReader();};
destination.getWriter=()=>{calls.writer++;return getWriter();};
let error;
try {await source.pipeTo(destination,{signal:`+signal+`});} catch(e) {error=e;}
if(!(error instanceof TypeError)||source.locked||destination.locked||Object.values(calls).some(n=>n!==0))
  throw Error("invalid signal touched streams: "+JSON.stringify({error:String(error),source:source.locked,destination:destination.locked,calls}));`)
		})
	}
}

func TestPipeSignalCompatibility(t *testing.T) {
	rt, _, loop := streamRuntime(t, Options{})
	evalStreams(t, rt, loop, `
for(const signal of [undefined,new AbortController().signal]) {
  let reads=0,writes=0,closed=0;
  const source=new ReadableStream({pull(c){reads++;if(reads===1)c.enqueue(new Uint8Array([42]));else c.close();}},{highWaterMark:0});
  const destination=new WritableStream({write(value){if(value[0]!==42)throw Error("value");writes++;},close(){closed++;}});
  await source.pipeTo(destination,{signal});
  if(source.locked||destination.locked||reads!==2||writes!==1||closed!==1)throw Error("signal compatibility");
}`)
}

func TestPipePreAbortedSignal(t *testing.T) {
	rt, _, loop := streamRuntime(t, Options{})
	evalStreams(t, rt, loop, `
const reason=new Error("already aborted"),controller=new AbortController();
controller.abort(reason);
let reads=0,writes=0,cancels=0,aborts=0;
const source=new ReadableStream({pull(){reads++;},cancel(error){if(error!==reason)throw Error("cancel reason");cancels++;}},{highWaterMark:0});
const destination=new WritableStream({write(){writes++;},abort(error){if(error!==reason)throw Error("abort reason");aborts++;}});
let error;
try {await source.pipeTo(destination,{signal:controller.signal});} catch(e) {error=e;}
if(error!==reason||source.locked||destination.locked||reads||writes||cancels!==1||aborts!==1)
  throw Error("pre-aborted pipe");`)
}

func TestPipeSignalCleanupFailureReleasesLocks(t *testing.T) {
	rt, _, loop := streamRuntime(t, Options{})
	evalStreams(t, rt, loop, `
const reason=new Error("remove listener failed");
const signal=new AbortController().signal;
signal.removeEventListener=()=>{throw reason;};
const source=new ReadableStream({pull(c){c.close();}},{highWaterMark:0});
const destination=new WritableStream();
let error;
try {await source.pipeTo(destination,{signal});} catch(e) {error=e;}
if(error!==reason||source.locked||destination.locked)
  throw Error("cleanup left locks: "+JSON.stringify({source:source.locked,destination:destination.locked}));`)
}
