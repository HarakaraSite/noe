package runtime

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/agent/provider"
)

func TestWaitTaskFromHook(t *testing.T) {
	cwd := t.TempDir()
	writePhaseHook(t, cwd, "gate.mjs", `export async function beforeModelRequest(event, api) {
 const child=await api.agents.spawn_agent({name:"gate",model:"gate/model",hooks:[]},"review");
 const result=await api.agents.wait_task(child);
 if(result.state!=="completed" || result.answer!=="approved") throw Error(JSON.stringify(result));
 await api.context.setRuntimeInfo({key:"gate",text:result.answer});
}`)
	p := agentProviderFunc(func(_ context.Context, req provider.Request) (provider.Response, error) {
		if req.Model == "gate/model" {
			return agentAnswer("approved"), nil
		}
		return agentAnswer("done"), nil
	})
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code, err := Execute(ctx, Source{filepath.Join(cwd, "main.js"), `import {run_agent} from "noe:agent";
const result=await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",hooks:["./gate.mjs"]},"task");
if(result.lastResult.state!=="completed")throw Error(JSON.stringify(result));
console.log(result.lastResult.answer);`}, Options{Cwd: cwd, Stdout: &out, Agent: agentTestOptions(p)})
	if code != 0 || err != nil || out.String() != "done\n" {
		t.Fatalf("code=%d err=%v stdout=%q", code, err, &out)
	}
}

func TestWaitTaskYieldsToJSAndOwnerCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p := agentProviderFunc(func(ctx context.Context, _ provider.Request) (provider.Response, error) {
		<-ctx.Done()
		return provider.Response{}, ctx.Err()
	})
	cwd := t.TempDir()
	source := Source{filepath.Join(cwd, "main.js"), `import {spawn_agent,wait_task} from "noe:agent";
const task=await spawn_agent({provider:"openrouter",model:"fixture/model",effort:"auto"},"wait");
const pending=wait_task(task);
setTimeout(()=>tick(),10);
await pending;`}
	h, err := newHost(ctx, Options{Cwd: cwd, Agent: agentTestOptions(p)}, source)
	if err != nil {
		t.Fatal(err)
	}
	defer h.rt.Close()
	var ticked atomic.Bool
	if err := h.rt.Set("tick", func() { ticked.Store(true); cancel() }); err != nil {
		t.Fatal(err)
	}
	code, err := h.run()
	if code == 0 || err == nil || !errors.Is(ctx.Err(), context.Canceled) || !ticked.Load() {
		t.Fatalf("code=%d err=%v ticked=%v", code, err, ticked.Load())
	}
}
