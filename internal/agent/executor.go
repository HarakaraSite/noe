package agent

import (
	"context"
	"encoding/json"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

const (
	ExecutionTask = "task"
	ExecutionTool = "tool"
)

// Execution describes one fresh JavaScript task or tool execution. The
// operation is Host-owned and carries the scope needed by agent calls made by
// the executor. Config is a JSON snapshot of the task or run configuration.
type Execution struct {
	Operation   *Operation
	Kind        string
	Config      json.RawMessage
	Instruction string
	Code        string
}

// Result is the observable result of a task or tool execution, independent of
// whether the selected runtime uses an in-process VM or a supervised process.
type Result struct {
	Stdout             string                       `json:"stdout"`
	Stderr             string                       `json:"stderr"`
	ExitCode           int                          `json:"exitCode"`
	Reason             string                       `json:"reason"`
	StdoutOmittedBytes int64                        `json:"stdoutOmittedBytes,omitempty"`
	StderrOmittedBytes int64                        `json:"stderrOmittedBytes,omitempty"`
	Data               json.RawMessage              `json:"data,omitempty"`
	Diagnostics        []executionresult.Diagnostic `json:"diagnostics,omitempty"`
}

func failedResult() Result { return Result{ExitCode: -1, Reason: "failed"} }

func reasonForContext(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	return "failed"
}

func appendExecutionDiagnostic(result *Result, err error, stage string) {
	if result == nil || err == nil {
		return
	}
	code := "execution_error"
	switch {
	case errors.Is(err, context.Canceled):
		code = "cancelled"
	case errors.Is(err, context.DeadlineExceeded):
		code = "timeout"
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code && diagnostic.Message == err.Error() {
			return
		}
	}
	result.Diagnostics = append(result.Diagnostics, executionresult.Diagnostic{
		Code: code, Stage: stage, Message: err.Error(),
	})
}

func appendReasonDiagnostic(result *Result, stage string) {
	if result == nil {
		return
	}
	code, message := "", ""
	switch result.Reason {
	case "cancelled":
		code, message = "cancelled", "execution was cancelled"
	case "timeout":
		code, message = "timeout", "execution deadline exceeded"
	default:
		return
	}
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == code {
			return
		}
	}
	result.Diagnostics = append(result.Diagnostics, executionresult.Diagnostic{
		Code: code, Stage: stage, Message: message,
	})
}

// Executor runs one task or tool in the caller-selected runtime environment.
// The returned process result is interpreted by the same Host lifecycle code
// used by legacy child-process execution.
type Executor func(context.Context, Execution) (Result, error)
