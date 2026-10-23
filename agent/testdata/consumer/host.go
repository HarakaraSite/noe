package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	publicagent "forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type hostProvider struct {
	mu         sync.Mutex
	identities map[string][2]string
}

func (p *hostProvider) Generate(ctx context.Context, q model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	instruction := ""
	hasTool := false
	for _, raw := range q.Messages {
		var m struct {
			Role    string
			Content json.RawMessage
		}
		if json.Unmarshal(raw, &m) != nil {
			continue
		}
		var text string
		_ = json.Unmarshal(m.Content, &text)
		if m.Role == "user" && !strings.HasPrefix(text, "NOE_CONTEXT_STATUS\n") {
			instruction = text
		}
		if m.Role == "tool" && strings.Contains(text, "child answer") {
			hasTool = true
		}
	}
	p.mu.Lock()
	p.identities[instruction] = [2]string{q.SessionID, q.RunID}
	p.mu.Unlock()
	if instruction == "root" && !hasTool {
		code := `import {run_agent} from "noe:agent";
import {readFileSync} from "node:fs";
if(readFileSync("input.txt","utf8")!=="selected root")throw Error("tool filesystem");
const child=await run_agent({name:"child"},"child");if(child.lastResult?.state!=="completed")throw Error("child");console.log(child.lastResult.answer);`
		args, _ := json.Marshal(map[string]string{"code": code})
		message, _ := json.Marshal(map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "files-and-child", "type": "function", "function": map[string]string{"name": "run_js", "arguments": string(args)}}}})
		return model.Response{Message: message, FinishReason: "tool_calls"}, nil
	}
	answer := instruction + " answer"
	message, _ := json.Marshal(map[string]string{"role": "assistant", "content": answer})
	return model.Response{Message: message, FinishReason: "stop"}, nil
}
func runGoHost() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root, err := os.MkdirTemp("", "noe-public-agent-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(root)
	if err = os.WriteFile(filepath.Join(root, "input.txt"), []byte("selected root"), 0600); err != nil {
		return err
	}
	provider := &hostProvider{identities: make(map[string][2]string)}
	models, err := model.NewService(model.Options{Provider: provider})
	if err != nil {
		return err
	}
	profile := stdlib.Profile{Modules: stdlib.Modules{FS: &stdlib.FileSystemOptions{Root: root, ReadOnly: true}}, NewLoader: stdlib.FileLoader(nil)}
	host, err := publicagent.NewHost(ctx, publicagent.HostConfig{Models: models, Runtime: &profile})
	if err != nil {
		return err
	}
	defer host.Close()
	client, err := host.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	instruction := "root"
	accepted, err := client.Spawn(ctx, publicagent.SpawnRequest{Config: publicagent.TaskConfig{Config: publicagent.AgentConfig{Provider: "custom", Model: "fixture", Name: "external", Effort: "none"}, MaxSteps: 3}, Instruction: &instruction, KeepAlive: true})
	if err != nil {
		return err
	}
	result, err := client.WaitTask(ctx, publicagent.TaskRequest{AgentID: accepted.AgentID, CommandID: accepted.CommandID})
	if err != nil || result.State != "completed" || result.Answer != "root answer" {
		return fmt.Errorf("Go Host root=%+v err=%v", result, err)
	}
	queued, err := client.Queue(ctx, publicagent.QueueRequest{AgentID: accepted.AgentID, Instruction: "queued"})
	if err != nil {
		return err
	}
	result, err = client.WaitTask(ctx, publicagent.TaskRequest{AgentID: accepted.AgentID, CommandID: queued.CommandID})
	if err != nil || result.State != "completed" || result.Answer != "queued answer" {
		return fmt.Errorf("Go Host queue=%+v err=%v", result, err)
	}
	provider.mu.Lock()
	first, next, child := provider.identities["root"], provider.identities["queued"], provider.identities["child"]
	provider.mu.Unlock()
	if first[0] == "" || first[0] != next[0] || first[1] == next[1] || first[0] == child[0] {
		return errors.New("session/run ownership changed")
	}
	client.Close()
	another, err := host.NewClient(ctx)
	if err != nil {
		return err
	}
	defer another.Close()
	if _, err := another.Models().ListModels(ctx, model.ModelQuery{}); err != nil {
		return err
	}
	if profile.Environment.Stdout != nil || profile.Environment.Stdin != nil {
		return errors.New("Host changed caller profile")
	}
	return nil
}
