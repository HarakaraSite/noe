// Package agentexec runs task and tool source in fresh in-process runtimes.
package agentexec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	core "forge.harakara.site/littleisland/noe/v2/internal/agent"
	"forge.harakara.site/littleisland/noe/v2/internal/agentbridge"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/stdlib"
)

type Executor struct {
	Profile stdlib.Profile
	Host    *core.Host
}

type capture struct {
	sync.Mutex
	capturedOutput
}

func (c *capture) Write(p []byte) (int, error) {
	c.Lock()
	defer c.Unlock()
	return c.capturedOutput.Write(p)
}
func (c *capture) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Write(p)
}
func (c *capture) result() (string, int64) {
	c.Lock()
	defer c.Unlock()
	return c.capturedOutput.result()
}

// Execute creates only the runtime required by this task or tool. Configuration,
// callbacks and scope stay in Go; no source file or capability is serialized.
func (e *Executor) Execute(ctx context.Context, execution core.Execution) (core.Result, error) {
	profile := e.Profile
	stdout := &capture{capturedOutput: capturedOutput{maxBytes: int(execution.OutputBytes)}}
	stderr := &capture{capturedOutput: capturedOutput{maxBytes: int(execution.OutputBytes)}}
	profile.Environment.Stdin = strings.NewReader("")
	profile.Environment.Stdout, profile.Environment.Stderr = stdout, stderr
	initialize := profile.Initialize
	profile.Initialize = func(ctx context.Context, view *stdlib.Bindings) (func(), error) {
		var cleanup func()
		if initialize != nil {
			var err error
			cleanup, err = initialize(ctx, view)
			if err != nil {
				return cleanup, err
			}
		}
		if view.Worker() {
			return cleanup, nil
		}
		binding, err := agentbridge.AttachOperation(ctx, view, e.Host, execution.Operation)
		if err != nil {
			return cleanup, err
		}
		return func() {
			binding.Close()
			if cleanup != nil {
				cleanup()
			}
		}, nil
	}
	code := execution.Code
	if execution.Kind == "task" {
		if !json.Valid(execution.Config) {
			return core.Result{}, errors.New("agent: invalid task configuration")
		}
		encoded, _ := json.Marshal(string(execution.Config))
		instruction, _ := json.Marshal(execution.Instruction)
		code = "import {executeTask} from 'noe:agent/task';\nawait executeTask(JSON.parse(" + string(encoded) + ")," + string(instruction) + ");"
	} else if execution.Kind != "tool" {
		return core.Result{}, fmt.Errorf("agent: unknown execution kind %q", execution.Kind)
	}
	result, err := stdlib.Execute(ctx, stdlib.Source{Name: "[agent " + execution.Kind + "]", Code: code, Module: true, Virtual: true}, profile)
	out := core.Result{
		ExitCode: result.Code, Reason: "completed",
		Data:        append(json.RawMessage(nil), result.Data...),
		Diagnostics: append([]executionresult.Diagnostic(nil), result.Diagnostics...),
	}
	if result.Code != 0 {
		out.Reason = "failed"
	}
	if err != nil {
		out.ExitCode = 1
		out.Reason = "failed"
		if errors.Is(err, context.Canceled) {
			out.Reason = "cancelled"
		}
		if errors.Is(err, context.DeadlineExceeded) {
			out.Reason = "timeout"
		}
		if len(out.Diagnostics) == 0 {
			code, stage := "execution_error", "execute"
			if errors.Is(err, context.Canceled) {
				code = "cancelled"
			}
			if errors.Is(err, context.DeadlineExceeded) {
				code = "timeout"
			}
			out.Diagnostics = append(out.Diagnostics, executionresult.Diagnostic{
				Code: code, Stage: stage, Message: stdlib.Diagnostic(err),
			})
		}
	}
	// Task failures are reported through their scoped finish request; VM errors
	// remain an execution result, as with nonzero command exits.
	out.Stdout, out.StdoutOmittedBytes = stdout.result()
	out.Stderr, out.StderrOmittedBytes = stderr.result()
	return out, nil
}
