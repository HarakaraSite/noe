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

func TestNoeHTTPReceiveAgentRunJSSavesResultClosesServerAndAnswers(t *testing.T) {
	fixture := newHTTPReceiveFixture(t, "agent")
	dir := t.TempDir()
	artifact := filepath.Join(dir, "receive-result.json")
	code := fmt.Sprintf(`import {fetch} from "noe:http";
import {writeFile} from "node:fs/promises";
const fixture=%q, output=%q;
let resolveReceived;
const received=new Promise(resolve=>{resolveReceived=resolve;});
const server=serve({port:0},async request=>{
 const reader=request.body.getReader();let byteCount=0,sum=0,prefix=[];
 for(;;){
  const {value,done}=await reader.read();if(done)break;
  for(const byte of value){byteCount++;sum+=byte;if(prefix.length<3)prefix.push(byte);}
  if(prefix.length===3){
   const ack=await fetch(fixture+"/prefix?mode=agent");
   if(ack.status!==204)throw Error("prefix acknowledgement failed");
   break;
  }
 }
 if(prefix.length===3){for(;;){const {value,done}=await reader.read();if(done)break;for(const byte of value){byteCount++;sum+=byte;}}}
 const result={byteCount,sum};resolveReceived(result);return Response.json(result);
});
const ready=await fetch(fixture+"/ready",{method:"POST",body:JSON.stringify({agent:server.url})});
if(ready.status!==204)throw Error("could not publish receive listener");
const result=await received;
await fetch(fixture+"/wait");
server.close();
await writeFile(output,JSON.stringify(result));
console.log(JSON.stringify(result));`, fixture.server.URL, artifact)

	var steps atomic.Int32
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		step := steps.Add(1)
		if step == 1 {
			return hookToolReply(map[string]string{"receive": code}, "receive"), nil
		} else if step == 2 {
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
			if len(toolResults) != 1 {
				return provider.Response{}, fmt.Errorf("run_js receive result missing: %d tool results", len(toolResults))
			}
			var result struct {
				Stdout string `json:"stdout"`
			}
			if err := json.Unmarshal([]byte(toolResults[0]), &result); err != nil {
				return provider.Response{}, fmt.Errorf("decode run_js receive result: %w", err)
			}
			if strings.TrimSpace(result.Stdout) != `{"byteCount":7,"sum":689}` {
				return provider.Response{}, fmt.Errorf("unexpected run_js receive output %q", result.Stdout)
			}
			return agentAnswer("Received 7 bytes, saved the result, and closed the server."), nil
		}
		return provider.Response{}, fmt.Errorf("unexpected fixture provider step %d", step)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2},"Receive the binary request body, save its count and sum, close the listener, and report the result.");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result.lastResult));
console.log(result.lastResult.answer);`
	execution := make(chan struct {
		code int
		err  error
	}, 1)
	go func() {
		code, err := Execute(ctx, Source{Name: filepath.Join(dir, "main.mjs"), Code: source}, Options{
			Cwd: dir, Agent: agentTestOptions(p), Stdout: &stdout, Stderr: &stderr,
		})
		execution <- struct {
			code int
			err  error
		}{code, err}
	}()
	defer fixture.release()

	urls := fixture.waitReady(t)
	conn := beginHTTPReceiveRequest(t, urls["agent"], "/receive", int64(len(httpReceivePrefix)+len(httpReceiveRemainder)), httpReceivePrefix)
	fixture.waitPrefix(t, "agent")
	status, body := finishHTTPReceiveRequest(t, conn, httpReceiveRemainder)
	var got struct {
		ByteCount int `json:"byteCount"`
		Sum       int `json:"sum"`
	}
	if err := json.Unmarshal(body, &got); err != nil || status != http.StatusOK || got.ByteCount != 7 || got.Sum != 689 {
		t.Fatalf("receive response: status=%d body=%q decoded=%+v err=%v", status, body, got, err)
	}
	fixture.release()
	select {
	case result := <-execution:
		if result.code != 0 || result.err != nil || stdout.String() != "Received 7 bytes, saved the result, and closed the server.\n" || stderr.Len() != 0 || steps.Load() != 2 {
			t.Fatalf("agent execution: code=%d err=%v steps=%d stdout=%q stderr=%q", result.code, result.err, steps.Load(), &stdout, &stderr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("fixture agent did not finish run_js and return its final answer")
	}
	saved, err := os.ReadFile(artifact)
	var savedResult struct {
		ByteCount int `json:"byteCount"`
		Sum       int `json:"sum"`
	}
	if err != nil || json.Unmarshal(saved, &savedResult) != nil || savedResult.ByteCount != 7 || savedResult.Sum != 689 {
		t.Fatalf("saved run_js result=%q decoded=%+v err=%v", saved, savedResult, err)
	}
	requireHTTPReceivePortReleased(t, urls["agent"])
}
