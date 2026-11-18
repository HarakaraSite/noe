// Package agentexec runs task and tool source in fresh in-process runtimes.
package agentexec

import (
	"context"
	jsonv1 "encoding/json"
	"encoding/json/jsontext"
	json "encoding/json/v2"
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
	Profile         stdlib.Profile
	Host            *core.Host
	OutputDirectory *OutputDirectory
}

type capture struct {
	sync.Mutex
	capturedOutput
	spill  *outputSpill
	closed bool
}

func (c *capture) Write(p []byte) (int, error) {
	c.Lock()
	defer c.Unlock()
	if c.closed {
		return 0, errors.New("agent output capture is closed")
	}
	if c.spill != nil {
		c.spill.write(&c.capturedOutput, p)
	}
	return c.capturedOutput.Write(p)
}
func (c *capture) WriteContext(ctx context.Context, p []byte) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return c.Write(p)
}
func (c *capture) finish(result *core.Result, stream string) (string, int64, *executionresult.OutputFile) {
	c.Lock()
	defer c.Unlock()
	c.closed = true
	text, omitted := c.capturedOutput.result()
	var file *executionresult.OutputFile
	if c.spill != nil {
		var err error
		file, err = c.spill.finish(c.total)
		if err != nil {
			// Filesystem errors can contain host paths under a virtual root.
			result.Diagnostics = append(result.Diagnostics, executionresult.Diagnostic{
				Code: "output_save_error", Stage: "cleanup",
				Message: "Could not save full " + stream + " to the configured tool output directory",
			})
		}
	}
	return text, omitted, file
}

// Execute creates only the runtime required by this task or tool. Configuration,
// callbacks and scope stay in Go; no source file or capability is serialized.
func (e *Executor) Execute(ctx context.Context, execution core.Execution) (core.Result, error) {
	profile := e.Profile
	stdout := &capture{capturedOutput: capturedOutput{maxBytes: int(execution.OutputBytes)}}
	stderr := &capture{capturedOutput: capturedOutput{maxBytes: int(execution.OutputBytes)}}
	if execution.Kind == core.ExecutionTool && e.OutputDirectory != nil {
		stdout.spill = &outputSpill{directory: e.OutputDirectory, stream: "stdout"}
		stderr.spill = &outputSpill{directory: e.OutputDirectory, stream: "stderr"}
	}
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
		if !jsontext.Value(execution.Config).IsValid() {
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
		Data:        append(jsonv1.RawMessage(nil), result.Data...),
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
	out.Stdout, out.StdoutOmittedBytes, out.StdoutFile = stdout.finish(&out, "stdout")
	out.Stderr, out.StderrOmittedBytes, out.StderrFile = stderr.finish(&out, "stderr")
	return out, nil
}
