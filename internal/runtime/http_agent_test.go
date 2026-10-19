package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestNoeHTTPAgentConsultsDocsAndRunsRoundtrip(t *testing.T) {
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
		if step == 1 {
			return hookToolReply(map[string]string{"lookup": "// Read the HTTP contract.\nconst {docs}=await import(\"noe:docs\");console.log(docs(\"noe:http\",\"serve\"));"}, "lookup"), nil
		}
		if step == 2 {
			if len(toolResults) != 1 || !strings.Contains(toolResults[0], "noe:http") || !strings.Contains(toolResults[0], "close()") {
				return provider.Response{}, fmt.Errorf("HTTP reference was not delivered")
			}
			return hookToolReply(map[string]string{"roundtrip": `// Exchange JSON over loopback and close the listener.
import {fetch,serve} from "noe:http";
const server=serve({port:0},async r=>Response.json({value:(await r.json()).value*2}));
try{
 const response=await fetch(server.url,{method:"POST",body:JSON.stringify({value:21})});
 console.log(JSON.stringify(await response.json()));
}finally{server.close();}`}, "roundtrip"), nil
		}
		if step != 3 || len(toolResults) != 2 {
			return provider.Response{}, fmt.Errorf("HTTP roundtrip result missing")
		}
		var result struct{ Stdout string }
		var value struct{ Value int }
		if err := json.Unmarshal([]byte(toolResults[1]), &result); err != nil {
			return provider.Response{}, err
		}
		if err := json.Unmarshal([]byte(result.Stdout), &value); err != nil || value.Value != 42 {
			return provider.Response{}, fmt.Errorf("unexpected HTTP result: %q", result.Stdout)
		}
		return agentAnswer("HTTP value 42"), nil
	})
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	var stdout, stderr bytes.Buffer
	source := `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:3},"Exchange JSON over loopback using noe:http and report the value.");
if(result.lastResult?.state!=="completed")throw Error(JSON.stringify(result.lastResult));
console.log(result.lastResult.answer);`
	dir := t.TempDir()
	code, err := Execute(ctx, Source{Name: filepath.Join(dir, "task.mjs"), Code: source}, Options{Cwd: dir, Agent: agentTestOptions(p), Stdout: &stdout, Stderr: &stderr})
	if code != 0 || err != nil || stdout.String() != "HTTP value 42\n" || stderr.Len() != 0 || steps.Load() != 3 {
		t.Fatalf("code=%d err=%v steps=%d stdout=%q stderr=%q", code, err, steps.Load(), &stdout, &stderr)
	}
}
