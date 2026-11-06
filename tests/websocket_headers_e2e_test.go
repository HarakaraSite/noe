//go:build e2e

package e2e_test

import (
	"context"
	"path/filepath"
	"testing"
)

const websocketHeadersProgram = `
const server=serve({hostname:"127.0.0.1",port:0},request=>{
 if(request.headers.get("authorization")!=="Bearer fixture"||request.headers.get("x-client")!=="noe")return new Response("header mismatch",{status:401});
 const {socket,response}=upgradeWebSocket(request,{protocol:"chat.v1"});
 socket.onopen=()=>socket.send("authenticated");
 socket.onmessage=event=>{if(event.data==="done")socket.close(1000,"done")};
 return response;
});
try{
 const headers=new Headers({Authorization:"Bearer fixture","X-Client":"noe"});
 const socket=new WebSocket(server.url.replace("http","ws"),["chat.v1"],{headers});
 headers.set("Authorization","changed");let received=false;
 await new Promise((resolve,reject)=>{
  socket.onmessage=event=>{received=event.data==="authenticated";socket.send("done")};
  socket.onerror=event=>reject(event.error);
  socket.onclose=event=>received&&event.wasClean&&event.code===1000&&socket.protocol==="chat.v1"?resolve():reject(Error("header/protocol/close mismatch"));
 });
 console.log("WebSocket headers passed");
}finally{server.close()}
`

func TestWebSocketHeadersAcrossExecutionModes(t *testing.T) {
	for _, mode := range []string{"run", "eval", "worker", "agent"} {
		t.Run(mode, func(t *testing.T) {
			work := t.TempDir()
			var result cliResult
			switch mode {
			case "run":
				writeFixture(t, filepath.Join(work, "main.js"), websocketHeadersProgram)
				result = runCLI(t, work, nil, "run", "main.js")
			case "eval":
				result = runCLI(t, work, nil, "eval", websocketHeadersProgram)
			case "worker":
				writeFixture(t, filepath.Join(work, "worker.mjs"), websocketHeadersProgram+`import {parentPort} from "node:worker_threads";parentPort.postMessage("ok");parentPort.close();`)
				result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";const worker=new Worker("./worker.mjs");const result=await new Promise((resolve,reject)=>{worker.once("message",resolve);worker.once("error",reject)});await worker.terminate();if(result!=="ok")throw Error("worker headers");`)
			case "agent":
				provider := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
					for _, message := range request.Messages {
						if message.Role == "tool" {
							tool := agentToolResult(t, message)
							if tool.ExitCode != 0 || tool.Stdout != "WebSocket headers passed\n" || tool.Stderr != "" {
								t.Errorf("WebSocket tool result=%+v", tool)
							}
							return agentModelReply{Content: "WebSocket headers passed"}
						}
					}
					return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("websocket-headers", "// Connect with explicit authentication headers.\n"+websocketHeadersProgram)}}}, FinishReason: "tool_calls"}
				})
				defer provider.Close()
				env := agentCLIEnv(t, provider.URL)
				env["NOE_AGENT_TRACE"] = ""
				result = runCLI(t, work, env, "eval", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"check WebSocket headers");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));console.log(result.lastResult.answer);`)
			}
			requireExit(t, result, 0)
			if result.stdout != "WebSocket headers passed\n" || result.stderr != "" {
				t.Fatalf("%s result=%+v", mode, result)
			}
		})
	}
}
