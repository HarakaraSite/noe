//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

const cancellationCleanupSource = `
import {open,readFile} from "node:fs/promises";
const stop=new AbortController(),reason={source:"user"};
const signal=AbortSignal.any(new Set([stop.signal]));
let started;
const reading=new Promise(resolve=>{started=resolve;});
let cancelled=false,aborted=false,file,writing;
{
  await using owned=await open("owned.bin","wx+");
  file=owned;
  const source=new ReadableStream({
    pull(){started();return new Promise(()=>{});},
    cancel(error){if(error!==reason)throw Error("cancel reason");cancelled=true;}
  },{highWaterMark:0});
  const sink=new WritableStream({abort(error){if(error!==reason)throw Error("abort reason");aborted=true;}});
  const piping=source.pipeTo(sink,{signal});
  await reading;
  stop.abort(reason);
  const failure=await piping.then(()=>null,error=>error);
  if(failure!==reason || !cancelled || !aborted || source.locked || sink.locked)throw Error("combined cancellation");
  writing=file.write("scoped bytes");
}
if((await writing).bytesWritten!==12 || await readFile("owned.bin","utf8")!=="scoped bytes")throw Error("admitted write disposal");
if((await file.stat().then(()=>null,error=>error))?.code!=="ERR_FILE_CLOSED")throw Error("await using did not close file");
console.log("combined cancellation and disposal passed");
`

func TestCancellationAndDisposalAcrossExecutionModes(t *testing.T) {
	for _, mode := range []string{"run", "eval", "worker", "events"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var result cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "main.mjs"), cancellationCleanupSource)
				result = runCLI(t, work, nil, "run", "main.mjs")
			case "eval":
				result = runCLI(t, work, nil, "eval", cancellationCleanupSource)
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.mjs"), cancellationCleanupSource)
				result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";
const worker=new Worker("./worker.mjs");
await new Promise((resolve,reject)=>{worker.on("error",reject);worker.on("exit",code=>code?reject(Error("worker exit "+code)):resolve());});`)
			case "events":
				result = runCLI(t, work, nil, "eval", "--events=-", cancellationCleanupSource)
				result.stdout = processIOEventText(t, result.stdout, "stdout")
			}
			requireExit(t, result, 0)
			if result.stderr != "" || !strings.Contains(result.stdout, "combined cancellation and disposal passed") {
				t.Fatalf("cancellation/disposal result=%+v", result)
			}
		})
	}
}

func TestCancellationAndDisposalInFixtureAgentRunJS(t *testing.T) {
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 0 || !strings.Contains(result.Stdout, "combined cancellation and disposal passed") {
					t.Errorf("generated cancellation/disposal failed: %+v", result)
				}
				return agentModelReply{Content: "cleanup complete"}
			}
		}
		if agentUser(request) == "combine cancellation and close scoped resources" {
			return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("cancellation-cleanup", cancellationCleanupSource)}}}, FinishReason: "tool_calls"}
		}
		return agentModelReply{Content: "cleanup complete"}
	})
	defer server.Close()
	result := runCLI(t, t.TempDir(), agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"combine cancellation and close scoped resources");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`)
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "cleanup complete") {
		t.Fatalf("fixture agent result=%+v", result)
	}
}
