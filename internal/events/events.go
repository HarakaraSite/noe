// Package events writes the common JSONL observation stream. It has no runtime
// or provider dependencies so CLI output and agent workers share one writer.
package events

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"sync"
	"time"
)

// Event contains event-specific fields. Writer owns version, seq, time and
// executionId. Callers must not modify an event while Emit is using it.
type Event map[string]any

type Writer struct {
	mu  sync.Mutex
	out io.Writer
	id  string
	seq uint64
	err error
}

func New(out io.Writer, id string) *Writer { return &Writer{out: out, id: id} }

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
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		return
	}
	record := make(Event, len(event)+4)
	for k, v := range event {
		record[k] = v
	}
	w.seq++
	record["version"], record["seq"] = 1, w.seq
	record["time"], record["executionId"] = time.Now().UTC().Format(time.RFC3339Nano), w.id
	w.err = json.NewEncoder(w.out).Encode(record)
}

func (w *Writer) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}
