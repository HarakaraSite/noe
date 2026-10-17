package markdown

import (
	"strings"
	"testing"
)

func TestRenderJapaneseTableColumns(t *testing.T) {
	input := "| 項目 | 結果 |\n| --- | --- |\n| A | 成功 |\n| 日本語 | OK |"
	want := "| 項目   | 結果 |\n| ------ | ---- |\n| A      | 成功 |\n| 日本語 | OK   |"
	if got := Render(input, Options{Enabled: true, Width: 80}); got != want {
		t.Fatalf("Japanese table:\n%s\nwant:\n%s", got, want)
	}
}

func TestHardWrapCountsColumnsAndKeepsCombiningMarks(t *testing.T) {
	input := "日e\u0301本語"
	fragments := wrapRenderedHard(input, 3)
	if got := strings.Join(fragments, "|"); got != "日e\u0301|本|語" {
		t.Fatalf("wrapped fragments = %q", got)
	}
	// A single wide character must make progress even in a one-column cell.
	if got := strings.Join(wrapRenderedHard("日本", 1), ""); got != "日本" {
		t.Fatalf("narrow wrapping lost content: %q", got)
	}
}
