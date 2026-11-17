package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	"github.com/coder/websocket"
)

func websocketConsumer(mode string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var commands, posts atomic.Int32
	closed := make(chan struct{}, 1)
	failures := make(chan error, 4)
	terminal := func(id string, output any) []byte {
		data, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "output": output}})
		return data
	}
	answer := []any{map[string]any{"type": "message", "role": "assistant", "content": []any{map[string]string{"type": "output_text", "text": "websocket-consumer"}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "data: %s\n\n", terminal("resp_standalone", answer))
			return
		}
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			failures <- err
			return
		}
		defer func() { connection.CloseNow(); closed <- struct{}{} }()
		for step := 1; ; step++ {
			_, data, err := connection.Read(r.Context())
			if err != nil {
				return
			}
			commands.Add(1)
			var request struct {
				Previous string            `json:"previous_response_id"`
				Input    []json.RawMessage `json:"input"`
			}
			if err := json.Unmarshal(data, &request); err != nil {
				failures <- err
				return
			}
			output := answer
			if step == 1 {
				if request.Previous != "" {
					failures <- fmt.Errorf("new conversation reused a response")
				}
				output = []any{map[string]any{"type": "function_call", "id": "fc_consumer", "call_id": "call_consumer", "name": "run_js", "arguments": `{"code":"console.log(42)"}`, "status": "completed"}}
			} else if step != 2 || request.Previous != "resp_tool" || len(request.Input) != 2 {
				failures <- fmt.Errorf("continuation did not send only new items: previous=%q count=%d", request.Previous, len(request.Input))
				return
			}
			id := "resp_final"
			if step == 1 {
				id = "resp_tool"
			}
			if err := connection.Write(r.Context(), websocket.MessageText, terminal(id, output)); err != nil {
				failures <- err
				return
			}
		}
	}))
	defer server.Close()
	directory, err := os.MkdirTemp("", "noe-websocket-consumer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(directory)
	configuration := filepath.Join(directory, "providers.json")
	data, _ := json.Marshal(map[string]any{"providers": map[string]any{"openai-api": map[string]any{"auth": "none", "baseURL": server.URL}}})
	if err := os.WriteFile(configuration, data, 0o600); err != nil {
		return err
	}
	service, err := model.NewService(model.Options{ConfigFile: configuration})
	if err != nil {
		return err
	}
	if _, err := service.Generate(ctx, model.Request{Provider: "openai-api", Model: "gpt-6.1-sol", Messages: []json.RawMessage{json.RawMessage(`{"role":"user","content":"standalone"}`)}}); err != nil {
		return err
	}
	profile := stdlib.Profile{Modules: stdlib.Modules{Stdio: true}}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: service, Runtime: &profile})
	if err != nil {
		return err
	}
	defer host.Close()
	if mode == "gohost" {
		client, err := host.NewClient(ctx)
		if err != nil {
			return err
		}
		defer client.Close()
		instruction := "test transport"
		spawned, err := client.Spawn(ctx, agent.SpawnRequest{Config: agent.TaskConfig{Config: agent.AgentConfig{Provider: "openai-api", Model: "gpt-6.1-sol", Name: "websocket", Effort: "low"}, MaxSteps: 3}, Instruction: &instruction})
		if err != nil {
			return err
		}
		outcome, err := client.WaitTask(ctx, agent.TaskRequest{AgentID: spawned.AgentID, CommandID: spawned.CommandID})
		if err != nil {
			return err
		}
		if outcome.Answer != "websocket-consumer" {
			return fmt.Errorf("WebSocket outcome: %+v", outcome)
		}
	} else {
		_, err := stdlib.Execute(ctx, stdlib.Source{Name: "[WebSocket consumer]", Module: true, Virtual: true, Code: `import {run_agent} from "noe:agent";const result=await run_agent({provider:"openai-api",model:"gpt-6.1-sol",effort:"low",maxSteps:3},"test transport");if(result.lastResult?.answer!=="websocket-consumer")throw Error(JSON.stringify(result));`}, stdlib.Profile{Initialize: func(ctx context.Context, b *stdlib.Bindings) (func(), error) {
			binding, err := agent.Attach(ctx, b, host)
			if err != nil {
				return nil, err
			}
			return binding.Close, nil
		}})
		if err != nil {
			return err
		}
	}
	select {
	case <-closed:
	case <-ctx.Done():
		return fmt.Errorf("completed consumer run retained its socket")
	}
	select {
	case err := <-failures:
		return err
	default:
	}
	if commands.Load() != 2 || posts.Load() != 1 {
		return fmt.Errorf("consumer commands=%d standalone posts=%d", commands.Load(), posts.Load())
	}
	return nil
}
