package agentexec

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestProgressOutputSamplesCompleteBoundedLines(t *testing.T) {
	var progress progressOutput
	start := time.Time{}
	for _, check := range []struct {
		offset time.Duration
		write  string
		want   string
	}{
		{0, "part", ""},
		{0, "ial\r\nskipped", "partial"},
		{progressInterval / 2, " tail", ""},
		{progressInterval, " suffix\n\nsecond\nignored\n", "second"},
		{2 * progressInterval, strings.Repeat("x", progressLineBytes-1) + "é", ""},
		{3 * progressInterval, "\n", strings.Repeat("x", progressLineBytes-1) + "…"},
	} {
		got := progress.sample([]byte(check.write), start.Add(check.offset))
		if got != check.want || !utf8.ValidString(got) {
			t.Fatalf("sample at %s = %q, want %q", check.offset, got, check.want)
		}
	}
}
