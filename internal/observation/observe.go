// Package observation implements NOE file snapshot and wait adapters.
package observation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"forge.harakara.site/littleisland/noe/v2/internal/loganalysis"
)

type Cursor = loganalysis.Cursor
type Snapshot = loganalysis.Snapshot
type Usage = loganalysis.Usage
type ToolResult = loganalysis.ToolResult
type Query struct {
	LogPath         string  `json:"logPath"`
	RunID           string  `json:"runId"`
	Cursor          *Cursor `json:"cursor,omitempty"`
	Wait            bool    `json:"wait,omitempty"`
	IncludeThinking bool    `json:"includeThinking,omitempty"`
	IncludeOutput   bool    `json:"includeOutput,omitempty"`
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
	file, err := os.Open(q.LogPath)
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("open observation log %q: %w", q.LogPath, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Snapshot{}, false, err
	}
	snapshot, err := loganalysis.Analyze(ctx, io.LimitReader(file, info.Size()), loganalysis.Query{RunID: q.RunID, Cursor: q.Cursor, IncludeThinking: q.IncludeThinking, IncludeOutput: q.IncludeOutput})
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("observation log %q: %w", q.LogPath, err)
	}
	return snapshot, isTerminalStatus(snapshot.Status), nil
}
func isTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}
