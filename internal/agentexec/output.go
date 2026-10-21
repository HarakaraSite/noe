package agentexec

import (
	"fmt"
	"unicode/utf8"
)

// Captured data is bounded per stream, independently of how much the child
// writes. Continue accepting every write so a verbose child never blocks just
// because its retained output is full. Each collector owns its own instance.
const capturedOutputLimit = 64 * 1024

type capturedOutput struct {
	head      []byte
	tail      []byte
	tailStart int
	total     int64
}

func (c *capturedOutput) Write(p []byte) (int, error) {
	n := len(p)
	c.total += int64(n)
	const half = capturedOutputLimit / 2
	if len(c.head) < half {
		count := min(len(p), half-len(c.head))
		if c.head == nil && count > 0 {
			c.head = make([]byte, 0, half)
		}
		c.head = append(c.head, p[:count]...)
		p = p[count:]
	}
	if len(p) == 0 {
		return n, nil
	}
	if c.tail == nil {
		c.tail = make([]byte, 0, half)
	}
	if len(p) >= half {
		c.tail = c.tail[:half]
		copy(c.tail, p[len(p)-half:])
		c.tailStart = 0
		return n, nil
	}
	if len(c.tail) < half {
		count := min(len(p), half-len(c.tail))
		c.tail = append(c.tail, p[:count]...)
		p = p[count:]
	}
	for len(p) > 0 {
		count := copy(c.tail[c.tailStart:], p)
		c.tailStart = (c.tailStart + count) % half
		p = p[count:]
	}
	return n, nil
}

// result is called only after the stream collector finishes. The marker is
// additional to the retained data budget. For valid UTF-8, avoid introducing
// broken characters at either side of the removed middle section.
func (c *capturedOutput) result() (string, int64) {
	head := c.head
	tail := make([]byte, 0, len(c.tail))
	tail = append(tail, c.tail[c.tailStart:]...)
	tail = append(tail, c.tail[:c.tailStart]...)
	if c.total <= capturedOutputLimit {
		return string(head) + string(tail), 0
	}
	for i := len(head) - 1; i >= max(0, len(head)-utf8.UTFMax); i-- {
		if utf8.RuneStart(head[i]) {
			if !utf8.FullRune(head[i:]) {
				head = head[:i]
			}
			break
		}
	}
	for len(tail) > 0 && !utf8.RuneStart(tail[0]) {
		tail = tail[1:]
	}
	omitted := c.total - int64(len(head)+len(tail))
	return string(head) + fmt.Sprintf("\n[NOE: output truncated; %d bytes omitted]\n", omitted) + string(tail), omitted
}
