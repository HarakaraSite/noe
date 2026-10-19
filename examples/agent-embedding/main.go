// This host uses a deterministic provider to demonstrate a real run_js child.
// It needs a separately built NOE executable, but no credential or remote API.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
	quickjs "github.com/go-quickjs/go-quickjs"
)

func main() {
	executable := flag.String("noe", "", "path to a separate NOE executable")
	flag.Parse()
	if err := run(*executable); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(executable string) error {
	if executable == "" {
		return errors.New("pass -noe /absolute/path/to/noe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rt := quickjs.New()
	defer rt.Close()
	rt.AbortOn(ctx.Done())
	loop := stdlib.NewLoop(rt)
	rt.OnClose(loop.Close)
	if _, err := stdlib.Install(rt, stdlib.Config{
		Core: stdlib.CoreConfig{Loop: loop}, Docs: true,
	}); err != nil {
		return err
	}
	binding, err := agent.Install(ctx, rt, agent.Config{
		Loop: loop, Executable: executable, Provider: fixtureProvider{},
	})
	if err != nil {
		return err
	}
	defer binding.Close()
	done := make(chan struct{})
	var answer, failure string
	if err := rt.Set("embeddingDone", func(result, errorText string) {
		answer, failure = result, errorText
		close(done)
	}); err != nil {
		return err
	}
	if _, err := rt.EvalContext(ctx, `
		import("noe:agent").then(async ({run_agent}) => {
			const result = await run_agent({
				provider:"openrouter", model:"embedding/fixture", effort:"auto",
				maxTokens:1024, maxSteps:3, executionLimits:{providerCalls:3}
			}, "Compute the total of 7, 11, and 13 using JavaScript.");
			if (result.lastResult?.state !== "completed") throw Error(JSON.stringify(result));
			return result.lastResult.answer;
		}).then(answer => embeddingDone(answer, ""), error => embeddingDone("", String(error.stack || error)));
	`); err != nil {
		return err
	}
	// A completed task can leave an idle managed instance. Wait for the root
	// result, then close the binding rather than waiting for the whole loop.
	if err := loop.RunUntil(ctx, done); err != nil {
		return err
	}
	select {
	case <-done:
	default:
		return errors.New("loop stopped before the root result")
	}
	if failure != "" {
		return errors.New(failure)
	}
	binding.Close()
	fmt.Println(answer)
	return nil
}

type fixtureProvider struct{}

func (fixtureProvider) Generate(ctx context.Context, request agent.Request) (agent.Response, error) {
	if err := ctx.Err(); err != nil {
		return agent.Response{}, err
	}
	for _, raw := range request.Messages {
		var message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if json.Unmarshal(raw, &message) != nil || message.Role != "tool" {
			continue
		}
		var result struct {
			Stdout   string `json:"stdout"`
			ExitCode int    `json:"exitCode"`
		}
		if err := json.Unmarshal([]byte(message.Content), &result); err != nil {
			return agent.Response{}, err
		}
		if result.ExitCode != 0 || result.Stdout != "31\n" {
			return agent.Response{}, fmt.Errorf("unexpected run_js result: %s", message.Content)
		}
		return agent.Response{Message: json.RawMessage(`{"role":"assistant","content":"The total is 31."}`), FinishReason: "stop"}, nil
	}
	arguments, _ := json.Marshal(map[string]string{"code": "// Compute the requested total.\nconsole.log(7 + 11 + 13);"})
	message, err := json.Marshal(map[string]any{
		"role": "assistant", "content": nil,
		"tool_calls": []any{map[string]any{
			"id": "embedding-tool", "type": "function",
			"function": map[string]string{"name": "run_js", "arguments": string(arguments)},
		}},
	})
	return agent.Response{Message: message, FinishReason: "tool_calls"}, err
}
