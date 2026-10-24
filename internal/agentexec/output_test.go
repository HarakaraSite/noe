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
