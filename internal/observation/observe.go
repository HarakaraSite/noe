// Package observation implements NOE file snapshot and wait adapters.
package observation

import (
	"bytes"
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

	var previous []byte
	var scratch []byte
	hasPrevious := false
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		var snapshot Snapshot
		var terminal bool
		if q.Wait {
			content, err := readObservationLog(ctx, q.LogPath, scratch)
			if err != nil {
				return Snapshot{}, err
			}
			if hasPrevious {
				same, err := sameObservationLog(ctx, previous, content)
				if err != nil {
					return Snapshot{}, err
				}
				if same {
					scratch = content
					if err := waitForPoll(ctx); err != nil {
						return Snapshot{}, err
					}
					continue
				}
			}
			snapshot, terminal, err = analyzeObservation(ctx, q, content)
			if err != nil {
				return Snapshot{}, err
			}
			scratch = previous[:0]
			previous, hasPrevious = content, true
		} else {
			var err error
			snapshot, terminal, err = observeOnce(ctx, q)
			if err != nil {
				return Snapshot{}, err
			}
		}
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		if !q.Wait || terminal {
			return snapshot, nil
		}
		if err := waitForPoll(ctx); err != nil {
			return Snapshot{}, err
		}
	}
}

func sameObservationLog(ctx context.Context, previous, current []byte) (bool, error) {
	if len(previous) != len(current) {
		return false, nil
	}
	const chunkSize = 32 * 1024
	for offset := 0; offset < len(current); offset += chunkSize {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		end := min(offset+chunkSize, len(current))
		if !bytes.Equal(previous[offset:end], current[offset:end]) {
			return false, nil
		}
	}
	return true, nil
}

func waitForPoll(ctx context.Context) error {
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
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

func readObservationLog(ctx context.Context, path string, scratch []byte) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open observation log %q: %w", path, err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	content, err := readObservationBytes(ctx, file, info.Size(), scratch)
	if err != nil {
		return nil, fmt.Errorf("observation log %q: %w", path, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func readObservationBytes(ctx context.Context, reader io.Reader, limit int64, scratch []byte) ([]byte, error) {
	content := scratch[:0]
	const initialCapacity = 32 * 1024
	emptyReads := 0
	reader = observationContextReader{ctx: ctx, reader: reader}
	for int64(len(content)) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(content) == cap(content) {
			capacity := cap(content) * 2
			if capacity < initialCapacity {
				capacity = initialCapacity
			}
			grown := make([]byte, len(content), capacity)
			copy(grown, content)
			content = grown
		}
		readSize := cap(content) - len(content)
		remaining := limit - int64(len(content))
		if int64(readSize) > remaining {
			readSize = int(remaining)
		}
		n, err := reader.Read(content[len(content) : len(content)+readSize])
		content = content[:len(content)+n]
		if err != nil {
			if err == io.EOF {
				return content, nil
			}
			return nil, err
		}
		if n == 0 {
			emptyReads++
			if emptyReads >= 100 {
				return nil, io.ErrNoProgress
			}
			continue
		}
		emptyReads = 0
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return content, nil
}

func analyzeObservation(ctx context.Context, q Query, content []byte) (Snapshot, bool, error) {
	snapshot, err := loganalysis.Analyze(ctx, bytes.NewReader(content), loganalysis.Query{RunID: q.RunID, Cursor: q.Cursor, IncludeThinking: q.IncludeThinking, IncludeOutput: q.IncludeOutput})
	if err != nil {
		return Snapshot{}, false, fmt.Errorf("observation log %q: %w", q.LogPath, err)
	}
	return snapshot, isTerminalStatus(snapshot.Status), nil
}

type observationContextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r observationContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

func isTerminalStatus(status string) bool {
	return status == "completed" || status == "failed" || status == "cancelled"
}
