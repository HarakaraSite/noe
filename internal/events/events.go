// Package events writes the common JSONL observation stream. It has no runtime
// or provider dependencies so CLI output and agent workers share one writer.
package events

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"noe/internal/hostio"
	"time"
)

// Event contains event-specific fields. Writer owns version, seq, time and
// executionId. Callers must not modify an event while Emit is using it.
type Event map[string]any

// CommittedCancellation reports cancellation of an auxiliary display after a
// complete JSONL record was committed. The sequence must stay consumed, but
// the next record can still be written safely.
type CommittedCancellation struct{ Err error }

func (e *CommittedCancellation) Error() string { return e.Err.Error() }
func (e *CommittedCancellation) Unwrap() error { return e.Err }

type Writer struct {
	gate chan struct{}
	out  io.Writer
	id   string
	seq  uint64
	err  error
}

func New(out io.Writer, id string) *Writer {
	gate := make(chan struct{}, 1)
	gate <- struct{}{}
	return &Writer{out: out, id: id, gate: gate}
}

func NewID() (string, error) {
	var id [8]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(id[:]), nil
}

// Emit serializes concurrent producers into complete JSONL records. The first
// write error is retained for the CLI to report as an execution failure.
func (w *Writer) Emit(event Event) {
	_ = w.emit(context.Background(), event, false)
}

// EmitContext preserves the same sequence and serialization while allowing a
// native stream write to cancel both lock acquisition and the output write.
func (w *Writer) EmitContext(ctx context.Context, event Event) error {
	return w.emit(ctx, event, true)
}

func (w *Writer) emit(ctx context.Context, event Event, cancellable bool) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.gate:
	}
	defer func() { w.gate <- struct{}{} }()
	if w.err != nil {
		return w.err
	}
	record := make(Event, len(event)+4)
	for k, v := range event {
		record[k] = v
	}
	w.seq++
	record["version"], record["seq"] = 1, w.seq
	record["time"], record["executionId"] = time.Now().UTC().Format(time.RFC3339Nano), w.id
	if !cancellable {
		w.err = json.NewEncoder(w.out).Encode(record)
		return w.err
	}
	data, err := json.Marshal(record)
	if err == nil {
		var out hostio.Writer
		out, err = hostio.NewWriter(w.out)
		if err == nil {
			defer out.Close()
			var written int
			written, err = out.WriteContext(ctx, append(data, '\n'))
			var committed *CommittedCancellation
			if ctx.Err() != nil && errors.Is(err, ctx.Err()) && errors.As(err, &committed) {
				return err
			}
			if written == 0 && ctx.Err() != nil && errors.Is(err, ctx.Err()) {
				// Nothing from this record reached the sink. The caller may
				// recover from a stream abort without poisoning future records.
				w.seq--
				return err
			}
		}
	}
	w.err = err
	return err
}

func (w *Writer) Err() error {
	<-w.gate
	defer func() { w.gate <- struct{}{} }()
	return w.err
}
