package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func runReference(args []string) error {
	flags := flagSet("reference")
	noePath := flags.String("noe", "", "absolute path to an independent NOE executable")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("reference accepts only --noe")
	}
	if *noePath == "" || !filepath.IsAbs(*noePath) {
		return errors.New("reference requires --noe with an absolute path")
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	if sameExecutable(self, *noePath) {
		return errors.New("reference NOE executable must be separate from this consumer")
	}
	workDir, err := os.MkdirTemp("", "noe-agent-consumer-reference-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	if err := writeReferenceInputs(workDir); err != nil {
		return err
	}
	marker := filepath.Join(workDir, "consumer-was-reexecuted-as-noe")
	oldMarker, hadMarker := os.LookupEnv("NOE_CONSUMER_SELF_REEXEC_MARKER")
	if err := os.Setenv("NOE_CONSUMER_SELF_REEXEC_MARKER", marker); err != nil {
		return err
	}
	defer func() {
		if hadMarker {
			_ = os.Setenv("NOE_CONSUMER_SELF_REEXEC_MARKER", oldMarker)
		} else {
			_ = os.Unsetenv("NOE_CONSUMER_SELF_REEXEC_MARKER")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Second)
	defer cancel()
	if err := runAgentReference(ctx, *noePath, workDir); err != nil {
		return err
	}
	if err := runQueueAndInterruptReference(ctx, *noePath, workDir); err != nil {
		return err
	}
	if err := runCloseWhileWaitingReference(ctx, *noePath, workDir); err != nil {
		return err
	}
	if err := runRegistryHTTPReference(ctx, *noePath, workDir); err != nil {
		return err
	}
	if err := runAuthReference(ctx, *noePath, workDir); err != nil {
		return err
	}
	if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
		return errors.New("consumer self-reexecution marker was written; a separate NOE executable was not used")
	}
	fmt.Println("reference consumer passed: aggregate, control, registry HTTP, and auth")
	return nil
}

func flagSet(name string) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	return flags
}

func sameExecutable(a, b string) bool {
	left, leftErr := filepath.EvalSymlinks(a)
	right, rightErr := filepath.EvalSymlinks(b)
	if leftErr == nil && rightErr == nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func writeReferenceInputs(workDir string) error {
	files := map[string]string{
		"north.csv":        "name,amount\nA,4\nB,7\n",
		"south.csv":        "name,amount\nC,11\n",
		"child-helper.mjs": `export function classifyTotal(total) { return total === 22 ? "expected-total" : "wrong-total"; }`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0600); err != nil {
			return err
		}
	}
	return nil
}

func runAgentReference(ctx context.Context, noePath, workDir string) error {
	provider := newReferenceProvider()
	imageProvider := &referenceImageProvider{png: fixturePNG()}
	var decisionCalls atomic.Int32
	decisionProvider := referenceDecisionProvider{calls: &decisionCalls}
	var loadCalls atomic.Int32
	var observedMu sync.Mutex
	observed := map[string]int{}
	trace := &borrowedSink{}
	configPath := filepath.Join(workDir, "provider-config.json")
	keyPath := filepath.Join(workDir, "provider-key")
	config := `{"providers":{"openrouter":{"models":{"fixture/model":{"catalog":{"description":"external consumer fixture"}}}}}}`
	if err := os.WriteFile(configPath, []byte(config), 0600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte("local-fixture-key"), 0600); err != nil {
		return err
	}
	childMap := json.RawMessage(`{"imports":{"fixture-child":"./child-helper.mjs"}}`)
	sink := &borrowedSink{}
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	if _, err := stdlib.Install(rt, stdlib.Config{Core: stdlib.CoreConfig{NoWebAPIs: true, Loop: loop, Stdout: sink, Stderr: sink}}); err != nil {
		_ = rt.Close()
		return fmt.Errorf("install reference partial stdlib: %w", err)
	}
	operationCtx, operationCancel := context.WithCancel(ctx)
	defer operationCancel()
	binding, err := publicagent.Install(operationCtx, rt, publicagent.Config{
		Loop: loop, Cwd: workDir, Executable: noePath,
		Providers: publicagent.ProviderOptions{ConfigFile: configPath, KeyFile: keyPath, UserAgent: "noe-agent-external-consumer-fixture"},
		Provider:  provider, DecisionProvider: decisionProvider, ImageProvider: imageProvider,
		ChildImportMap: childMap,
		Load: func(specifier, referrer string) (string, string, error) {
			if specifier != "fixture:root" {
				return "", "", fmt.Errorf("unexpected root module %q from %q", specifier, referrer)
			}
			loadCalls.Add(1)
			return `export const rootMarker = "external-root-loader";`, "fixture:root", nil
		},
		Observe: func(event publicagent.Event) {
			name, _ := event["type"].(string)
			observedMu.Lock()
			observed[name]++
			observedMu.Unlock()
		},
		Trace: trace,
	})
	if err != nil {
		_ = rt.Close()
		return fmt.Errorf("install reference agent: %w", err)
	}
	// ChildImportMap is a snapshot. Mutating the caller's bytes after Install
	// must not change the map passed to task and run_js processes.
	for i := range childMap {
		childMap[i] = 'x'
	}
	defer func() {
		binding.Close()
		_ = rt.Close()
	}()

	rootSource := `
		import {run_agent} from "noe:agent";
		import {rootMarker} from "fixture:root";
		if (rootMarker !== "external-root-loader") throw Error("root Load was not used");
		globalThis.fixtureConfig = {provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:4};
		run_agent(globalThis.fixtureConfig,"step72 aggregate fixture")
		  .then(value => { globalThis.fixtureAggregateOutcome = {value}; fixtureAggregateDone(); },
		        error => { globalThis.fixtureAggregateOutcome = {error:String(error && error.message || error)}; fixtureAggregateDone(); });
	`
	resultBytes, err := runPromise(ctx, rt, loop, "fixtureAggregateDone", "fixture-root.mjs", rootSource)
	if err != nil {
		return fmt.Errorf("external run_agent/task/run_js flow: %w", err)
	}
	if err := verifyJavaScriptErrorReference(ctx, rt, loop); err != nil {
		return err
	}
	var result struct {
		State      string `json:"state"`
		LastResult struct {
			State  string `json:"state"`
			Answer string `json:"answer"`
		} `json:"lastResult"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return fmt.Errorf("decode aggregate task result: %w", err)
	}
	if result.State != "stopped" || result.LastResult.State != "completed" || result.LastResult.Answer != "aggregate fixture complete" {
		return fmt.Errorf("aggregate task returned an unexpected state: %s; fixture detail: %s", resultBytes, provider.failure())
	}
	var report struct {
		Count    int    `json:"count"`
		Total    int    `json:"total"`
		Decision string `json:"decision"`
		Nested   string `json:"nested"`
		Category string `json:"category"`
	}
	reportBytes, err := os.ReadFile(filepath.Join(workDir, "summary.json"))
	if err != nil {
		return fmt.Errorf("run_js did not save summary.json: %w", err)
	}
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		return fmt.Errorf("decode run_js summary: %w", err)
	}
	if report.Count != 3 || report.Total != 22 || report.Decision != "yes" || report.Nested != "nested-agent-ok" || report.Category != "expected-total" {
		return fmt.Errorf("run_js aggregate report mismatch: %+v", report)
	}
	imageBytes, err := os.ReadFile(filepath.Join(workDir, "generated.png"))
	if err != nil || string(imageBytes) != string(imageProvider.png) {
		return fmt.Errorf("injected image provider did not save the fixture image: %v", err)
	}
	if loadCalls.Load() == 0 || decisionCalls.Load() != 1 || imageProvider.calls.Load() != 1 || provider.calls.Load() < 3 {
		return fmt.Errorf("injected boundaries were not exercised: load=%d decision=%d image=%d chat=%d", loadCalls.Load(), decisionCalls.Load(), imageProvider.calls.Load(), provider.calls.Load())
	}
	observedMu.Lock()
	runJSCount, agentStarts := observed["run_js.started"], observed["agent.started"]
	observedMu.Unlock()
	if runJSCount == 0 || agentStarts < 2 {
		return fmt.Errorf("root broker did not observe run_js and nested agent work: run_js=%d agents=%d", runJSCount, agentStarts)
	}
	binding.Close()
	if binding.Context().Err() == nil {
		return errors.New("reference Binding.Close left its context active")
	}
	if trace.wasClosed() || sink.wasClosed() {
		return errors.New("closing the agent binding/runtime closed a caller-owned sink")
	}
	if _, err := trace.Write([]byte("trace-still-borrowed")); err != nil {
		return fmt.Errorf("trace sink is unusable after Binding.Close: %w", err)
	}
	if _, err := sink.Write([]byte("stdout-still-borrowed")); err != nil {
		return fmt.Errorf("stdlib sink is unusable after Binding.Close: %w", err)
	}
	return nil
}

func runQueueAndInterruptReference(ctx context.Context, noePath, workDir string) error {
	provider := newReferenceProvider()
	provider.holdStarted = make(chan struct{}, 1)
	provider.holdStopped = make(chan struct{})
	sink := &borrowedSink{}
	rt, loop, binding, err := installAgentRuntime(ctx, noePath, workDir, publicagent.Config{Provider: provider}, sink)
	if err != nil {
		return fmt.Errorf("install queue/interrupt binding: %w", err)
	}
	defer func() { binding.Close(); _ = rt.Close() }()
	if _, err := rt.EvalContext(ctx, `globalThis.fixtureHoldReady = new Promise(resolve => { globalThis.fixtureHoldResolve = resolve; });`); err != nil {
		return fmt.Errorf("prepare interrupt readiness: %w", err)
	}
	go func() {
		select {
		case <-provider.holdStarted:
			loop.Post(func() { _, _ = rt.EvalContext(ctx, `globalThis.fixtureHoldResolve()`) })
		case <-ctx.Done():
		}
	}()
	source := `import {spawn_agent,queue_agent,observe_agent,interrupt_agent} from "noe:agent";
globalThis.controlFixtureStage="spawning";
(async () => {
	const config={provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2};
	async function waitFor(agentId, commandId, state) {
		for (let i=0;i<10000;i++) {
			const snapshot=await observe_agent({agentId});
			if(snapshot.state==="failed")throw Error(JSON.stringify(snapshot));
			if(snapshot.state===state && snapshot.lastResult?.commandId===commandId)return snapshot;
			await new Promise(resolve=>setTimeout(resolve,2));
		}
		throw Error("agent observation timed out: "+commandId);
	}
	const {agentId}=await spawn_agent(config,undefined,{keepAlive:true});
	globalThis.controlFixtureStage="spawned";
	await queue_agent({agentId,instruction:"step72 queue first fixture",commandId:"first"});
	globalThis.controlFixtureStage="first-queued";
	const first=await waitFor(agentId,"first","idle");
	globalThis.controlFixtureStage="first-idle";
	if(first.lastResult?.state!=="completed"||first.lastResult?.answer!=="queue-first-ok")throw Error(JSON.stringify(first));
	await queue_agent({agentId,instruction:"step72 interrupt fixture",commandId:"hold"});
	await queue_agent({agentId,instruction:"step72 queue after hold fixture",commandId:"after-hold"});
	globalThis.controlFixtureStage="hold-queued";
	let active;
	for(let i=0;i<10000;i++){active=await observe_agent({agentId});if(active.currentRunId)break;await new Promise(resolve=>setTimeout(resolve,2));}
	if(!active?.currentRunId)throw Error("queued task did not start");
	globalThis.controlFixtureStage="hold-active";
	await globalThis.fixtureHoldReady;
	globalThis.controlFixtureStage="hold-started";
	await interrupt_agent({runId:active.currentRunId});
	await interrupt_agent({agentId});
	globalThis.controlFixtureStage="interrupt-requested";
	for(let i=0;i<10000;i++){active=await observe_agent({agentId});if(active.state==="stopped")break;await new Promise(resolve=>setTimeout(resolve,2));}
	if(active.state!=="stopped"||active.reason!=="interrupted")throw Error(JSON.stringify(active));
	globalThis.controlFixtureStage="stopped";
	return {first:first.lastResult.answer,stopped:active.state,reason:active.reason,queueLength:active.queueLength};
})().then(value=>{globalThis.controlFixtureOutcome={value};controlFixtureDone();},error=>{globalThis.controlFixtureOutcome={error:String(error&&error.message||error)};controlFixtureDone();});`
	if _, err := runPromise(ctx, rt, loop, "controlFixtureDone", "fixture-control.mjs", source); err != nil {
		stage, _ := rt.Eval("String(globalThis.controlFixtureStage || 'not-started')")
		return fmt.Errorf("queue/observe/interrupt reference flow at %s: %w", stage.String(), err)
	}
	controlBytes, err := rt.EvalContext(ctx, `JSON.stringify(globalThis.controlFixtureOutcome.value)`)
	if err != nil {
		return fmt.Errorf("read control observation result: %w", err)
	}
	if !strings.Contains(controlBytes.String(), `"first":"queue-first-ok"`) || !strings.Contains(controlBytes.String(), `"stopped":"stopped"`) || !strings.Contains(controlBytes.String(), `"reason":"interrupted"`) {
		return fmt.Errorf("unexpected control result: %s", controlBytes.String())
	}
	if !strings.Contains(controlBytes.String(), `"queueLength":0`) {
		return fmt.Errorf("interrupt left queued work behind: %s", controlBytes.String())
	}
	// The provider signals from the Go callback, then the owner loop wakes JS.
	select {
	case <-provider.holdStopped:
	case <-ctx.Done():
		return fmt.Errorf("provider callback did not stop after interrupt: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		return errors.New("provider callback did not stop after interrupt")
	}
	return nil
}

func runCloseWhileWaitingReference(parentCtx context.Context, noePath, cwd string) error {
	ctx, cancel := context.WithTimeout(parentCtx, 10*time.Second)
	defer cancel()
	provider := newReferenceProvider()
	provider.holdStarted = make(chan struct{}, 1)
	provider.holdStopped = make(chan struct{})
	sink := &borrowedSink{}
	rt, _, binding, err := installAgentRuntime(ctx, noePath, cwd, publicagent.Config{Provider: provider}, sink)
	if err != nil {
		return fmt.Errorf("install binding-close fixture: %w", err)
	}
	defer func() { binding.Close(); _ = rt.Close() }()
	source := `import {run_agent} from "noe:agent";
run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:2},"step72 close fixture").catch(error=>{globalThis.closeFixtureError=String(error&&error.message||error)});`
	if _, err := rt.EvalModuleContext(ctx, "fixture-close.mjs", source); err != nil {
		return fmt.Errorf("start close fixture task: %w", err)
	}
	select {
	case <-provider.holdStarted:
	case <-ctx.Done():
		return fmt.Errorf("close fixture provider did not start: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		return errors.New("close fixture provider did not start")
	}
	binding.Close()
	if binding.Context().Err() == nil {
		return errors.New("Binding.Close did not cancel the waiting provider context")
	}
	select {
	case <-provider.holdStopped:
	case <-ctx.Done():
		return fmt.Errorf("Binding.Close did not stop the waiting provider: %w", ctx.Err())
	case <-time.After(5 * time.Second):
		return errors.New("Binding.Close did not stop the waiting provider")
	}
	if sink.wasClosed() {
		return errors.New("Binding.Close closed the caller-owned output sink")
	}
	if _, err := sink.Write([]byte("sink-open-after-binding-close")); err != nil {
		return fmt.Errorf("caller sink is unusable after Binding.Close: %w", err)
	}
	return nil
}

func installAgentRuntime(ctx context.Context, noePath, cwd string, cfg publicagent.Config, sink *borrowedSink) (*quickjs.Runtime, *stdlib.Loop, *publicagent.Binding, error) {
	rt := quickjs.New()
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	if _, err := stdlib.Install(rt, stdlib.Config{Core: stdlib.CoreConfig{NoWebAPIs: true, Loop: loop, Stdout: sink, Stderr: sink}}); err != nil {
		_ = rt.Close()
		return nil, nil, nil, err
	}
	cfg.Loop, cfg.Cwd, cfg.Executable = loop, cwd, noePath
	binding, err := publicagent.Install(ctx, rt, cfg)
	if err != nil {
		_ = rt.Close()
		return nil, nil, nil, err
	}
	return rt, loop, binding, nil
}

func runPromise(ctx context.Context, rt *quickjs.Runtime, loop *stdlib.Loop, doneName, moduleName, source string) ([]byte, error) {
	done := make(chan struct{})
	var once sync.Once
	if err := rt.Set(doneName, func() { once.Do(func() { close(done) }) }); err != nil {
		return nil, fmt.Errorf("register completion callback: %w", err)
	}
	if _, err := rt.EvalModuleContext(ctx, moduleName, source); err != nil {
		return nil, fmt.Errorf("evaluate %s: %w", moduleName, err)
	}
	if err := loop.RunUntil(ctx, done); err != nil {
		return nil, fmt.Errorf("drive %s: %w", moduleName, err)
	}
	nameJSON, _ := json.Marshal(strings.TrimSuffix(doneName, "Done") + "Outcome")
	value, err := rt.EvalContext(ctx, "JSON.stringify(globalThis["+string(nameJSON)+"])")
	if err != nil {
		return nil, fmt.Errorf("read %s result: %w", moduleName, err)
	}
	var outcome struct {
		Error string          `json:"error"`
		Value json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal([]byte(value.String()), &outcome); err != nil {
		return nil, fmt.Errorf("decode %s result: %w", moduleName, err)
	}
	if outcome.Error != "" {
		return nil, fmt.Errorf("%s rejected: %s", moduleName, outcome.Error)
	}
	return outcome.Value, nil
}

func newReferenceProvider() *referenceProvider {
	return &referenceProvider{callsByRun: make(map[string]int)}
}

type referenceProvider struct {
	mu          sync.Mutex
	callsByRun  map[string]int
	lastFailure string
	calls       atomic.Int32
	holdStarted chan struct{}
	holdStopped chan struct{}
	holdOnce    sync.Once
}

func (p *referenceProvider) failure() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastFailure
}

func (p *referenceProvider) Generate(ctx context.Context, request publicagent.Request) (publicagent.Response, error) {
	p.calls.Add(1)
	p.mu.Lock()
	p.callsByRun[request.RunID]++
	call := p.callsByRun[request.RunID]
	p.mu.Unlock()
	text := requestCurrentTaskText(request)
	switch {
	case strings.Contains(text, "step72 aggregate fixture"):
		if call == 1 {
			return toolCallResponse(referenceAggregateCode), nil
		}
		stdout, stderr, exitCode, ok := latestToolResult(request)
		if !ok || exitCode != 0 || !strings.Contains(stdout, `"category":"expected-total"`) {
			p.mu.Lock()
			p.lastFailure = fmt.Sprintf("aggregate run_js fixture failed: found=%v exit=%d stdout=%q stderr=%q", ok, exitCode, stdout, stderr)
			p.mu.Unlock()
			return publicagent.Response{}, errors.New("aggregate run_js fixture failed")
		}
		return assistantResponse("aggregate fixture complete"), nil
	case strings.Contains(text, "step72 nested child"):
		return assistantResponse("nested-agent-ok"), nil
	case strings.Contains(text, "step72 queue first fixture"):
		return assistantResponse("queue-first-ok"), nil
	case strings.Contains(text, "step72 interrupt fixture"):
		fallthrough
	case strings.Contains(text, "step72 close fixture"):
		if p.holdStarted != nil {
			select {
			case p.holdStarted <- struct{}{}:
			default:
			}
		}
		<-ctx.Done()
		if p.holdStopped != nil {
			p.holdOnce.Do(func() { close(p.holdStopped) })
		}
		return publicagent.Response{}, ctx.Err()
	default:
		return assistantResponse("fixture complete"), nil
	}
}

func verifyJavaScriptErrorReference(ctx context.Context, rt *quickjs.Runtime, loop *stdlib.Loop) error {
	_, err := runPromise(ctx, rt, loop, "fixtureJsErrorDone", "fixture-js-error.mjs", `
		Promise.reject(new Error("fixture JS failure")).then(
			value=>{globalThis.fixtureJsErrorOutcome={value};fixtureJsErrorDone();},
			error=>{globalThis.fixtureJsErrorOutcome={error:String(error&&error.message||error)};fixtureJsErrorDone();});
	`)
	if err == nil || !strings.Contains(err.Error(), "fixture JS failure") {
		return fmt.Errorf("root Promise rejection was not propagated: %v", err)
	}
	return nil
}

func latestToolResult(request publicagent.Request) (stdout, stderr string, exitCode int, found bool) {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(request.Messages[i], &message) != nil || message.Role != "tool" {
			continue
		}
		var result struct {
			Stdout   string `json:"stdout"`
			Stderr   string `json:"stderr"`
			ExitCode int    `json:"exitCode"`
		}
		if json.Unmarshal([]byte(message.Content), &result) != nil {
			return "", "", 0, false
		}
		return result.Stdout, result.Stderr, result.ExitCode, true
	}
	return "", "", 0, false
}

func requestCurrentTaskText(request publicagent.Request) string {
	for i := len(request.Messages) - 1; i >= 0; i-- {
		var message struct {
			Role string `json:"role"`
		}
		if json.Unmarshal(request.Messages[i], &message) == nil && message.Role == "user" {
			return string(request.Messages[i])
		}
	}
	return ""
}

func assistantResponse(text string) publicagent.Response {
	message, _ := json.Marshal(map[string]any{"role": "assistant", "content": text})
	return publicagent.Response{Message: message, FinishReason: "stop"}
}

func toolCallResponse(code string) publicagent.Response {
	arguments, _ := json.Marshal(map[string]string{"code": code})
	message, _ := json.Marshal(map[string]any{
		"role": "assistant", "content": "run deterministic external fixture code",
		"tool_calls": []any{map[string]any{
			"id": "fixture-run-js", "type": "function",
			"function": map[string]string{"name": "run_js", "arguments": string(arguments)},
		}},
	})
	return publicagent.Response{Message: message, FinishReason: "tool_calls"}
}

const referenceAggregateCode = `
const fs = await import("node:fs");
const {classifyTotal} = await import("fixture-child");
const values = ["north.csv", "south.csv"].flatMap(file => {
  const lines = fs.readFileSync(file, "utf8").trim().split("\n").slice(1);
  return lines.map(line => Number(line.split(",")[1]));
});
const count = values.length;
const total = values.reduce((sum, value) => sum + value, 0);
const {decide} = await import("noe:decision");
const decision = await decide({provider:"openrouter",model:"fixture/decision",state:{count,total},questions:{route:{type:"choice",criteria:{yes:"Use the verified aggregate",no:"Reject it"}}}});
const {generate_image} = await import("noe:image");
const image = await generate_image({provider:"fixture-image",model:"fixture/image",prompt:"create a local fixture image",output:"generated.png"});
const nested = await run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:1},"step72 nested child");
const summary = {count,total,decision:decision.answers.route.choice,nested:nested.lastResult.answer,category:classifyTotal(total)};
fs.writeFileSync("summary.json",JSON.stringify(summary,null,2)+"\n");
console.log(JSON.stringify({summary,image:image.path}));
`

type referenceDecisionProvider struct{ calls *atomic.Int32 }

func (p referenceDecisionProvider) Decide(_ context.Context, request publicagent.DecisionRequest) (publicagent.DecisionResponse, error) {
	p.calls.Add(1)
	if request.Model != "fixture/decision" || len(request.Questions) != 1 {
		return publicagent.DecisionResponse{}, errors.New("unexpected decision request")
	}
	return publicagent.DecisionResponse{JSON: json.RawMessage(`{"model":"fixture/decision","answers":{"route":{"type":"choice","choice":"yes"}}}`), StatusCode: http.StatusOK}, nil
}

type referenceImageProvider struct {
	png   []byte
	calls atomic.Int32
}

func (p *referenceImageProvider) GenerateImage(_ context.Context, request publicagent.ImageRequest) (publicagent.GeneratedImage, error) {
	p.calls.Add(1)
	if request.Provider != "fixture-image" || request.Model != "fixture/image" || !filepath.IsAbs(request.Output) || filepath.Base(request.Output) != "generated.png" {
		return publicagent.GeneratedImage{}, errors.New("unexpected image request")
	}
	return publicagent.GeneratedImage{Content: append([]byte(nil), p.png...), ResponseModel: request.Model}, nil
}

func fixturePNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 20, G: 40, B: 60, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		panic(err)
	}
	return data.Bytes()
}

func runRegistryHTTPReference(ctx context.Context, noePath, parentDir string) error {
	const apiKey = "fixture-registry-key-never-print"
	var requests atomic.Int32
	var accepted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+apiKey {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var body struct {
			Model string `json:"model"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Model != "fixture/model" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests.Add(1)
		accepted.Store(true)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w,
			"data: {\"id\":\"local-1\",\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"registry-http-ok\"},\"finish_reason\":null}]}\n\n"+
				"data: {\"id\":\"local-1\",\"model\":\"fixture/model\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n"+
				"data: {\"id\":\"local-1\",\"model\":\"fixture/model\",\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n"+
				"data: [DONE]\n\n")
	}))
	defer server.Close()
	workDir, err := os.MkdirTemp(parentDir, "registry-http-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	configPath, keyPath := filepath.Join(workDir, "config.json"), filepath.Join(workDir, "provider.key")
	config := map[string]any{"providers": map[string]any{"openrouter": map[string]any{
		"models": map[string]any{"fixture/model": map[string]any{"catalog": map[string]any{"description": "local HTTP fixture"}}},
	}}}
	configBytes, _ := json.Marshal(config)
	if err := os.WriteFile(configPath, configBytes, 0600); err != nil {
		return err
	}
	if err := os.WriteFile(keyPath, []byte(apiKey), 0600); err != nil {
		return err
	}
	sink := &borrowedSink{}
	providerClient := &http.Client{Timeout: 5 * time.Second}
	options := publicagent.Config{Providers: publicagent.ProviderOptions{
		ConfigFile: configPath, KeyFile: keyPath, BaseURL: server.URL + "/api/v1", HTTPClient: providerClient,
		UserAgent: "noe-agent-external-consumer-fixture",
	}}
	rt, loop, binding, err := installAgentRuntime(ctx, noePath, workDir, options, sink)
	if err != nil {
		return fmt.Errorf("install local HTTP registry: %w", err)
	}
	defer func() { binding.Close(); _ = rt.Close() }()
	source := `import {run_agent} from "noe:agent";
run_agent({provider:"openrouter",model:"fixture/model",effort:"auto",maxSteps:1},"step72 local registry HTTP fixture")
 .then(value=>{globalThis.httpRegistryOutcome={value};httpRegistryDone();},error=>{globalThis.httpRegistryOutcome={error:String(error&&error.message||error)};httpRegistryDone();});`
	value, err := runPromise(ctx, rt, loop, "httpRegistryDone", "fixture-registry.mjs", source)
	if err != nil {
		return fmt.Errorf("local HTTP registry run_agent: %w", err)
	}
	if len(value) == 0 || !strings.Contains(string(value), "registry-http-ok") || requests.Load() != 1 || !accepted.Load() {
		return fmt.Errorf("explicit ProviderOptions did not reach the local HTTP fixture: answer=%s requests=%d accepted=%v", value, requests.Load(), accepted.Load())
	}
	return nil
}

func runAuthReference(ctx context.Context, noePath, parentDir string) error {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return fmt.Errorf("generate local auth fixture key: %w", err)
	}
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/discovery":
			_, _ = fmt.Fprintf(w, `{"issuer":"https://auth.openai.com","token_endpoint":%q,"jwks_uri":%q}`, endpoint+"/token", endpoint+"/jwks")
		case "/jwks":
			n := base64.RawURLEncoding.EncodeToString(key.N.Bytes())
			e := base64.RawURLEncoding.EncodeToString([]byte{byte(key.E >> 16), byte(key.E >> 8), byte(key.E)})
			_, _ = fmt.Fprintf(w, `{"keys":[{"kid":"consumer-fixture","kty":"RSA","n":%q,"e":%q}]}`, n, e)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	endpoint = server.URL
	workDir, err := os.MkdirTemp(parentDir, "auth-reference-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workDir)
	token, err := signedFixtureToken(key, "consumer-fixture-client", "consumer-fixture-nonce")
	if err != nil {
		return err
	}
	secretAccess, secretRefresh := "fixture-access-token-never-print", "fixture-refresh-token-never-print"
	request := map[string]any{
		"file": filepath.Join(workDir, "credentials.json"), "clientId": "consumer-fixture-client", "hostId": "consumer-fixture-host", "nonce": "consumer-fixture-nonce",
		"tokens": map[string]any{"id_token": token, "access_token": secretAccess, "refresh_token": secretRefresh,
			"token_type": "Bearer", "expires_in": 3600, "scope": "chatgpt.tokens.use.direct"},
	}
	requestJSON, _ := json.Marshal(request)
	config := publicagent.Config{Auth: &publicagent.AuthOptions{HTTPClient: server.Client(), DiscoveryURL: endpoint + "/discovery"}}
	sink := &borrowedSink{}
	rt, loop, binding, err := installAgentRuntime(ctx, noePath, workDir, config, sink)
	if err != nil {
		return fmt.Errorf("install Auth opt-in: %w", err)
	}
	defer func() { binding.Close(); _ = rt.Close() }()
	source := `import {save_chatgpt_credentials} from "noe:auth";
save_chatgpt_credentials(JSON.parse(` + jsString(string(requestJSON)) + `))
	 .then(value=>{globalThis.authFixtureOutcome={value};authFixtureDone();},error=>{globalThis.authFixtureOutcome={error:String(error&&error.message||error)};authFixtureDone();});`
	value, err := runPromise(ctx, rt, loop, "authFixtureDone", "fixture-auth.mjs", source)
	if err != nil {
		return fmt.Errorf("local Auth helper: %w", err)
	}
	var account map[string]any
	if err := json.Unmarshal(value, &account); err != nil {
		return fmt.Errorf("decode Auth result: %w", err)
	}
	accountJSON, _ := json.Marshal(account)
	if strings.Contains(string(accountJSON), token) || strings.Contains(string(accountJSON), secretAccess) || strings.Contains(string(accountJSON), secretRefresh) {
		return errors.New("Auth helper returned credential material")
	}
	data, err := os.ReadFile(request["file"].(string))
	if err != nil {
		return fmt.Errorf("Auth helper did not save its fake credential file: %w", err)
	}
	if !strings.Contains(string(data), secretAccess) || !strings.Contains(string(data), secretRefresh) {
		return errors.New("saved fake credential record is incomplete")
	}
	info, err := os.Stat(request["file"].(string))
	if err != nil || info.Mode().Perm() != 0600 {
		return errors.New("saved credential fixture did not use private file permissions")
	}
	return nil
}

func signedFixtureToken(key *rsa.PrivateKey, clientID, nonce string) (string, error) {
	encode := func(value any) (string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", err
		}
		return base64.RawURLEncoding.EncodeToString(data), nil
	}
	header, err := encode(map[string]string{"alg": "RS256", "kid": "consumer-fixture"})
	if err != nil {
		return "", err
	}
	claims, err := encode(map[string]any{
		"iss": "https://auth.openai.com", "sub": "fixture-subject", "email": "consumer@example.test",
		"aud": clientID, "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce,
	})
	if err != nil {
		return "", err
	}
	unsigned := header + "." + claims
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func jsString(value string) string {
	data, _ := json.Marshal(value)
	return string(data)
}
