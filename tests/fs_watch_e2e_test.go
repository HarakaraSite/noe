//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const fsWatchSource = `
import fs,{watch} from "noe:fs";
const controller=new AbortController();
{
 await using changes=watch(".",{signal:AbortSignal.any([controller.signal])});
 const next=changes.next();
 fs.writeFileSync("watched.txt","initial");
 const event=await next;
 if(event.done||event.value.filename!=="watched.txt")throw Error("watch event "+JSON.stringify(event));
}
let changes;
{
 await using owned=watch("watched.txt");
 changes=owned;
 const changed=owned.next();
 fs.appendFileSync("watched.txt","updated");
 if((await changed).value.eventType!=="change")throw Error("iterator event");
}
if(!(await changes.next()).done)throw Error("watch iterator disposal");
const canceled=watch(".",{signal:controller.signal});
const pending=canceled.next().then(()=>null,error=>error);
controller.abort(0);
const failure=await pending;
if(failure?.name!=="AbortError"||failure.cause!==0)throw Error("watch abort");
await canceled.close();
console.log("filesystem watch passed");
`

func TestFilesystemWatchAcrossExecutionModes(t *testing.T) {
	for _, mode := range []string{"run", "eval", "worker", "events"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var result cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "watch.mjs"), fsWatchSource)
				result = runCLI(t, work, nil, "run", "watch.mjs")
			case "eval":
				result = runCLI(t, work, nil, "eval", fsWatchSource)
			case "worker":
				writeFixture(t, filepath.Join(work, "watch.mjs"), fsWatchSource)
				result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";
const worker=new Worker("./watch.mjs");
await new Promise((resolve,reject)=>{worker.on("error",reject);worker.on("exit",code=>code?reject(Error("worker exit "+code)):resolve())});`)
			case "events":
				result = runCLI(t, work, nil, "eval", "--events=-", fsWatchSource)
				result.stdout = processIOEventText(t, result.stdout, "stdout")
			}
			requireExit(t, result, 0)
			if result.stderr != "" || !strings.Contains(result.stdout, "filesystem watch passed") {
				t.Fatalf("filesystem watch result=%+v", result)
			}
		})
	}
}

func TestFilesystemWatchInFixtureAgentRunJS(t *testing.T) {
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 0 || !strings.Contains(result.Stdout, "filesystem watch passed") {
					t.Errorf("generated watch failed: %+v", result)
				}
				return agentModelReply{Content: "watch complete"}
			}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("filesystem-watch", fsWatchSource)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"watch a local file and stop watching");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`)
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "watch complete") {
		t.Fatalf("fixture watch result=%+v", result)
	}
}

func TestFilesystemWatchUnreferencedExit(t *testing.T) {
	for _, source := range []string{
		`import {watch} from "noe:fs";watch(".");console.log("unreferenced watch");`,
		`import {watch} from "noe:fs";watch(".",{persistent:false}).next();console.log("unreferenced watch");`,
	} {
		result := runCLI(t, t.TempDir(), nil, "eval", source)
		requireExit(t, result, 0)
		if result.stderr != "" || !strings.Contains(result.stdout, "unreferenced watch") {
			t.Fatalf("unreferenced watch result=%+v", result)
		}
	}
}
