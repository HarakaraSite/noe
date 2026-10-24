package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"forge.harakara.site/littleisland/noe/v2/agent/model"
	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

type directToolModels struct{}

func (directToolModels) ListModels(context.Context, model.ModelQuery) ([]model.Model, error) {
	return nil, nil
}

func (directToolModels) Generate(context.Context, model.Request) (model.Response, error) {
	return model.Response{}, model.ErrUnsupported
}
func (directToolModels) Decide(context.Context, model.DecisionRequest) (model.DecisionResponse, error) {
	return model.DecisionResponse{}, model.ErrUnsupported
}
func (directToolModels) GenerateImage(context.Context, model.ImageRequest) (model.GeneratedImage, error) {
	return model.GeneratedImage{}, model.ErrUnsupported
}

func TestDirectRunJSExecutorErrorIsKeptOutOfStderr(t *testing.T) {
	host := NewHost(Options{
		Models: modelsWithChat(&fixtureProvider{}),
		Execute: func(context.Context, Execution) (Result, error) {
			return Result{ExitCode: 0, Reason: "completed", Stderr: "actual stderr"}, errors.New("executor failed")
		},
	})
	t.Cleanup(host.Close)
	op := host.NewOperation(context.Background())
	t.Cleanup(op.Close)
	runID := startFixtureRun(t, op)
	code := "console.log('run')"
	request := runJSRequest{RunID: runID, Code: &code}
	responseJSON := lifecycleCall(t, op, "run_js", request)
	var response runJSResponse
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		t.Fatalf("decode run_js response %q: %v", responseJSON, err)
	}
	if response.Stderr != "actual stderr" {
		t.Fatalf("executor error was appended to user stderr: %q", response.Stderr)
	}
	if response.Reason != "failed" || response.ExitCode == 0 || len(response.Diagnostics) != 1 ||
		response.Diagnostics[0].Code != "execution_error" || response.Diagnostics[0].Stage != "execute" ||
		response.Diagnostics[0].Message != "executor failed" {
		t.Fatalf("executor error was not separated into diagnostics: %+v", response)
	}
}

type customProviderWithCatalog struct{ agentTestModelService }

func (customProviderWithCatalog) HasProvider(name string) (bool, error) {
	return name == "custom", nil
}

func TestDirectHostUsesModelServiceProviderClassification(t *testing.T) {
	models := customProviderWithCatalog{agentTestModelService{
		chat:    &fixtureProvider{},
		catalog: []model.Model{{Provider: "listed", Model: "model", Kind: "agent"}},
	}}
	host := NewHost(Options{Models: models})
	defer host.Close()
	op := host.NewOperation(context.Background())
	defer op.Close()
	response, err := op.Call("start", `{"provider":"custom","model":"model","name":"fixture","maxTokens":64,"effort":"none"}`)
	if err != nil || !json.Valid([]byte(response)) {
		t.Fatalf("custom provider start response=%q err=%v", response, err)
	}
	if _, err := op.Call("start", `{"provider":"missing","model":"model","name":"fixture","maxTokens":64,"effort":"none"}`); err == nil {
		t.Fatal("model service accepted a provider it does not classify")
	}
}

func TestDirectRunJSUsesHostExecutorAndRunScope(t *testing.T) {
	ctx := context.Background()
	called := 0
	var observed []events.Event
	host := NewHost(Options{

		Models:  directToolModels{},
		Observe: func(event events.Event) { observed = append(observed, event) },
		Execute: func(ctx context.Context, execution Execution) (Result, error) {
			called++
			if ctx.Err() != nil {
				t.Fatalf("executor context is canceled: %v", ctx.Err())
			}
			if execution.Operation == nil || execution.Operation.host == nil {
				t.Fatal("executor did not receive an owned operation")
			}
			if execution.Kind != ExecutionTool || execution.Code != "return 1" {
				t.Fatalf("execution=%+v", execution)
			}
			var config Config
			if err := json.Unmarshal(execution.Config, &config); err != nil {
				t.Fatalf("decode run defaults: %v", err)
			}
			if config.Provider != "fixture" || config.Model != "fixture-model" {
				t.Fatalf("tool defaults=%+v", config)
			}
			return Result{
				ExitCode: 0, Reason: "completed", Stdout: "tool output",
				Data:        json.RawMessage(`{"answer":42}`),
				Diagnostics: []executionresult.Diagnostic{{Code: "cleanup_error", Stage: "cleanup", Message: "close failed"}},
			}, nil
		},
	})
	defer host.Close()
	op := host.NewOperation(ctx)
	defer op.Close()

	startPayload, err := json.Marshal(startRequest{Provider: "fixture", Model: "fixture-model", Name: "fixture", MaxTokens: 64, Effort: "none"})
	if err != nil {
		t.Fatal(err)
	}
	startJSON, err := op.Call("start", string(startPayload))
	if err != nil {
		t.Fatal(err)
	}
	var started struct {
		RunID string `json:"runId"`
	}
	if err := json.Unmarshal([]byte(startJSON), &started); err != nil || started.RunID == "" {
		t.Fatalf("start response=%s err=%v", startJSON, err)
	}
	code := "return 1"
	requestPayload, err := json.Marshal(runJSRequest{RunID: started.RunID, Code: &code})
	if err != nil {
		t.Fatal(err)
	}
	responseJSON, err := op.Call("run_js", string(requestPayload))
	if err != nil {
		t.Fatal(err)
	}
	var response runJSResponse
	if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
		t.Fatalf("run_js response=%s err=%v", responseJSON, err)
	}
	if called != 1 || response.ExitCode != 0 || response.Reason != "completed" || response.Stdout != "tool output" {
		t.Fatalf("called=%d response=%+v", called, response)
	}
	if string(response.Data) != `{"answer":42}` || len(response.Diagnostics) != 1 || response.Diagnostics[0].Message != "close failed" {
		t.Fatalf("run_js response lost execution result fields: %+v", response)
	}
	var finished events.Event
	for _, event := range observed {
		if event["type"] == "run_js.finished" {
			finished = event
		}
	}
	if finished == nil {
		t.Fatal("run_js.finished event was not observed")
	}
	encoded, err := json.Marshal(finished)
	if err != nil {
		t.Fatal(err)
	}
	var eventFields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &eventFields); err != nil {
		t.Fatal(err)
	}
	if string(eventFields["data"]) != `{"answer":42}` || !json.Valid(eventFields["diagnostics"]) {
		t.Fatalf("run_js.finished omitted result fields: %s", encoded)
	}
}

func TestInvalidRunJSPragmaDoesNotExecute(t *testing.T) {
	for _, limit := range []int{0, 1023, 262145} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			called := false
			host := NewHost(Options{Models: modelsWithChat(&fixtureProvider{}), Execute: func(context.Context, Execution) (Result, error) { called = true; return Result{}, nil }})
			t.Cleanup(host.Close)
			op := host.NewOperation(context.Background())
			t.Cleanup(op.Close)
			runID := startFixtureRun(t, op)
			code := fmt.Sprintf("// Perform a write.\n// @noe: {\"outputBytes\": %d}\nthrow Error('must not run')", limit)
			responseJSON := lifecycleCall(t, op, "run_js", runJSRequest{RunID: runID, Code: &code})
			var response runJSResponse
			if err := json.Unmarshal([]byte(responseJSON), &response); err != nil {
				t.Fatal(err)
			}
			if called || response.Reason != "failed" || response.ExitCode == 0 || len(response.Diagnostics) != 1 || response.Diagnostics[0].Stage != "setup" {
				t.Fatalf("called=%v response=%+v", called, response)
			}
		})
	}
}
