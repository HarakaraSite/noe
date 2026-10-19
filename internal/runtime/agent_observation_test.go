package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent"
	"noe/internal/agent/provider"
)

func writeObservationLog(t *testing.T, path string, rows ...map[string]any) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	for _, row := range rows {
		if err := json.NewEncoder(file).Encode(row); err != nil {
			t.Fatal(err)
		}
	}
}

func TestObserveAgentReadsFinalAndCursorWithoutProvider(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "events.jsonl")
	writeObservationLog(t, path,
		map[string]any{"version": 1, "executionId": "exec", "seq": 1, "type": "agent.started", "runId": "child", "parentRunId": "parent", "name": "child-A", "model": "fixture"},
		map[string]any{"version": 1, "executionId": "exec", "seq": 2, "type": "thinking", "runId": "child", "requestId": "m1", "text": "考え"},
		map[string]any{"version": 1, "executionId": "exec", "seq": 3, "type": "thinking", "runId": "child", "requestId": "m1", "text": "ています"},
		map[string]any{"version": 1, "executionId": "exec", "seq": 4, "type": "agent.final", "runId": "child", "text": "完了しました"},
		map[string]any{"version": 1, "executionId": "exec", "seq": 5, "type": "agent.completed", "runId": "child"},
	)
	var calls atomic.Int32
	p := agentProviderFunc(func(context.Context, provider.Request) (provider.Response, error) {
		calls.Add(1)
		return agentAnswer("unexpected"), nil
	})
	encodedPath, _ := json.Marshal(path)
	source := `const fixturePath=` + string(encodedPath) + `;
import {observe_agent} from "noe:agent";
const options={logPath:"events.jsonl",runId:"child"};
const first=await observe_agent(options);
if(first.status!=="completed" || first.parentRunId!=="parent" || first.finalText!=="完了しました") throw Error(JSON.stringify(first));
if(first.events.some(e=>e.type==="thinking")) throw Error("thinking included by default");
const detailed=await observe_agent({...options,includeThinking:true});
if(detailed.events.filter(e=>e.type==="thinking").map(e=>e.text).join("")!=="考えています") throw Error("missing thinking");
const again=await observe_agent({...options,cursor:first.cursor,wait:true});
if(again.events.length!==0 || again.finalText!==first.finalText) throw Error("cursor lost snapshot state");
const missing=await observe_agent({...options,runId:"later"});
if(missing.status!=="unknown")throw Error("unknown run state");
const fs=await import("fs/promises");
const before=await fs.readFile(fixturePath,"utf8");
const added=[{type:"agent.started",seq:6},{type:"agent.final",seq:7,text:"later done"},{type:"agent.completed",seq:8}]
  .map(e=>JSON.stringify({...e,version:1,executionId:"exec",runId:"later"})).join("\n")+"\n";
await fs.writeFile(fixturePath,before+added);
const appeared=await observe_agent({...options,runId:"later",cursor:missing.cursor});
if(appeared.finalText!=="later done")throw Error("returned unknown cursor cannot be reused");
let rejected=false;try{await observe_agent({...options,wait:"yes"})}catch(e){rejected=e instanceof TypeError}
if(!rejected)throw Error("invalid wait type accepted");`
	code, err := Execute(context.Background(), Source{filepath.Join(dir, "main.js"), source}, Options{Cwd: dir, Agent: &agent.Options{Provider: p}})
	if err != nil || code != 0 || calls.Load() != 0 {
		t.Fatalf("code=%d error=%v provider calls=%d", code, err, calls.Load())
	}
}

func TestObserveAgentWaitYieldsToJSAndIsCancelledWithEvaluation(t *testing.T) {
	dir := t.TempDir()
	writeObservationLog(t, filepath.Join(dir, "events.jsonl"), map[string]any{"version": 1, "executionId": "exec", "seq": 1, "type": "agent.started", "runId": "child"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ticked atomic.Bool
	// A native observer wait must yield to the owner's event loop; a timer can
	// run while Go waits for file events, then evaluation cancellation drains it.
	settings := Options{Cwd: dir}
	h, err := newHost(settings)
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	if err := h.rt.Set("tick", func() {
		ticked.Store(true)
		// Cancel only after proving that observation yielded to the timer.
		// The timeout guards a blocked loop, rather than timing host startup.
		cancel()
	}); err != nil {
		t.Fatal(err)
	}
	code, err := h.run(ctx, Source{filepath.Join(dir, "main.js"), `import {observe_agent} from "noe:agent";
setTimeout(()=>tick(),10);
await observe_agent({logPath:"events.jsonl",runId:"child",wait:true});`})
	if code == 0 || err == nil || !errors.Is(ctx.Err(), context.Canceled) || !ticked.Load() {
		t.Fatalf("code=%d err=%v ticked=%v", code, err, ticked.Load())
	}
}
