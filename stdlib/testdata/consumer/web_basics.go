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
const mark=performance.mark("child",{startTime:1});
parentPort.postMessage({detail,type:mark.entryType,entries:performance.getEntries().length});parentPort.close();`
	code := fmt.Sprintf(`import {Worker} from "node:worker_threads";
const input={value:1};const mark=performance.mark("parent",{startTime:2,detail:input});input.value=9;
const measured=performance.measure("duration",{start:"parent",end:5});
if(!(mark instanceof PerformanceMark)||mark.detail.value!==1||measured.duration!==3)throw Error("User Timing contract");
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
