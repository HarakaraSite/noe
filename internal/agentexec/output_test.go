package agentexec

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCapturedOutputUsesRequestedBudget(t *testing.T) {
	for _, limit := range []int{1024, 1025, 65536, 131072, 262144} {
		for _, content := range []string{strings.Repeat("x", limit-1), strings.Repeat("x", limit), "HEAD" + strings.Repeat("日本語😀", limit) + "TAIL"} {
			capture := capturedOutput{maxBytes: limit}
			// Different chunk boundaries exercise both append and ring-buffer paths.
			for start := 0; start < len(content); start += 173 {
				chunk := []byte(content[start:min(start+173, len(content))])
				if n, err := capture.Write(chunk); n != len(chunk) || err != nil {
					t.Fatalf("write=%d,%v", n, err)
				}
			}
			got, omitted := capture.result()
			if len(content) <= limit {
				if got != content || omitted != 0 {
					t.Fatalf("limit=%d changed fitting output", limit)
				}
				continue
			}
			marker := fmt.Sprintf("\n[NOE: output truncated; %d bytes omitted]\n", omitted)
			if !utf8.ValidString(got) || !strings.HasPrefix(got, "HEAD") || !strings.HasSuffix(got, "TAIL") || len(got)-len(marker) > limit || int64(len(got)-len(marker))+omitted != int64(len(content)) {
				t.Fatalf("limit=%d retained=%d omitted=%d", limit, len(got), omitted)
			}
		}
	}
}

func TestCapturedOutputReplacesInvalidUTF8AtTextBoundary(t *testing.T) {
	capture := capturedOutput{maxBytes: 32}
	for _, b := range []byte{'a', 0xff, 0xc3, 0xa9, 'b', 0xe2, 0x82} {
		if n, err := capture.Write([]byte{b}); n != 1 || err != nil {
			t.Fatalf("write=%d,%v", n, err)
		}
	}
	got, omitted := capture.result()
	if want := "a\uFFFDéb\uFFFD\uFFFD"; got != want || omitted != 0 || !utf8.ValidString(got) {
		t.Fatalf("result=%q omitted=%d want=%q omitted=0", got, omitted, want)
	}
}

func TestCapturedOutputKeepsRuneAcrossUntruncatedHeadTailBoundary(t *testing.T) {
	capture := capturedOutput{maxBytes: 32}
	want := strings.Repeat("a", 15) + "é" + "z"
	if n, err := capture.Write([]byte(want)); n != len(want) || err != nil {
		t.Fatalf("write=%d,%v", n, err)
	}
	got, omitted := capture.result()
	if got != want || omitted != 0 || !utf8.ValidString(got) {
		t.Fatalf("result=%q omitted=%d want=%q omitted=0", got, omitted, want)
	}
}
