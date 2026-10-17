package runtime

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"

	"noe/internal/agent/provider"
)

func TestAgentRootRequiresProviderModelAndEffort(t *testing.T) {
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("unexpected"), nil
	})
	source := `import {run_agent} from "noe:agent";
for (const key of ["provider","model","effort"]) {
  for (const omission of ["absent","undefined"]) {
    const config = {provider:"openrouter",model:"fixture/model",effort:"auto"};
    if (omission === "absent") delete config[key];
    else config[key] = undefined;
    let rejected = false;
    try { await run_agent(config,"must not contact provider"); } catch (e) {
      if (!(e instanceof TypeError) || !e.message.includes(key)) throw e;
      rejected = true;
    }
    if (!rejected) throw Error("missing root " + key + " accepted");
  }
}
let rejected = false;
try { await run_agent(); } catch (e) {
  if (!(e instanceof TypeError)) throw e;
  rejected = true;
}
if (!rejected) throw Error("unconfigured root accepted");
`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "main.js"), source}, Options{Agent: agentTestOptions(p)})
	if code != 0 || err != nil || calls.Load() != 0 {
		t.Fatalf("code=%d error=%v provider calls=%d", code, err, calls.Load())
	}
}
