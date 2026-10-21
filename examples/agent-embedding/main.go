// This example runs a managed agent through the public Go Host and Client API.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"forge.harakara.site/littleisland/noe/v2/agent"
	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	models, err := model.NewService(model.Options{Provider: fixtureProvider{}})
	if err != nil {
		return err
	}
	runtimeProfile := &stdlib.Profile{}
	host, err := agent.NewHost(ctx, agent.HostConfig{Models: models, Runtime: runtimeProfile})
	if err != nil {
		return err
	}
	defer host.Close()

	client, err := host.NewClient(ctx)
	if err != nil {
		return err
	}
	defer client.Close()

	instruction := "Return the total of 7, 11, and 13."
	snapshot, err := client.Run(ctx, agent.SpawnRequest{
		Config: agent.TaskConfig{
			Config:   agent.AgentConfig{Provider: "fixture", Model: "fixture/model", Name: "embedding-example", Effort: "none"},
			MaxSteps: 1,
		},
		Instruction: &instruction,
	})
	if err != nil {
		return err
	}
	if snapshot.LastResult == nil || snapshot.LastResult.State != "completed" {
		return fmt.Errorf("agent task did not complete: %+v", snapshot.LastResult)
	}
	fmt.Println(snapshot.LastResult.Answer)
	return nil
}

type fixtureProvider struct{}

func (fixtureProvider) Generate(ctx context.Context, _ model.Request) (model.Response, error) {
	if err := ctx.Err(); err != nil {
		return model.Response{}, err
	}
	return model.Response{
		Message:      json.RawMessage(`{"role":"assistant","content":"The total is 31."}`),
		FinishReason: "stop",
	}, nil
}

var _ model.Provider = fixtureProvider{}
