package provider

import (
	"bytes"
	jsonv1 "encoding/json"
	"io"
	"strings"
	"sync"
)

// observedBody lets the SDK consume its own EventStream while independently
// assembling complete SSE events and recording the underlying body's read
// errors. The SDK's EventStream intentionally stops reading at [DONE].
type observedBody struct {
	source  io.ReadCloser
	observe func(StreamEvent)
	extract func(jsonv1.RawMessage) []StreamEvent

	mu         sync.Mutex
	line       []byte
	skipLF     bool
	firstLine  bool
	dataLines  []string
	chat       *streamAccumulator
	native     *responsesAccumulator
	consumeErr error
	done       bool
	readErr    error

	closeOnce sync.Once
	closeErr  error
}

func newObservedBody(source io.ReadCloser) *observedBody {
	return &observedBody{source: source, chat: newStreamAccumulator()}
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

func (b *observedBody) snapshot() (done bool, readErr, consumeErr error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.done, b.readErr, b.consumeErr
}

func (b *observedBody) response() (Response, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.consumeErr != nil {
		return Response{}, b.consumeErr
	}
	if b.native != nil {
		return b.native.response()
	}
	return b.chat.response()
}

func (b *observedBody) nativeQuota() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.native != nil && b.native.quota
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
			if event := b.processLineLocked(); event != nil && b.observe != nil {
				completed = append(completed, event)
			}
			b.skipLF = true
		case '\n':
			if event := b.processLineLocked(); event != nil && b.observe != nil {
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
			clear(b.dataLines)
			b.dataLines = b.dataLines[:0]
			if data == "[DONE]" {
				b.done = true
			} else {
				event = make(jsonv1.RawMessage, len(data))
				copy(event, data)
				if b.native != nil {
					if err := b.native.consumeEvent(event); b.consumeErr == nil {
						b.consumeErr = err
					}
				} else if b.consumeErr == nil {
					b.consumeErr = b.chat.consumeEvent(event)
				}
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
