package provider

import (
	"bytes"
	jsonv1 "encoding/json"
	"io"
	"strings"
	"sync"
)

// observedBody lets the SDK consume its own EventStream while independently
// recording complete SSE events and the underlying body's read errors. The SDK's EventStream intentionally stops reading at [DONE].
type observedBody struct {
	source  io.ReadCloser
	observe func(StreamEvent)
	extract func(jsonv1.RawMessage) []StreamEvent

	mu        sync.Mutex
	line      []byte
	skipLF    bool
	firstLine bool
	dataLines []string
	// Each payload owns its bytes and is immutable after append. snapshot callers
	// must not modify payload bytes; snapshot only copies this outer slice.
	events  []jsonv1.RawMessage
	done    bool
	readErr error

	closeOnce sync.Once
	closeErr  error
}

func newObservedBody(source io.ReadCloser) *observedBody {
	return &observedBody{source: source}
}

func (b *observedBody) Read(p []byte) (int, error) {
	n, err := b.source.Read(p)
	b.mu.Lock()
	var completed []jsonv1.RawMessage
	if n > 0 {
		completed = b.feedLocked(p[:n])
	}
	if err != nil && err != io.EOF && b.readErr == nil {
		b.readErr = err
	}
	b.mu.Unlock()
	if b.observe != nil {
		extract := b.extract
		if extract == nil {
			extract = streamObservations
		}
		for _, event := range completed {
			for _, observation := range extract(event) {
				b.observe(observation)
			}
		}
	}
	return n, err
}

func (b *observedBody) Close() error {
	b.closeOnce.Do(func() { b.closeErr = b.source.Close() })
	return b.closeErr
}

func (b *observedBody) snapshot() (events []jsonv1.RawMessage, done bool, readErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	events = make([]jsonv1.RawMessage, len(b.events))
	// Payloads remain immutable, so only the mutable outer slice needs copying.
	copy(events, b.events)
	return events, b.done, b.readErr
}

func (b *observedBody) feedLocked(chunk []byte) []jsonv1.RawMessage {
	var completed []jsonv1.RawMessage
	for _, c := range chunk {
		if b.skipLF {
			b.skipLF = false
			if c == '\n' {
				continue
			}
		}
		switch c {
		case '\r':
			if event := b.processLineLocked(); event != nil {
				completed = append(completed, event)
			}
			b.skipLF = true
		case '\n':
			if event := b.processLineLocked(); event != nil {
				completed = append(completed, event)
			}
		default:
			b.line = append(b.line, c)
		}
	}
	return completed
}

func (b *observedBody) processLineLocked() jsonv1.RawMessage {
	if !b.firstLine {
		b.line = bytes.TrimPrefix(b.line, []byte("\xef\xbb\xbf"))
		b.firstLine = true
	}
	if len(b.line) == 0 {
		var event jsonv1.RawMessage
		if len(b.dataLines) > 0 {
			data := strings.Join(b.dataLines, "\n")
			b.dataLines = b.dataLines[:0]
			if data == "[DONE]" {
				b.done = true
			} else {
				event = make(jsonv1.RawMessage, len(data))
				copy(event, data)
				b.events = append(b.events, event)
			}
		}
		b.line = b.line[:0]
		return event
	}

	line := string(b.line)
	b.line = b.line[:0]
	if strings.HasPrefix(line, ":") {
		return nil
	}
	field, value, found := strings.Cut(line, ":")
	if !found {
		field, value = line, ""
	} else {
		value = strings.TrimPrefix(value, " ")
	}
	if field == "data" {
		b.dataLines = append(b.dataLines, value)
	}
	return nil
}
