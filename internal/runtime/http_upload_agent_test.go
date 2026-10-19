package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent/provider"
)

func TestNoeHTTPUploadAgentConsultsFetchDocsAndSavesRunJSResult(t *testing.T) {
	fixture := newUploadTestFixture(t, "agent")
	dir := t.TempDir()
	artifact := filepath.Join(dir, "upload-result.json")
	var steps atomic.Int32
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		step := steps.Add(1)
		var toolResults []string
		for _, raw := range req.Messages {
			var message struct{ Role, Content string }
			if err := json.Unmarshal(raw, &message); err != nil {
				return provider.Response{}, err
			}
			if message.Role == "tool" {
				toolResults = append(toolResults, message.Content)
			}
		}
		switch step {
		case 1:
			return hookToolReply(map[string]string{
				"lookup": `// Read fetch's request-stream contract.
const {docs}=await import("noe:docs");console.log(docs("noe:http","fetch"));`,
			}, "lookup"), nil
		case 2:
			if len(toolResults) != 1 || !strings.Contains(toolResults[0], "Uint8Array") || !strings.Contains(strings.ToLower(toolResults[0]), "stream") {
				return provider.Response{}, fmt.Errorf("fetch streaming documentation was not delivered to run_js: %q", toolResults)
			}
			code := fmt.Sprintf(`// Upload a stream only after consulting noe:http.fetch.
import {fetch} from "noe:http";
import {writeFileSync} from "node:fs";
const base=%q, output=%q;
const storage=new Uint8Array([99,0,255,17,88]);
let pull=0;
const body=new ReadableStream({async pull(controller){
 if(pull++===0){controller.enqueue(storage.subarray(1,4));return;}
 if(pull===2){controller.enqueue(new Uint8Array(0));return;}
 const ack=await fetch(base+"/ack?label=agent");
 if(ack.status!==204)throw Error("prefix acknowledgement failed");
 controller.enqueue(new Uint8Array([34,128,0,255]));controller.close();
}},{highWaterMark:0});
const response=await fetch(base+"/upload?label=agent",{method:"POST",body});
const result={status:response.status,body:await response.text()};
writeFileSync(output,JSON.stringify(result));
console.log(JSON.stringify(result));`, fixture.baseURL(), artifact)
			return hookToolReply(map[string]string{"upload": code}, "upload"), nil
		case 3:
			if len(toolResults) != 2 {
				return provider.Response{}, fmt.Errorf("run_js upload result is missing: %d tool results", len(toolResults))
			}
			var result struct {
				Stdout string `json:"stdout"`
			}
			if err := json.Unmarshal([]byte(toolResults[1]), &result); err != nil {
				return provider.Response{}, fmt.Errorf("decode run_js result: %w", err)
			}
			var upload struct {
				Status int    `json:"status"`
				Body   string `json:"body"`
			}
			if err := json.Unmarshal([]byte(result.Stdout), &upload); err != nil || upload.Status != http.StatusOK || upload.Body != "received" {
				return provider.Response{}, fmt.Errorf("unexpected run_js upload output: %q err=%v", result.Stdout, err)
			}
			return agentAnswer("Stream upload completed and result was saved."), nil
		default:
			return provider.Response{}, fmt.Errorf("unexpected provider step %d", step)
		}
	})

	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"Consult the noe:http fetch docs, stream a binary upload, save its result, and report completion.");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result.lastResult));
console.log(result.lastResult.answer);`
	code, err := Execute(ctx, Source{Name: filepath.Join(dir, "main.mjs"), Code: source}, Options{
		Cwd: dir, Agent: agentTestOptions(p), Stdout: &stdout, Stderr: &stderr,
	})
	if code != 0 || err != nil || stdout.String() != "Stream upload completed and result was saved.\n" || stderr.Len() != 0 || steps.Load() != 3 {
		t.Fatalf("code=%d err=%v steps=%d stdout=%q stderr=%q", code, err, steps.Load(), &stdout, &stderr)
	}
	fixture.requireBodies(t, "agent")
	data, err := os.ReadFile(artifact)
	var saved struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err != nil || json.Unmarshal(data, &saved) != nil || saved.Status != http.StatusOK || saved.Body != "received" {
		t.Fatalf("saved run_js result=%q err=%v", data, err)
	}
}
