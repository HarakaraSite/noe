package main

import (
	"context"
	"encoding/json"
	"fmt"
)

// mockProvider scripts named demo scenarios. It is deliberately not a model.
type mockProvider struct{}

const sumCode = `const {readFile}=await import("fs/promises");
const values=JSON.parse(await readFile(process.env.NOE_AGENT_FIXTURES+"/%s.json","utf8"));
console.log(JSON.stringify({count:values.length,total:values.reduce((a,b)=>a+b,0),average:values.reduce((a,b)=>a+b,0)/values.length}));`
const delegateCode = `const children = ["left","right"].map(name=>createAgent({name}));
const results=await Promise.all(children.map((child,i)=>child.run("Sum the "+["left","right"][i]+" fixture using run_js. Return JSON with total.",{cacheKey:"spike-child-"+["left","right"][i]})));
const parts=results.map(JSON.parse);
console.log(JSON.stringify({parts,total:parts.reduce((sum,p)=>sum+p.total,0)}));`

func (mockProvider) generate(ctx context.Context, q modelRequest) (modelResponse, error) {
	if err := ctx.Err(); err != nil {
		return modelResponse{}, err
	}
	var results []jsResult
	for _, raw := range q.Messages {
		var msg struct{ Role, Content string }
		_ = json.Unmarshal(raw, &msg)
		if msg.Role == "tool" {
			var result jsResult
			_ = json.Unmarshal([]byte(msg.Content), &result)
			results = append(results, result)
		}
	}
	var code string
	if len(results) == 0 {
		switch q.Name {
		case "single", "left", "right":
			name := q.Name
			if name == "single" {
				name = "numbers"
			}
			code = fmt.Sprintf(sumCode, name)
		case "recovery":
			code = fmt.Sprintf(sumCode, "missing")
		case "delegate":
			code = delegateCode
		case "service":
			code = `console.log(JSON.stringify(await webSearch("NOE spike")));`
		case "loop":
			code = `console.log("continue");`
		default:
			return modelResponse{}, fmt.Errorf("mock has no scenario named %q", q.Name)
		}
	} else if q.Name == "recovery" && len(results) == 1 {
		code = fmt.Sprintf(sumCode, "numbers")
	} else if q.Name == "loop" {
		code = `console.log("continue");`
	}
	response := modelResponse{Usage: json.RawMessage(`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`)}
	if code != "" {
		args, _ := json.Marshal(map[string]string{"code": code})
		response.Message = map[string]any{"role": "assistant", "content": nil, "reasoning_details": []any{map[string]any{"type": "reasoning.text", "text": "mock reasoning fixture", "index": 0}}, "tool_calls": []any{map[string]any{"id": fmt.Sprintf("call-%d", len(results)+1), "type": "function", "function": map[string]any{"name": "run_js", "arguments": string(args)}}}}
		response.FinishReason = "tool_calls"
	} else {
		last := results[len(results)-1]
		if last.ExitCode != 0 {
			return modelResponse{}, fmt.Errorf("mock tool failed: %s", last.Stderr)
		}
		response.Message = map[string]any{"role": "assistant", "content": last.Stdout}
		response.FinishReason = "stop"
	}
	return response, nil
}
