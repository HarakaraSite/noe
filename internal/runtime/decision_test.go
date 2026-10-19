package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"noe/internal/agent"
	"noe/internal/decision"
)

type decisionProviderFunc func(context.Context, decision.Request) (decision.Response, error)

func (f decisionProviderFunc) Decide(ctx context.Context, r decision.Request) (decision.Response, error) {
	return f(ctx, r)
}

func echoDecision(_ context.Context, r decision.Request) (decision.Response, error) {
	raw, err := json.Marshal(map[string]any{"model": r.Model, "answers": map[string]any{"q": map[string]any{"type": "future", "state": r.State, "question": r.Questions["q"]}}, "extension": true})
	return decision.Response{JSON: raw, StatusCode: 200}, err
}

func TestDecisionModuleParallelAndInputValidation(t *testing.T) {
	var calls atomic.Int32
	p := decisionProviderFunc(func(ctx context.Context, r decision.Request) (decision.Response, error) {
		calls.Add(1)
		return echoDecision(ctx, r)
	})
	var out bytes.Buffer
	source := `import {decide} from "noe:decision";
const base={provider:"openrouter",model:"decision/model",state:{value:42},questions:{q:{type:"future",unknown:{keep:true}}}};
const [a,b]=await Promise.all([decide(base),decide({...base,state:["other"]})]);
if(a.answers.q.state.value!==42 || b.answers.q.state[0]!=="other" || !a.extension || !a.answers.q.question.unknown.keep)throw Error("lost JSON");
const cycle={};cycle.self=cycle;
let rejected=0;
for(const options of [null,{}, {...base,provider:undefined},{...base,model:undefined},{...base,effort:"auto"},{...base,state:" "},{...base,state:null},{...base,questions:[]},{...base,questions:{q:null}},{...base,state:cycle},{...base,state:{bad:NaN}},{...base,state:{bad:undefined}},{...base,state:[()=>{}]},{...base,state:{big:1n}}]){
 const promise=decide(options);if(!(promise instanceof Promise))throw Error("not a promise");
 try{await promise;}catch(e){if(!(e instanceof TypeError))throw e;rejected++;}
}
if(rejected!==14)throw Error("validation count "+rejected);
console.log("decisions ready");`
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "decision.js"), source}, Options{Stdout: &out, Agent: &agent.Options{DecisionProvider: p}})
	if code != 0 || err != nil || out.String() != "decisions ready\n" || calls.Load() != 2 {
		t.Fatalf("code=%d err=%v out=%q calls=%d", code, err, out.String(), calls.Load())
	}
}

func TestDecisionImportNeedsNoCredentials(t *testing.T) {
	t.Setenv("NOE_OPENROUTER_API_KEY_FILE", filepath.Join(t.TempDir(), "missing"))
	code, err := Execute(context.Background(), Source{filepath.Join(t.TempDir(), "lazy.js"), `const api=await import("noe:decision");if(Object.keys(api).join()!=="decide")throw Error("exports");`}, Options{})
	if code != 0 || err != nil {
		t.Fatalf("code=%d err=%v", code, err)
	}
}

func TestDecisionUnawaitedWorkIsTrackedAndCancelled(t *testing.T) {
	started := make(chan struct{})
	stopped := make(chan struct{})
	p := decisionProviderFunc(func(ctx context.Context, _ decision.Request) (decision.Response, error) {
		close(started)
		<-ctx.Done()
		close(stopped)
		return decision.Response{}, ctx.Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() { <-started; cancel() }()
	code, err := Execute(ctx, Source{filepath.Join(t.TempDir(), "cancel.js"), `import {decide} from "noe:decision";decide({provider:"openrouter",model:"m",state:"x",questions:{q:{}}});`}, Options{Agent: &agent.Options{DecisionProvider: p}})
	if code != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("code=%d err=%v", code, err)
	}
	select {
	case <-stopped:
	default:
		t.Fatal("provider not recovered before execution returned")
	}
}
