package stdlib_test

import (
	"testing"

	"forge.harakara.site/littleisland/noe/v2/internal/stdlib"
)

func TestWebWorkerAbortAndTextStreams(t *testing.T) {
	code := `
(async()=>{
 const controller=new AbortController();controller.abort();
 if(!(controller.signal.reason instanceof DOMException)||controller.signal.reason.name!=="AbortError")throw Error("worker abort default");
 const decoder=new TextDecoderStream("utf-8",{fatal:true});
 if(!decoder.fatal||decoder.ignoreBOM)throw Error("worker stream options");
 let text="";
 for await(const value of ReadableStream.from(["\ud83d","","\ude00"]).pipeThrough(new TextEncoderStream()).pipeThrough(decoder))text+=value;
 if(text!=="😀")throw Error("worker split surrogate");
 const timeout=AbortSignal.timeout(0);
 await new Promise(resolve=>timeout.addEventListener("abort",resolve,{once:true}));
 if(!(timeout.reason instanceof DOMException)||timeout.reason.name!=="TimeoutError")throw Error("worker timeout");
 postMessage("worker abort/text streams passed");close();
})()`
	out, stderr := run(t, stdlib.Config{Workers: workerFiles(map[string]string{"./abort-text.js": code})}, `
const worker=new Worker("./abort-text.js");worker.onmessage=event=>console.log(event.data);
`)
	if out != "worker abort/text streams passed" || stderr != "" {
		t.Fatalf("output = %q, stderr = %q", out, stderr)
	}
}
