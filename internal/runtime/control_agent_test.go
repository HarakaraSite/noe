package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent/provider"
	"noe/internal/events"
)

func TestPublicSteerAgentControlsAnActiveTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	providerStarted := filepath.Join(t.TempDir(), "provider-started")
	started := make(chan provider.Request, 3)
	release := make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var calls atomic.Int32
	p := agentProviderFunc(func(callCtx context.Context, request provider.Request) (provider.Response, error) {
		if calls.Add(1) == 1 {
			if err := os.WriteFile(providerStarted, []byte("started"), 0600); err != nil {
				return provider.Response{}, err
			}
			started <- request
			select {
			case <-release:
				return provider.Response{Message: json.RawMessage(`{"role":"assistant","content":"candidate","reasoning_details":[{"text":"preserved"}]}`), FinishReason: "stop"}, nil
			case <-callCtx.Done():
				return provider.Response{}, callCtx.Err()
			}
		}
		started <- request
		return agentAnswer("public steer applied"), nil
	})
	accepted := make(chan struct{}, 1)
	options := agentTestOptions(p)
	options.Observe = func(event events.Event) {
		if event["type"] == "command.accepted" && event["commandId"] == "public-steer-1" {
			accepted <- struct{}{}
		}
	}
	source := fmt.Sprintf(`import {spawn_agent,observe_agent,steer_agent} from "noe:agent";
import {existsSync} from "node:fs";
const providerStarted=%q;
const {agentId}=await spawn_agent({provider:"openrouter",model:"fixture/model",name:"public-steer",effort:"auto",maxSteps:2,cacheKey:"public-steer-cache"},"initial instruction");
let snapshot;
for(let i=0;i<5000;i++){snapshot=await observe_agent({agentId});if(snapshot.currentRunId)break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot || !snapshot.currentRunId)throw Error("task did not start");
for(let i=0;i<5000&&!existsSync(providerStarted);i++)await new Promise(resolve=>setTimeout(resolve,1));
if(!existsSync(providerStarted))throw Error("provider did not begin the first request");
const ack=await steer_agent({runId:snapshot.currentRunId,instruction:"add a public correction",commandId:"public-steer-1"});
if(ack.status!=="accepted")throw Error(JSON.stringify(ack));
for(let i=0;i<5000;i++){snapshot=await observe_agent({agentId});if(snapshot.state==="stopped"||snapshot.state==="failed")break;await new Promise(resolve=>setTimeout(resolve,1));}
if(!snapshot || !snapshot.lastResult || snapshot.lastResult.answer!=="public steer applied")throw Error(JSON.stringify(snapshot));
console.log("public steer completed");`, providerStarted)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	type execution struct {
		code int
		err  error
	}
	done := make(chan execution, 1)
	go func() {
		code, err := Execute(ctx, Source{"control-agent.js", source}, Options{Stdout: &stdout, Stderr: &stderr, Agent: options})
		done <- execution{code: code, err: err}
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatalf("task did not reach provider: %v", ctx.Err())
	}
	select {
	case <-accepted:
		close(release)
	case <-ctx.Done():
		t.Fatalf("public steer was not accepted: %v", ctx.Err())
	}
	var second provider.Request
	select {
	case second = <-started:
	case <-ctx.Done():
		t.Fatalf("steered provider request did not arrive: %v", ctx.Err())
	}
	if len(second.Messages) != 4 || second.CacheKey != "public-steer-cache" {
		t.Fatalf("public steer history/cache: %+v", second)
	}
	var candidate struct {
		ReasoningDetails []map[string]string `json:"reasoning_details"`
	}
	if err := json.Unmarshal(second.Messages[2], &candidate); err != nil || len(candidate.ReasoningDetails) != 1 || candidate.ReasoningDetails[0]["text"] != "preserved" {
		t.Fatalf("full final candidate was not retained: %s (%v)", second.Messages[2], err)
	}
	var steer struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(second.Messages[3], &steer); err != nil || steer.Role != "user" || steer.Content != "add a public correction" {
		t.Fatalf("public steer was not sent as user input: %+v (%v)", steer, err)
	}
	select {
	case result := <-done:
		if result.code != 0 || result.err != nil || stdout.String() != "public steer completed\n" {
			t.Fatalf("code=%d error=%v stdout=%q", result.code, result.err, stdout.String())
		}
	case <-ctx.Done():
		t.Fatalf("runtime did not complete: %v", ctx.Err())
	}
}
