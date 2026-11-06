//go:build e2e

package e2e_test

import (
	"context"
	"encoding/base64"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

const mappedE2EProgram = "\"use strict\";\nfunction compiled(){\n throw new Error(\"source map marker\");\n}\ncompiled();\n"
const mappedE2EMap = `{"version":3,"sources":["original.ts"],"names":["original"],"mappings":";;AAKAA"}`

func mappedE2ESource(t *testing.T, work, kind string) string {
	t.Helper()
	reference := "data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte(mappedE2EMap))
	if kind == "sidecar" {
		name := "map 日本語 space#%.map"
		writeFixture(t, filepath.Join(work, "maps", name), mappedE2EMap)
		reference = "maps/" + url.PathEscape(name)
	}
	return mappedE2EProgram + "//# sourceMappingURL=" + reference
}

func TestSourceMapsAcrossExecutionModes(t *testing.T) {
	for _, kind := range []string{"inline", "sidecar"} {
		for _, mode := range []string{"run", "eval", "worker", "events"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				work := t.TempDir()
				source := mappedE2ESource(t, work, kind)
				var result cliResult
				switch mode {
				case "run":
					writeFixture(t, filepath.Join(work, "bundle.js"), source)
					result = runCLI(t, work, nil, "run", "bundle.js")
				case "eval":
					result = runCLI(t, work, nil, "eval", source)
				case "events":
					result = runCLI(t, work, nil, "eval", "--events=-", source)
				case "worker":
					writeFixture(t, filepath.Join(work, "bundle.js"), source)
					result = runCLI(t, work, nil, "eval", `import {Worker} from "node:worker_threads";
const worker=new Worker("./bundle.js");let failure;
await new Promise((resolve,reject)=>{
 worker.once("error",error=>{failure=error;resolve()});
 worker.once("exit",code=>{if(!failure)reject(Error("worker exited without exception: "+code))});
});
if(!failure.stack.includes("original.ts:6:1")||!failure.message.includes("source map marker"))throw Error("worker stack: "+failure.stack);
console.log("worker source map passed");`)
				}
				if mode == "worker" {
					requireExit(t, result, 0)
					if result.stderr != "" || !strings.Contains(result.stdout, "worker source map passed") {
						t.Fatalf("worker mapping result=%+v", result)
					}
					return
				}
				requireExit(t, result, 1)
				if !strings.Contains(result.stderr, "original.ts:6:1") || !strings.Contains(result.stderr, "source map marker") {
					t.Fatalf("mapped CLI diagnostic=%+v", result)
				}
			})
		}
	}
}

func TestSourceMapsInFixtureAgentRunJS(t *testing.T) {
	work := t.TempDir()
	source := mappedE2ESource(t, work, "inline")
	server := agentAPIFixture(t, func(_ context.Context, request agentChatRequest) agentModelReply {
		for _, message := range request.Messages {
			if message.Role == "tool" {
				result := agentToolResult(t, message)
				if result.ExitCode != 1 || len(result.Diagnostics) != 1 || !strings.Contains(result.Diagnostics[0].Stack, "original.ts:6:1") || !strings.Contains(result.Diagnostics[0].Message, "source map marker") {
					t.Errorf("tool source-map diagnostic=%+v", result)
				}
				return agentModelReply{Content: "mapped diagnostic received"}
			}
		}
		return agentModelReply{Deltas: []map[string]any{{"tool_calls": []any{agentTool("mapped-failure", source)}}}, FinishReason: "tool_calls"}
	})
	defer server.Close()
	result := runCLI(t, work, agentCLIEnv(t, server.URL), "eval", `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"inspect the mapped exception");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`)
	requireExit(t, result, 0)
	if !strings.Contains(result.stdout, "mapped diagnostic received") {
		t.Fatalf("source-map agent result=%+v", result)
	}
}
