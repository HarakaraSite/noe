//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func TestCommittedAnswerSurvivesMissingFinishReport(t *testing.T) {
	t.Setenv("NOE_CONTROL_REAL_BIN", lifecycleBinary(t))
	// Exercise the actual two-phase protocol: commit the answer, then let the
	// process exit without sending finish. Exit 0 alone must not mean success.
	executable := writeFakeNOE(t, `exec "$NOE_CONTROL_REAL_BIN" eval '
import {request} from "noe:agent/internal";
const {runId,snapshot}=JSON.parse(await request("start",JSON.stringify({provider:"openrouter",model:"fixture/model",name:"commit-only",effort:"auto"})));
const messages=snapshot.entries.flatMap(entry=>entry.messages);
const {modelStepId}=JSON.parse(await request("begin_model_step",JSON.stringify({runId,messages})));
const response=JSON.parse(await request("model",JSON.stringify({runId,modelStepId,model:"fixture/model",effort:"auto",messages})));
snapshot.entries.push({id:"history-"+(++snapshot.sequence),kind:"block",messages:[response.message]});
snapshot.revision++;
const result=JSON.parse(await request("commit_final",JSON.stringify({runId,answer:response.message.content,snapshot})));
if(!result.committed)throw Error("commit rejected");
'
`)
	observed := make(chan events.Event, 100)
	p := &fixtureProvider{gen: func(context.Context, provider.Request) (provider.Response, error) {
		return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"committed before process exit"}`), FinishReason: "stop"}, nil
	}}
	h := NewHost(Options{Provider: p, Executable: executable, Cwd: t.TempDir(), Observe: func(event events.Event) { observed <- event }})
	defer h.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	op := h.NewOperation(ctx)
	defer op.Close()
	instruction := "fixture commits but omits finish"
	id := spawnLifecycle(t, op, &instruction, false)
	result := lifecycleSnapshot(t, op, id, true)
	if result.LastResult == nil || result.LastResult.State != "failed" || result.LastResult.Answer != "committed before process exit" || result.LastResult.Error != "task process exited without an authenticated completion report" {
		t.Fatalf("lost committed answer or misreported task success: %+v", result.LastResult)
	}
	final, failed, completed := 0, 0, 0
	for len(observed) > 0 {
		switch (<-observed)["type"] {
		case "agent.final":
			final++
		case "agent.failed":
			failed++
		case "agent.completed":
			completed++
		}
	}
	if final != 1 || failed != 1 || completed != 0 || p.callCount() != 1 {
		t.Fatalf("events/provider calls final=%d failed=%d completed=%d calls=%d", final, failed, completed, p.callCount())
	}
}
