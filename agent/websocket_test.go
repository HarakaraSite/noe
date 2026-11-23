package agent_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	"github.com/coder/websocket"
)

type managedWSRequest struct {
	Connection int
	Previous   string `json:"previous_response_id"`
	Input      []struct {
		Type   string `json:"type"`
		Output string `json:"output"`
	} `json:"input"`
	Size int
}

type managedWSFixture struct {
	mu          sync.Mutex
	connections int
	posts       int
	requests    []managedWSRequest
	sessions    []string
	closed      chan struct{}
	server      *httptest.Server
}

func managedWSResponse(id string, output any) []byte {
	data, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output, "usage": map[string]int{"input_tokens": 12, "output_tokens": 3, "total_tokens": 15}}})
	return data
}

func newManagedWSFixture(t *testing.T, custom, expireAfterTool bool) *managedWSFixture {
	t.Helper()
	f := &managedWSFixture{closed: make(chan struct{}, 8)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wire/responses" || r.Header.Get("x-wire-fixture") != "managed" {
			t.Error("configured provider path/header was lost")
			http.Error(w, "configuration", 500)
			return
		}
		if r.Method == http.MethodPost {
			f.mu.Lock()
			f.posts++
			f.mu.Unlock()
			w.Header().Set("Content-Type", "text/event-stream")
			output := []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "standalone"}}}}
			fmt.Fprintf(w, "data: %s\n\n", managedWSResponse("resp_sse", output))
			return
		}
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { connection.CloseNow(); f.closed <- struct{}{} }()
		f.mu.Lock()
		f.connections++
		id := f.connections
		f.sessions = append(f.sessions, r.Header.Get("x-wire-session"))
		f.mu.Unlock()
		for step := 1; ; step++ {
			_, data, err := connection.Read(r.Context())
			if err != nil {
				return
			}
			var request managedWSRequest
			if err := json.Unmarshal(data, &request); err != nil {
				t.Error(err)
				return
			}
			request.Connection, request.Size = id, len(data)
			f.mu.Lock()
			f.requests = append(f.requests, request)
			f.mu.Unlock()
			var output []any
			replaying := expireAfterTool && id == 2 && step == 1
			if step == 1 && !replaying {
				if request.Previous != "" {
					t.Error("new run reused a previous response ID")
				}
				call := map[string]any{"type": "function_call", "id": fmt.Sprintf("fc_%d", id), "call_id": fmt.Sprintf("call_%d", id), "name": "run_js", "arguments": `{"code":"console.log(6*7)"}`, "status": "completed"}
				if custom {
					delete(call, "arguments")
					call["type"] = "custom_tool_call"
					call["input"] = "console.log(6*7)"
				}
				output = []any{map[string]any{"type": "reasoning", "id": fmt.Sprintf("rs_%d", id), "summary": []any{}, "encrypted_content": "fixture-opaque", "future": json.RawMessage(`{"large":9007199254740991}`)}, call}
			} else {
				if !replaying && (step != 2 || request.Previous != fmt.Sprintf("resp_%d_1", id) || len(request.Input) != 2) {
					t.Errorf("expected only tool output plus the new context notification: %+v", request)
				}
				found := false
				for _, input := range request.Input {
					if input.Type == "function_call_output" || input.Type == "custom_tool_call_output" {
						found = true
						var result struct {
							Stdout string `json:"stdout"`
						}
						if json.Unmarshal([]byte(input.Output), &result) != nil || result.Stdout != "42\n" {
							t.Error("tool execution result was lost")
						}
					}
				}
				if !found {
					t.Error("tool continuation missing")
				}
				if expireAfterTool && id == 1 {
					event := []byte(`{"type":"error","status":400,"error":{"type":"invalid_request_error","code":"websocket_connection_limit_reached"}}`)
					if err := connection.Write(r.Context(), websocket.MessageText, event); err != nil {
						t.Errorf("write connection limit error: %v", err)
					}
					return
				}
				output = []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "managed complete"}}}}
			}
			if err := connection.Write(r.Context(), websocket.MessageText, managedWSResponse(fmt.Sprintf("resp_%d_%d", id, step), output)); err != nil {
				return
			}
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestManagedWebSocketRunOwnershipAndStandaloneSSE(t *testing.T) {
	for _, providerName := range []string{"openai-api", "openai-chatgpt"} {
		for _, entry := range []string{"gohost", "javascript"} {
			t.Run(providerName+"/"+entry, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
				defer cancel()
				fixture := newManagedWSFixture(t, providerName == "openai-chatgpt", false)
				config := filepath.Join(t.TempDir(), "providers.json")
				data, _ := json.Marshal(map[string]any{"providers": map[string]any{providerName: map[string]any{"auth": "none", "baseURL": fixture.server.URL, "path": "/wire/responses", "headers": map[string]string{"x-wire-fixture": "managed"}, "sessionHeader": "x-wire-session"}}})
				if err := os.WriteFile(config, data, 0o600); err != nil {
					t.Fatal(err)
				}
				service, err := model.NewService(model.Options{ConfigFile: config, ConfigDir: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				standalone := func() {
					t.Helper()
					response, err := service.Generate(ctx, model.Request{Provider: providerName, Model: "gpt-6.1-sol", SessionID: "standalone", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"single call"}`)}})
					if err != nil || !strings.Contains(string(response.Message), "standalone") {
						t.Fatalf("standalone: %s %v", response.Message, err)
					}
				}
				standalone()
				profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}}
				host, err := agent.NewHost(ctx, agent.HostConfig{Models: service, Runtime: &profile})
				if err != nil {
					t.Fatal(err)
				}
				defer host.Close()
				if entry == "gohost" {
					client, err := host.NewClient(ctx)
					if err != nil {
						t.Fatal(err)
					}
					defer client.Close()
					config := agent.TaskConfig{Config: agent.AgentConfig{Provider: providerName, Model: "gpt-6.1-sol", Name: "websocket", Effort: "low"}, MaxSteps: 3}
					spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: config, Instruction: textPointer("first"), KeepAlive: true})
					if err != nil {
						t.Fatal(err)
					}
					first, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
					if err != nil || first.Answer != "managed complete" {
						t.Fatalf("first: %+v %v", first, err)
					}
					queued, err := client.Queue(ctx, agent.QueueRequest{AgentID: spawned.AgentID, Instruction: "second"})
					if err != nil {
						t.Fatal(err)
					}
					second, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: queued.CommandID})
					if err != nil || second.Answer != "managed complete" {
						t.Fatalf("second: %+v %v", second, err)
					}
				} else {
					_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[managed websocket]", Module: true, Virtual: true, Code: fmt.Sprintf(`import {run_agent} from "noe:agent";for(let i=0;i<2;i++){const result=await run_agent({provider:%q,model:"gpt-6.1-sol",effort:"low",maxSteps:3},"wire task");if(result.lastResult?.answer!=="managed complete")throw Error(JSON.stringify(result));}`, providerName)}, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
						binding, err := agent.Attach(ctx, b, host)
						if err != nil {
							return nil, err
						}
						return binding.Close, nil
					}})
					if err != nil {
						t.Fatal(stdlib.Diagnostic(err))
					}
				}
				host.Close()
				standalone()
				for range 2 {
					select {
					case <-fixture.closed:
					case <-ctx.Done():
						t.Fatal("completed run did not close its socket")
					}
				}
				fixture.mu.Lock()
				defer fixture.mu.Unlock()
				if fixture.connections != 2 || fixture.posts != 2 || len(fixture.requests) != 4 {
					t.Fatalf("connections=%d posts=%d requests=%d", fixture.connections, fixture.posts, len(fixture.requests))
				}
				for i := 0; i < 4; i += 2 {
					if fixture.requests[i+1].Size >= fixture.requests[i].Size/2 {
						t.Error("continuation uploaded the original history")
					}
				}
				if entry == "gohost" && (fixture.sessions[0] == "" || fixture.sessions[0] != fixture.sessions[1]) {
					t.Error("queued run lost managed session identity")
				}
			})
		}
	}
}

func TestManagedWebSocketRecoversFromConnectionLimitAfterTool(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	fixture := newManagedWSFixture(t, true, true)
	config := filepath.Join(t.TempDir(), "providers.json")
	data := fmt.Appendf(nil, `{"providers":{"openai-chatgpt":{"auth":"none","baseURL":%q,"path":"/wire/responses","headers":{"x-wire-fixture":"managed"}}}}`, fixture.server.URL)
	if err := os.WriteFile(config, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := model.NewService(model.Options{ConfigFile: config, ConfigDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	var toolRuns atomic.Int32
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}}
	host, err := agent.NewHost(ctx, agent.HostConfig{
		Models: service, Runtime: &profile,
		Observe: func(event agent.Event) {
			if event["type"] == "run_js.started" {
				toolRuns.Add(1)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	task := agent.TaskConfig{
		Config:   agent.AgentConfig{Provider: "openai-chatgpt", Model: "gpt-6.2-sol", Name: "websocket rollover", Effort: "xhigh"},
		MaxSteps: 2,
	}
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: task, Instruction: textPointer("compute and finish")})
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil || outcome.State != "completed" || outcome.Answer != "managed complete" {
		t.Fatalf("recovered outcome: %+v %v", outcome, err)
	}
	if count := toolRuns.Load(); count != 1 {
		t.Errorf("tool ran %d times, want 1", count)
	}

	fixture.mu.Lock()
	defer fixture.mu.Unlock()
	if fixture.connections != 2 || fixture.posts != 0 || len(fixture.requests) != 3 {
		t.Fatalf("connections=%d posts=%d requests=%d", fixture.connections, fixture.posts, len(fixture.requests))
	}
	initial, delta, replay := fixture.requests[0], fixture.requests[1], fixture.requests[2]
	if delta.Connection != initial.Connection || replay.Connection == delta.Connection || replay.Previous != "" {
		t.Errorf("expected a new connection without the old response ID: delta=%+v replay=%+v", delta, replay)
	}
	if len(delta.Input) != 2 || len(replay.Input) != len(initial.Input)+2+len(delta.Input) ||
		replay.Input[len(initial.Input)].Type != "reasoning" ||
		replay.Input[len(initial.Input)+1].Type != "custom_tool_call" ||
		replay.Input[len(initial.Input)+2] != delta.Input[0] {
		t.Errorf("expected the current history with the completed tool result: delta=%+v replay=%+v", delta.Input, replay.Input)
	}
}

func TestManagedWebSocketCancellationClosesActiveConnection(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	started, closed := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { connection.CloseNow(); close(closed) }()
		if _, _, err := connection.Read(r.Context()); err != nil {
			return
		}
		if err := connection.Write(r.Context(), websocket.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_running","status":"in_progress","output":[]}}`)); err != nil {
			return
		}
		close(started)
		connection.Read(r.Context())
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "config.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{"openai-api": map[string]any{"auth": "none", "baseURL": server.URL}}})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	service, err := model.NewService(model.Options{ConfigFile: path})
	if err != nil {
		t.Fatal(err)
	}
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: service, Runtime: &profile})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: agent.TaskConfig{Config: agent.AgentConfig{Provider: "openai-api", Model: "gpt-6.1-sol", Name: "cancel websocket", Effort: "low"}}, Instruction: textPointer("wait for cancellation")})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("model generation did not start")
	}
	if _, err := client.CancelTask(ctx, agent.CancelTaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("cancelled model socket remained open")
	}
	outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
	if err != nil || outcome.State != "cancelled" {
		t.Fatalf("cancelled outcome: %+v %v", outcome, err)
	}
}
