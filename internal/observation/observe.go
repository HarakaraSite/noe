// Package observation reconstructs one agent run from a NOE JSONL event log.
package observation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"noe/internal/events"
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
	LogPath         string  `json:"logPath"`
	RunID           string  `json:"runId"`
	Cursor          *Cursor `json:"cursor,omitempty"`
	Wait            bool    `json:"wait,omitempty"`
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
	ToolCallID string `json:"toolCallId"`
	Success    bool   `json:"success"`
	ExitCode   int    `json:"exitCode"`
	Reason     string `json:"reason,omitempty"`
	Duration   int64  `json:"duration,omitempty"`
	Text       string `json:"text,omitempty"`
}

// Snapshot is the reconstructed state of one run plus its meaningful event
// delta. State fields are always derived from the full run history; Cursor
// filters only Events.
type Snapshot struct {
	GroupID        string          `json:"groupId,omitempty"`
	Execution      json.RawMessage `json:"execution,omitempty"`
	Context        json.RawMessage `json:"context,omitempty"`
	AgentID        string          `json:"agentId,omitempty"`
	RunID          string          `json:"runId"`
	ParentRunID    string          `json:"parentRunId"`
	Name           string          `json:"name"`
	Model          string          `json:"model"`
	Status         string          `json:"status"`
	Phase          string          `json:"phase"`
	Action         string          `json:"action"`
	LastMessage    string          `json:"lastMessage"`
	FinalText      string          `json:"finalText,omitempty"`
	Error          string          `json:"error,omitempty"`
	LastToolResult *ToolResult     `json:"lastToolResult,omitempty"`
	Usage          *Usage          `json:"usage,omitempty"`
	UpdatedAt      string          `json:"updatedAt"`
	Cursor         Cursor          `json:"cursor"`
	Events         []events.Event  `json:"events"`
}

// Observe reads the log on every call so callers can reuse a cursor without
// maintaining any hidden reader state. When Wait is set, it polls until the
// selected run reaches an agent lifecycle terminal event or ctx is cancelled.
func Observe(ctx context.Context, q Query) (Snapshot, error) {
	if q.LogPath == "" {
		return Snapshot{}, errors.New("observation logPath is required")
	}
	if q.RunID == "" {
		return Snapshot{}, errors.New("observation runId is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	// An unknown run has no execution yet. Its empty initial cursor can be
	// passed back unchanged when the first records become available.
	if q.Cursor != nil && q.Cursor.ExecutionID == "" && q.Cursor.Seq == 0 {
		q.Cursor = nil
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		snapshot, terminal, err := observeOnce(ctx, q)
		if err != nil {
			return Snapshot{}, err
		}
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if !q.Wait || terminal {
			return snapshot, nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return Snapshot{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func observeOnce(ctx context.Context, q Query) (Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, false, err
	}
	records, err := readRecords(ctx, q.LogPath)
	if err != nil {
		return Snapshot{}, false, err
	}

	targetExecution := ""
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if stringField(rec.event, "runId") != q.RunID {
			continue
		}
		if targetExecution != "" && targetExecution != rec.executionID {
			return Snapshot{}, false, fmt.Errorf("runId %q occurs in multiple executions in %q; the target is ambiguous", q.RunID, q.LogPath)
		}
		targetExecution = rec.executionID
	}

	if targetExecution == "" {
		next := Cursor{}
		if q.Cursor != nil {
			next = *q.Cursor
		}
		return Snapshot{
			RunID: q.RunID, Status: "unknown", Phase: "unknown", Cursor: next,
			Events: make([]events.Event, 0),
		}, false, nil
	}
	if q.Cursor != nil && q.Cursor.ExecutionID != targetExecution {
		return Snapshot{}, false, fmt.Errorf("cursor executionId %q does not match runId %q executionId %q; use the cursor returned for this run", q.Cursor.ExecutionID, q.RunID, targetExecution)
	}

	maxSeq := uint64(0)
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if rec.executionID == targetExecution && rec.seq > maxSeq {
			maxSeq = rec.seq
		}
	}

	state := newRunState(q.RunID)
	var lastProjectedSeq uint64
	for _, rec := range records {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, false, err
		}
		if rec.executionID != targetExecution || stringField(rec.event, "runId") != q.RunID {
			continue
		}
		state.apply(rec, q.IncludeOutput)
		if rec.seq > cursorSeq(q.Cursor) {
			if projected := projectEvent(rec.event, q); projected != nil {
				appendProjected(&state.snapshot.Events, projected, rec.seq, &lastProjectedSeq)
			}
		}
	}

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
		return Snapshot{}, false, err
	}
	return state.snapshot, isTerminalStatus(state.snapshot.Status), nil
}

func cursorSeq(cursor *Cursor) uint64 {
	if cursor == nil {
		return 0
	}
	return cursor.Seq
}
