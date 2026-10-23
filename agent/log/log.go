// Package log reconstructs agent state from caller-supplied event snapshots.
// It does not open files, poll, start a Host, or contact model providers.
package log

import (
	"bytes"
	"context"
	"io"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
	"forge.harakara.site/littleisland/noe/v2/internal/loganalysis"
)

type Query = loganalysis.Query
type Snapshot = loganalysis.Snapshot
type Cursor = loganalysis.Cursor
type Usage = loganalysis.Usage
type ToolResult = loganalysis.ToolResult
type ExecutionDiagnostic = executionresult.Diagnostic
type DiagnosticPosition = executionresult.Position
type Event = events.Event

func Analyze(ctx context.Context, reader io.Reader, query Query) (Snapshot, error) {
	return loganalysis.Analyze(ctx, reader, query)
}
func AnalyzeBytes(ctx context.Context, data []byte, query Query) (Snapshot, error) {
	return Analyze(ctx, bytes.NewReader(data), query)
}
