package agentexec

import (
	"bytes"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	progressLineBytes = 512
	progressInterval  = time.Second
)

// progressOutput samples complete lines independently of the result capture.
// Callers serialize sample calls with the stream's capture lock.
type progressOutput struct {
	emit    func(string)
	next    time.Time
	line    []byte
	clipped bool
	skip    bool
}

func (o *progressOutput) sample(data []byte, now time.Time) string {
	if len(data) == 0 {
		return ""
	}
	if now.Before(o.next) {
		o.skip = data[len(data)-1] != '\n'
		return ""
	}
	if o.skip {
		_, rest, complete := bytes.Cut(data, []byte{'\n'})
		o.skip = !complete
		data = rest
	}
	for len(data) > 0 {
		part, rest, complete := bytes.Cut(data, []byte{'\n'})
		size := min(len(part), progressLineBytes-len(o.line))
		o.line = append(o.line, part[:size]...)
		o.clipped = o.clipped || size < len(part)
		if !complete {
			return ""
		}
		line := o.line
		if o.clipped {
			// Keep a split UTF-8 rune out of the truncated line.
			for i := len(line) - 1; i >= len(line)-utf8.UTFMax; i-- {
				if utf8.RuneStart(line[i]) {
					if !utf8.FullRune(line[i:]) {
						line = line[:i]
					}
					break
				}
			}
		}
		text := strings.TrimSpace(outputText(line))
		if o.clipped {
			text += "…"
		}
		o.line = o.line[:0]
		o.clipped = false
		if text != "" {
			o.next = now.Add(progressInterval)
			o.skip = len(rest) != 0 && rest[len(rest)-1] != '\n'
			return text
		}
		data = rest
	}
	return ""
}
