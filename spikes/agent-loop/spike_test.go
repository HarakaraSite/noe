package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

var testNOE string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "noe-spike-tests-")
	if err != nil {
		panic(err)
	}
	testNOE = filepath.Join(dir, "noe")
	cmd := exec.Command("go", "build", "-o", testNOE, "./cmd/noe")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, string(out), err)
		os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func testHost(t *testing.T, p provider, trace io.Writer) *host {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	h, err := startHost(ctx, config{provider: "mock", model: "fixture", noe: testNOE, workdir: t.TempDir(), maxSteps: 6}, p, trace)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.close)
	return h
}

func executeScript(t *testing.T, h *host, code string) jsResult {
	t.Helper()
	path := filepath.Join(h.dir, "examples", "test.js")
	if err := os.WriteFile(path, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	return h.execute(h.ctx, "root", path, nil)
}

func traceEvents(t *testing.T, raw []byte) []map[string]any {
	t.Helper()
	var events []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	for {
		var e map[string]any
		err := decoder.Decode(&e)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	return events
}

func TestDemosThroughExistingNOE(t *testing.T) {
	for _, name := range []string{"single", "recovery", "delegate", "service"} {
		t.Run(name, func(t *testing.T) {
			var trace bytes.Buffer
			h := testHost(t, mockProvider{}, &trace)
			got := h.execute(h.ctx, "root", filepath.Join(h.dir, "examples", name+".js"), nil)
			if got.ExitCode != 0 || got.Stderr != "" {
				t.Fatalf("%+v", got)
			}
			var output map[string]any
			if err := json.Unmarshal([]byte(got.Stdout), &output); err != nil {
				t.Fatal(err, got.Stdout)
			}
			if name != "service" && output["total"] != float64(100) {
				t.Fatal(output)
			}
			if name == "service" && !strings.Contains(output["source"].(string), "fixture") {
				t.Fatal(output)
			}
			// Execute has completed all JS calls before we inspect the trace.
			events := traceEvents(t, trace.Bytes())
			failed, children := 0, 0
			for _, e := range events {
				if e["kind"] == "js_end" && e["exitCode"] != float64(0) {
					failed++
				}
				if e["kind"] == "run_start" && e["parentRunId"] != "" {
					children++
				}
			}
			if name == "recovery" && failed != 1 {
				t.Fatalf("failed tool calls = %d", failed)
			}
			if name == "delegate" && children != 2 {
				t.Fatalf("child agents = %d", children)
			}
		})
	}
}

type providerFunc func(context.Context, modelRequest) (modelResponse, error)

func (f providerFunc) generate(ctx context.Context, q modelRequest) (modelResponse, error) {
	return f(ctx, q)
}

func TestChildrenMakeConcurrentHostCalls(t *testing.T) {
	var mu sync.Mutex
	arrivals := 0
	ready := make(chan struct{})
	p := providerFunc(func(ctx context.Context, q modelRequest) (modelResponse, error) {
		if (q.Name == "left" || q.Name == "right") && len(q.Messages) == 2 {
			mu.Lock()
			arrivals++
			if arrivals == 2 {
				close(ready)
			}
			mu.Unlock()
			select {
			case <-ready:
			case <-ctx.Done():
				return modelResponse{}, ctx.Err()
			}
		}
		return (mockProvider{}).generate(ctx, q)
	})
	h := testHost(t, p, io.Discard)
	result := h.execute(h.ctx, "root", filepath.Join(h.dir, "examples", "delegate.js"), nil)
	if result.ExitCode != 0 {
		t.Fatalf("nested host calls stalled: %+v", result)
	}
	if arrivals != 2 {
		t.Fatalf("arrivals=%d", arrivals)
	}
}

func TestRunHistoryAndProviderFailures(t *testing.T) {
	t.Run("history is local to each invocation", func(t *testing.T) {
		p := providerFunc(func(ctx context.Context, q modelRequest) (modelResponse, error) {
			if len(q.Messages) != 2 {
				return modelResponse{}, fmt.Errorf("history leaked between invocations: %d", len(q.Messages))
			}
			return modelResponse{Message: map[string]any{"role": "assistant", "content": "fresh"}, FinishReason: "stop"}, nil
		})
		h := testHost(t, p, io.Discard)
		got := executeScript(t, h, `import {createAgent} from "../lib/agent.js"; const a=createAgent();console.log(await a.run("one"),await a.run("two"));`)
		if got.ExitCode != 0 || got.Stdout != "fresh fresh\n" {
			t.Fatalf("%+v", got)
		}
	})
	for _, scenario := range []string{"authentication", "truncation", "max-steps"} {
		t.Run(scenario, func(t *testing.T) {
			var p provider = mockProvider{}
			want := "maxSteps"
			if scenario == "authentication" {
				want = "login required"
				p = providerFunc(func(context.Context, modelRequest) (modelResponse, error) {
					return modelResponse{}, &providerError{Code: "authentication", Message: want}
				})
			}
			if scenario == "truncation" {
				want = "length"
				p = providerFunc(func(context.Context, modelRequest) (modelResponse, error) {
					return modelResponse{Message: map[string]any{"role": "assistant", "content": "unfinished"}, FinishReason: "length"}, nil
				})
			}
			h := testHost(t, p, io.Discard)
			got := executeScript(t, h, `import {createAgent} from "../lib/agent.js"; await createAgent({name:"loop"}).run("work",{maxSteps:1});`)
			if got.ExitCode == 0 || !strings.Contains(got.Stderr, want) {
				t.Fatalf("%+v", got)
			}
		})
	}
}

func TestFreshProcessesAndCredentialBoundary(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "provider-secret-fixture")
	h := testHost(t, mockProvider{}, io.Discard)
	got := executeScript(t, h, `import {rpc} from "../lib/host.js";
const {runId}=await rpc("/run/start",{name:"boundary"});
const first=await rpc("/js",{runId,code:'globalThis.marker=123; console.log(process.env.OPENROUTER_API_KEY===undefined);'});
const second=await rpc("/js",{runId,code:'console.log(typeof globalThis.marker);'});
console.log(first.stdout.trim(),second.stdout.trim());
await rpc("/run/finish",{runId,status:"completed"});`)
	if got.ExitCode != 0 || got.Stdout != "true undefined\n" {
		t.Fatalf("%+v", got)
	}
	// A child bridge capability is not accepted as the service's credential.
	req, _ := http.NewRequest(http.MethodPost, h.url+"/fixture/search", strings.NewReader(`{"query":"test"}`))
	req.Header.Set("Authorization", "Bearer "+h.token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
}

func TestWholeInvocationTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "busy.js")
	if err := os.WriteFile(path, []byte(`console.log("started");while(true){}`), 0600); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	code := mainCLI([]string{"--noe", testNOE, "--timeout", "500ms", "run", path}, &out, &stderr)
	if code != 124 || out.String() != "started\n" {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), stderr.String())
	}
}

func TestCancellationStopsNestedProcessesAndModelCalls(t *testing.T) {
	started := make(chan struct{}, 2)
	modelCancelled := make(chan struct{}, 1)
	p := providerFunc(func(ctx context.Context, q modelRequest) (modelResponse, error) {
		if q.Name == "left" {
			started <- struct{}{}
			<-ctx.Done()
			modelCancelled <- struct{}{}
			return modelResponse{}, ctx.Err()
		}
		return (mockProvider{}).generate(ctx, q)
	})
	h := testHost(t, p, io.Discard)
	pidFile := filepath.Join(h.cfg.workdir, "pid")
	script := `import {rpc} from "../lib/host.js";
const {runId}=await rpc("/run/start",{name:"owner"});
await rpc("/js",{runId,code:
 'const {writeFile}=await import("fs/promises"); const {readFile}=await import("fs/promises");'+
 'const stat=await readFile("/proc/self/stat","utf8"); await writeFile("pid",stat.split(" ")[0]);'+
 'await createAgent({name:"left"}).run("wait");'});`
	done := make(chan jsResult, 1)
	path := filepath.Join(h.dir, "examples", "cancel.js")
	if err := os.WriteFile(path, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	go func() { done <- h.execute(h.ctx, "root", path, nil) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("child model did not start")
	}
	h.cancel()
	select {
	case result := <-done:
		if result.Reason != "cancelled" {
			t.Fatal(result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("root process still running")
	}
	select {
	case <-modelCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("child model still running")
	}
	data, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	var pid int
	fmt.Sscan(string(data), &pid)
	deadline := time.Now().Add(3 * time.Second)
	for {
		err := syscall.Kill(pid, 0)
		if errors.Is(err, syscall.ESRCH) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("child NOE %d still alive", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestSDKStreamsAndPreservesProviderFields(t *testing.T) {
	var received map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test-key" {
			t.Error("wrong SDK route or auth")
		}
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"role":"assistant","reasoning_details":[{"index":0,"type":"reasoning.text","text":"part1","signature":"sig"}],"tool_calls":[{"index":0,"id":"call-a","type":"function","function":{"name":"run_js","arguments":"{\"code\":"}}]}}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"index":0,"delta":{"reasoning_details":[{"index":0,"text":"part2"}],"tool_calls":[{"index":0,"function":{"arguments":"\"console.log(1)\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":80}}}`+"\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	p := newOpenRouter("test-key", "test-model", 200)
	p.client = openai.NewClient(option.WithAPIKey("test-key"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	message := json.RawMessage(`{"role":"assistant","content":null,"reasoning_details":[{"type":"reasoning.text","text":"retain me","signature":"original"}]}`)
	result, err := p.generate(context.Background(), modelRequest{Messages: []json.RawMessage{message}, Tools: []json.RawMessage{}, CacheKey: "repeatable-key"})
	if err != nil {
		t.Fatal(err)
	}
	if received["session_id"] != "repeatable-key" || received["model"] != "test-model" || received["stream"] != true {
		t.Fatal(received)
	}
	messages := received["messages"].([]any)
	if messages[0].(map[string]any)["reasoning_details"] == nil {
		t.Fatal("reasoning was dropped from request")
	}
	details := result.Message["reasoning_details"].([]any)[0].(map[string]any)
	if details["text"] != "part1part2" || details["signature"] != "sig" {
		t.Fatal(details)
	}
	call := result.Message["tool_calls"].([]any)[0].(map[string]any)
	if call["function"].(map[string]any)["arguments"] != `{"code":"console.log(1)"}` {
		t.Fatal(call)
	}
	if result.FinishReason != "tool_calls" || !strings.Contains(string(result.Usage), "cached_tokens") {
		t.Fatal(result)
	}
}

func TestSDKRejectsBrokenStreamAndRedactsAuthError(t *testing.T) {
	for _, scenario := range []string{"broken", "authentication"} {
		t.Run(scenario, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if scenario == "authentication" {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(401)
					fmt.Fprint(w, `{"error":{"message":"bad test-secret-key"}}`)
					return
				}
				w.Header().Set("Content-Type", "text/event-stream")
				fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
			}))
			defer server.Close()
			p := newOpenRouter("test-secret-key", "model", 0)
			p.client = openai.NewClient(option.WithAPIKey(p.key), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
			_, err := p.generate(context.Background(), modelRequest{})
			if err == nil || strings.Contains(err.Error(), p.key) {
				t.Fatalf("error=%v", err)
			}
			var pe *providerError
			if !errors.As(err, &pe) {
				t.Fatalf("unclassified: %v", err)
			}
			want := "incomplete_stream"
			if scenario == "authentication" {
				want = "authentication"
			}
			if pe.Code != want {
				t.Fatal(pe)
			}
		})
	}
}
