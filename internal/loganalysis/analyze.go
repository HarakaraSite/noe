// Package loganalysis reconstructs a run from a complete snapshot of event data.
package loganalysis

import (
	"context"
	jsonv1 "encoding/json"
	"errors"
	"fmt"
	"io"

	"forge.harakara.site/littleisland/noe/v2/internal/events"
	"forge.harakara.site/littleisland/noe/v2/internal/executionresult"
)

// Cursor identifies a position in one NOE execution's event stream. The
// execution ID is part of the cursor because explicit log files may be reused
// and appended to by multiple NOE processes whose sequence numbers restart.
type Cursor struct {
	ExecutionID string `json:"executionId"`
	Seq         uint64 `json:"seq"`
}

// Query describes one observation of a run in a JSONL event log.
type Query struct {
	RunID           string  `json:"runId"`
	Cursor          *Cursor `json:"cursor,omitempty"`
	IncludeThinking bool    `json:"includeThinking,omitempty"`
	IncludeOutput   bool    `json:"includeOutput,omitempty"`
}

// Usage contains accumulated provider-reported usage for a run.
type Usage struct {
	PromptTokens     int64   `json:"promptTokens,omitempty"`
	CompletionTokens int64   `json:"completionTokens,omitempty"`
	TotalTokens      int64   `json:"totalTokens,omitempty"`
	CachedTokens     int64   `json:"cachedTokens,omitempty"`
	Cost             float64 `json:"cost,omitempty"`
}

// ToolResult describes the most recently completed run_js call. Text contains
// that tool's captured stdout and stderr when output was requested.
type ToolResult struct {
	ToolCallID  string                       `json:"toolCallId"`
	Success     bool                         `json:"success"`
	ExitCode    int                          `json:"exitCode"`
	Reason      string                       `json:"reason,omitempty"`
	Duration    int64                        `json:"duration,omitempty"`
	Text        string                       `json:"text,omitempty"`
	Data        jsonv1.RawMessage            `json:"data,omitzero"`
	StdoutFile  *executionresult.OutputFile  `json:"stdoutFile,omitempty"`
	StderrFile  *executionresult.OutputFile  `json:"stderrFile,omitempty"`
	DataFile    *executionresult.OutputFile  `json:"dataFile,omitempty"`
	Diagnostics []executionresult.Diagnostic `json:"diagnostics,omitempty"`
}

// Snapshot is the reconstructed state of one run plus its meaningful event
// delta. State fields are always derived from the full run history; Cursor
// filters only Events.
type Snapshot struct {
	GroupID        string            `json:"groupId,omitempty"`
	Execution      jsonv1.RawMessage `json:"execution,omitzero"`
	Context        jsonv1.RawMessage `json:"context,omitzero"`
	AgentID        string            `json:"agentId,omitempty"`
	RunID          string            `json:"runId"`
	ParentRunID    string            `json:"parentRunId"`
	Name           string            `json:"name"`
	Model          string            `json:"model"`
	Status         string            `json:"status"`
	Phase          string            `json:"phase"`
	Action         string            `json:"action"`
	LastMessage    string            `json:"lastMessage"`
	FinalText      string            `json:"finalText,omitempty"`
	Error          string            `json:"error,omitempty"`
	LastToolResult *ToolResult       `json:"lastToolResult,omitempty"`
	Usage          *Usage            `json:"usage,omitempty"`
	UpdatedAt      string            `json:"updatedAt"`
	Cursor         Cursor            `json:"cursor"`
	Events         []events.Event    `json:"events"`
}

// Analyze borrows reader. Cursor filters returned events only; state is rebuilt
// from every complete input line, ignoring an unterminated tail.
func Analyze(ctx context.Context, reader io.Reader, q Query) (Snapshot, error) {
	if ctx == nil || reader == nil {
		return Snapshot{}, errors.New("log: context and reader are required")
	}
	if q.RunID == "" {
		return Snapshot{}, errors.New("log: runId is required")
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if q.Cursor != nil && q.Cursor.ExecutionID == "" && q.Cursor.Seq == 0 {
		q.Cursor = nil
	}
	var targetExecution string
	var maxSeq uint64
	priorMaxSeq := make(map[string]uint64)
	ambiguous := false
	state := newRunState(q.RunID)
	projected := projectedEvents{events: &state.snapshot.Events}
	after := cursorSeq(q.Cursor)
	err := scanRecords(ctx, reader, func(rec record) {
		if ambiguous {
			return
		}
		matchesRun := stringField(rec.event, "runId") == q.RunID
		if targetExecution == "" {
			if !matchesRun {
				priorMaxSeq[rec.executionID] = max(priorMaxSeq[rec.executionID], rec.seq)
				return
			}
			targetExecution = rec.executionID
			maxSeq = priorMaxSeq[targetExecution]
			priorMaxSeq = nil
		}
		if rec.executionID != targetExecution {
			ambiguous = matchesRun
			return
		}
		// The cursor covers the execution, including other runs and hidden events.
		maxSeq = max(maxSeq, rec.seq)
		if !matchesRun || q.Cursor != nil && q.Cursor.ExecutionID != targetExecution {
			return
		}
		state.apply(rec, q.IncludeOutput)
		if rec.seq > after {
			if event := projectEvent(rec.event, q); event != nil {
				projected.append(event, rec.seq)
			}
		}
	})
	if err != nil {
		return Snapshot{}, err
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	// Decide selection errors only after all complete lines have been validated.
	if ambiguous {
		return Snapshot{}, fmt.Errorf("runId %q occurs in multiple executions; the target is ambiguous", q.RunID)
	}

	if targetExecution == "" {
		next := Cursor{}
		if q.Cursor != nil {
			next = *q.Cursor
		}
		return Snapshot{
			RunID: q.RunID, Status: "unknown", Phase: "unknown", Cursor: next,
			Events: make([]events.Event, 0),
		}, nil
	}
	if q.Cursor != nil && q.Cursor.ExecutionID != targetExecution {
		return Snapshot{}, fmt.Errorf("cursor executionId %q does not match runId %q executionId %q; use the cursor returned for this run", q.Cursor.ExecutionID, q.RunID, targetExecution)
	}

	projected.finishGroup()
	state.snapshot.Cursor = Cursor{ExecutionID: targetExecution, Seq: maxSeq}
	state.snapshot.Phase = state.currentPhase()
	if state.snapshot.Status == "completed" || state.snapshot.Status == "failed" || state.snapshot.Status == "cancelled" {
		state.snapshot.Phase = "settled"
	}
	state.snapshot.Action = state.currentAction()
	if state.snapshot.Events == nil {
		state.snapshot.Events = make([]events.Event, 0)
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	return state.snapshot, nil
}

func cursorSeq(cursor *Cursor) uint64 {
	if cursor == nil {
		return 0
	}
	return cursor.Seq
}
