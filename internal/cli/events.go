package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"

	"noe/internal/agent"
	"noe/internal/events"
)

// observation owns its file and output adapters for exactly one CLI invocation.
// REPL prompt/control bytes never go through these adapters.
type observation struct {
	writer         *events.Writer
	file           *os.File
	toStdout       bool
	stdout, stderr *eventOutput
	cancel         context.CancelFunc
	renderer       *agentRenderer
}

func openObservation(ctx context.Context, option *string, human bool, stdout, stderr io.Writer) (context.Context, *observation, error) {
	if option == nil && !human {
		return ctx, nil, nil
	}
	id, err := events.NewID()
	if err != nil {
		return ctx, nil, fmt.Errorf("noe: create events ID: %w", err)
	}
	ctx, cancel := context.WithCancel(ctx)
	o := &observation{toStdout: human || (option != nil && *option == "-"), cancel: cancel}
	var out io.Writer
	if option != nil && *option != "-" {
		path := *option
		flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
		if path == "" {
			path = "noe-events-" + time.Now().Format("20060102-150405") + "-" + id + ".jsonl"
			flags |= os.O_EXCL
		}
		path, err = filepath.Abs(path)
		if err == nil {
			o.file, err = os.OpenFile(path, flags, 0600)
		}
		if err != nil {
			cancel()
			return ctx, nil, fmt.Errorf("noe: open events file: %w", err)
		}
		out = o.file
		if *option == "" {
			fmt.Fprintln(stderr, "noe: events:", path)
		}
	} else if !human {
		out = stdout
	}
	if human {
		o.renderer = newAgentRenderer(stdout)
		out = &renderedEventsWriter{record: out, renderer: o.renderer}
	}
	o.writer = events.New(cancelOnWriteError{out: out, cancel: cancel}, id)
	var mirrorOut, mirrorErr io.Writer
	if !o.toStdout {
		mirrorOut, mirrorErr = stdout, stderr
	}
	o.stdout = &eventOutput{events: o.writer, kind: "stdout", mirror: mirrorOut}
	o.stderr = &eventOutput{events: o.writer, kind: "stderr", mirror: mirrorErr}
	return ctx, o, nil
}

func (o *observation) configureAgent(options *agent.Options) {
	if o != nil {
		options.Observe = o.writer.Emit
	}
}

func (o *observation) emit(event events.Event) {
	if o != nil {
		o.writer.Emit(event)
	}
}

func (o *observation) finish(code int, diagnostic io.Writer) int {
	if o == nil {
		return code
	}
	defer o.cancel()
	o.stdout.flush()
	o.stderr.flush()
	o.emit(events.Event{"type": "execution.finished", "exitCode": code})
	err := o.writer.Err()
	if o.renderer != nil {
		if renderErr := o.renderer.finish(); err == nil {
			err = renderErr
		}
	}
	if o.file != nil {
		if closeErr := o.file.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintln(diagnostic, "noe: write events:", err)
		return 1
	}
	return code
}

// Writer serializes complete JSONL records before calling this adapter, so the
// shared renderer sees the same values and ordering as `| noe observe`. Optional
// recording retains the original JSONL independently of the terminal format.
type renderedEventsWriter struct {
	record   io.Writer
	renderer *agentRenderer
}

func (w *renderedEventsWriter) Write(p []byte) (int, error) {
	if w.record != nil {
		n, err := w.record.Write(p)
		if err != nil {
			return n, err
		}
		if n != len(p) {
			return n, io.ErrShortWrite
		}
	}
	var event events.Event
	if err := json.Unmarshal(p, &event); err != nil {
		return 0, err
	}
	if err := w.renderer.render(event); err != nil {
		return 0, err
	}
	return len(p), nil
}

type cancelOnWriteError struct {
	out    io.Writer
	cancel context.CancelFunc
}

func (w cancelOnWriteError) Write(p []byte) (int, error) {
	n, err := w.out.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.cancel()
	}
	return n, err
}

// eventOutput emits text fragments immediately, including writes without a
// newline. Only incomplete UTF-8 runes are held until the next write or close.
type eventOutput struct {
	mu      sync.Mutex
	events  *events.Writer
	kind    string
	mirror  io.Writer
	pending []byte
}

func (w *eventOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	var mirrorErr error
	if w.mirror != nil {
		n, mirrorErr = w.mirror.Write(p)
		if mirrorErr == nil && n != len(p) {
			mirrorErr = io.ErrShortWrite
		}
	}
	// Capture the JS output even when its ordinary terminal mirror fails.
	w.pending = append(w.pending, p...)
	at := 0
	for at < len(w.pending) && utf8.FullRune(w.pending[at:]) {
		_, size := utf8.DecodeRune(w.pending[at:])
		at += size
	}
	if at > 0 {
		w.events.Emit(events.Event{"type": w.kind, "text": string(w.pending[:at])})
		w.pending = w.pending[at:]
	}
	if err := w.events.Err(); err != nil {
		return n, err
	}
	return n, mirrorErr
}

func (w *eventOutput) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) != 0 {
		w.events.Emit(events.Event{"type": w.kind, "text": string(w.pending)})
		w.pending = nil
	}
}
