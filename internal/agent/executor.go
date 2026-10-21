package agent

import (
	"context"
	"encoding/json"
	"errors"
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
	Stdout             string `json:"stdout"`
	Stderr             string `json:"stderr"`
	ExitCode           int    `json:"exitCode"`
	Reason             string `json:"reason"`
	StdoutOmittedBytes int64  `json:"stdoutOmittedBytes,omitempty"`
	StderrOmittedBytes int64  `json:"stderrOmittedBytes,omitempty"`
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

// Executor runs one task or tool in the caller-selected runtime environment.
// The returned process result is interpreted by the same Host lifecycle code
// used by legacy child-process execution.
type Executor func(context.Context, Execution) (Result, error)
