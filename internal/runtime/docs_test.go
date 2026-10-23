package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
	"forge.harakara.site/littleisland/noe/v2/internal/apidocs"
)

func TestDocsRuntimeContractAndCatalogExports(t *testing.T) {
	want, err := apidocs.Lookup("node:child_process", "execFile")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(want)
	source := `import {docs} from "noe:docs";
if(docs("child_process","execFile") !== ` + string(encoded) + `)throw Error("lookup differs");
if(docs(undefined)!==docs())throw Error("optional arguments");
for(const call of [()=>docs(null),()=>docs(1),()=>docs("node:fs",{}),()=>docs("node:fs",undefined,"extra")]) {
 let thrown=false;try{call()}catch(e){if(!(e instanceof TypeError))throw e;thrown=true}if(!thrown)throw Error("type validation");
}
for(const call of [()=>docs("node:net"),()=>docs("noe:run_js","missing"),()=>docs("noe:agent/hooks")]) {
 let thrown=false;try{call()}catch(e){if(e.code!=="ERR_DOC_NOT_FOUND")throw e;thrown=true}if(!thrown)throw Error("missing validation");
}
let toolOnly=false;try{await import("noe:run_js")}catch{toolOnly=true}if(!toolOnly)throw Error("tool is not a module");
`
	for _, topic := range apidocs.Topics() {
		if topic.Kind != "module" {
			continue
		}
		source += fmt.Sprintf(`{
 const m=await import(%q), text=docs(%q);
 const declaration=text.match(/(?:### Exports\s*\n\s*|Public exports: )([^\n]+)/);
 if(!declaration)throw Error("missing explicit export inventory");
 const documented=[...declaration[1].matchAll(/\x60([^\x60]+)\x60/g)].map(match=>match[1]).sort();
 const actual=Object.keys(m).sort();
 if(JSON.stringify(actual)!==JSON.stringify(documented))throw Error(%q+JSON.stringify({actual,documented}));
}
`, topic.ID, topic.ID, topic.ID+" exports absent from docs: ")
		for _, alias := range topic.Aliases {
			source += fmt.Sprintf(`if(JSON.stringify(Object.keys(await import(%q)).sort())!==JSON.stringify(Object.keys(await import(%q)).sort()))throw Error("alias exports differ");`+"\n", alias, topic.ID)
		}
	}
	source += `console.log("docs contracts passed");`
	if out := runStdlib(t, source, Options{}); out != "docs contracts passed\n" {
		t.Fatal(out)
	}
}

func TestDocsInWorkersAndNestedWorkers(t *testing.T) {
	dir := t.TempDir()
	code := `import {parentPort,workerData,Worker} from "node:worker_threads";
import {docs} from "noe:docs";
if(workerData.nested){
 const worker=new Worker("./worker.mjs",{workerData:{nested:false}});
 const text=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
 if(text!==docs("noe:sockets","connect"))throw Error("nested docs differ");
 await worker.terminate();
}
parentPort.postMessage(docs("noe:sockets","connect"));`
	if err := os.WriteFile(filepath.Join(dir, "worker.mjs"), []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	source := `import {Worker} from "node:worker_threads";import {docs} from "noe:docs";
const worker=new Worker("./worker.mjs",{workerData:{nested:true}});
const text=await new Promise((resolve,reject)=>{worker.on("message",resolve);worker.on("error",reject)});
if(text!==docs("noe:sockets","connect"))throw Error("worker docs differ");
await worker.terminate();console.log("worker docs passed");`
	if out := runStdlib(t, source, Options{Cwd: dir}); out != "worker docs passed\n" {
		t.Fatal(out)
	}
}

func TestAgentLooksUpDocsThenUsesAPI(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "bytes.bin"), []byte("abcdef"), 0600); err != nil {
		t.Fatal(err)
	}
	var steps atomic.Int32
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		step := steps.Add(1)
		var messages []struct{ Role, Content string }
		for _, raw := range req.Messages {
			var message struct{ Role, Content string }
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			messages = append(messages, message)
		}
		if step == 1 {
			if len(messages) == 0 || !strings.Contains(messages[0].Content, "noe:docs") || !strings.Contains(messages[0].Content, "fresh owned JavaScript runtime") {
				return provider.Response{}, fmt.Errorf("initial instructions lost identity or reference navigation")
			}
			return hookToolReply(map[string]string{"lookup": "// Read the positional file API contract.\nconst {docs}=await import(\"noe:docs\");console.log(docs(\"noe:fs-handle\"));console.log(docs(\"noe:fs-handle\",\"FileHandle.readAt\"));"}, "lookup"), nil
		}
		if step == 2 {
			found := false
			for _, message := range messages {
				if message.Role == "tool" && strings.Contains(message.Content, "bytesRead") && strings.Contains(message.Content, "readAt") {
					found = true
				}
			}
			if !found {
				return provider.Response{}, fmt.Errorf("API reference was not delivered as a tool result")
			}
			return hookToolReply(map[string]string{"read": "// Use the documented positional read and release the handle.\nconst {open}=await import(\"noe:fs-handle\");const f=await open(\"bytes.bin\");try{const r=await f.readAt(2,3);console.log(new TextDecoder().decode(r.data));}finally{await f.close();}"}, "read"), nil
		}
		for _, message := range messages {
			if message.Role == "tool" && strings.Contains(message.Content, "cde") {
				return agentAnswer("cde"), nil
			}
		}
		return provider.Response{}, fmt.Errorf("documented API failed")
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var out, stderr bytes.Buffer
	source := `import {run_agent} from "noe:agent";const r=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"Read bytes at offset 2 with length 3 from bytes.bin.");console.log(r.lastResult.answer);`
	code, err := Execute(ctx, Source{Name: filepath.Join(dir, "task.mjs"), Code: source}, Options{Cwd: dir, Agent: agentTestOptions(p), Stdout: &out, Stderr: &stderr})
	if err != nil || code != 0 || out.String() != "cde\n" || steps.Load() != 3 {
		t.Fatalf("code=%d err=%v steps=%d stdout=%q stderr=%q", code, err, steps.Load(), &out, &stderr)
	}
}
