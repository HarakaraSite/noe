package jsexec

import (
	"context"
	"errors"

	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	quickjs "github.com/go-quickjs/go-quickjs"
)

type executionStageError struct {
	stage string
	err   error
}

func (e *executionStageError) Error() string { return e.err.Error() }
func (e *executionStageError) Unwrap() error { return e.err }

func diagnosticsFor(err error, fallbackStage string) []executionresult.Diagnostic {
	if err == nil {
		return nil
	}
	if staged, ok := err.(*executionStageError); ok {
		return diagnosticsForStage(staged.err, staged.stage)
	}
	return diagnosticsForStage(err, fallbackStage)
}

func diagnosticsForStage(err error, stage string) []executionresult.Diagnostic {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []executionresult.Diagnostic
		for _, cause := range joined.Unwrap() {
			out = append(out, diagnosticsForStage(cause, stage)...)
		}
		return out
	}
	if staged, ok := err.(*executionStageError); ok {
		return diagnosticsForStage(staged.err, staged.stage)
	}
	code := diagnosticCode(err, stage)
	if detached, ok := err.(*diagnosticError); ok {
		data := detached.structured
		data.Code, data.Stage = code, stage
		if data.Message == "" {
			data.Message = detached.message
		}
		return []executionresult.Diagnostic{data}
	}
	diagnostic := executionresult.Diagnostic{Code: code, Stage: stage, Message: err.Error()}
	var syntax *quickjs.SyntaxError
	if errors.As(err, &syntax) {
		source, line, column := syntax.Position()
		diagnostic.Position = &executionresult.Position{Source: source, Line: line, Column: column}
	}
	return []executionresult.Diagnostic{diagnostic}
}

func diagnosticCode(err error, stage string) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case stage == "cleanup":
		return "cleanup_error"
	case stage == "serialize":
		return "serialization_error"
	default:
		return "execution_error"
	}
}

func errorLeaves(err error) []error {
	if err == nil {
		return nil
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []error
		for _, cause := range joined.Unwrap() {
			out = append(out, errorLeaves(cause)...)
		}
		return out
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		if cause := wrapped.Unwrap(); cause != nil {
			return errorLeaves(cause)
		}
	}
	return []error{err}
}
