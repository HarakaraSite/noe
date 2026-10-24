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
	maxBytes  int
	head      []byte
	tail      []byte
	tailStart int
	total     int64
}

func (c *capturedOutput) limit() int {
	if c.maxBytes > 0 {
		return c.maxBytes
	}
	return capturedOutputLimit
}

func (c *capturedOutput) Write(p []byte) (int, error) {
	n := len(p)
	c.total += int64(n)
	headLimit, tailLimit := (c.limit()+1)/2, c.limit()/2
	if len(c.head) < headLimit {
		count := min(len(p), headLimit-len(c.head))
		if c.head == nil && count > 0 {
			c.head = make([]byte, 0, headLimit)
		}
		c.head = append(c.head, p[:count]...)
		p = p[count:]
	}
	if len(p) == 0 {
		return n, nil
	}
	if c.tail == nil {
		c.tail = make([]byte, 0, tailLimit)
	}
	if len(p) >= tailLimit {
		c.tail = c.tail[:tailLimit]
		copy(c.tail, p[len(p)-tailLimit:])
		c.tailStart = 0
		return n, nil
	}
	if len(c.tail) < tailLimit {
		count := min(len(p), tailLimit-len(c.tail))
		c.tail = append(c.tail, p[:count]...)
		p = p[count:]
	}
	for len(p) > 0 {
		count := copy(c.tail[c.tailStart:], p)
		c.tailStart = (c.tailStart + count) % tailLimit
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
	if c.total <= int64(c.limit()) {
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
