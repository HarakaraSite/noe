package replui

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCompletionPreservesUnicodeAndCursorTail(t *testing.T) {
	e, out := newTestEditor(80)
	e.complete = func(line []rune, pos int) ([]string, int) {
		return []string{"日本", "日曜"}, 0
	}
	if got := edit(t, e, "日\t\r"); got[0] != "日" || !utf8.ValidString(out.String()) {
		t.Fatalf("partial UTF-8 completion: lines=%q output=%q", got, out.String())
	}
	e, _ = newTestEditor(80)
	e.complete = func(line []rune, pos int) ([]string, int) {
		return []string{"商品.税額"}, 4 // after 'log('
	}
	got := edit(t, e, "log(商品.税)\x1b[D\t\r")
	if got[0] != "log(商品.税額)" {
		t.Fatalf("completion changed surrounding source: %q", got)
	}
}

func TestHistorySearchAndEditing(t *testing.T) {
	for _, tc := range []struct{ name, keys, want string }{
		{"reverse", "\x12日本\r", "日本の履歴"},
		{"older and forward", "\x12match\x12\x13\r", "new match"},
		{"backspace query", "\x12matcx\x7fh\r", "new match"},
		{"unmatched query", "\x12matchNOTPRESENT\r", ""},
		{"backspace recovers match", "\x12matchx\x7f\r", "new match"},
		{"edit selected", "\x12match\x01edited \r", "edited new match"},
		{"cancel restores pending", "pending\x12match\x07!\r", "pending!"},
		{"interrupt", "\x12match\x03", "new match<^C>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, out := newTestEditor(20)
			for _, line := range []string{"old match", "日本の履歴", "new match"} {
				e.addHistory(line)
			}
			got := edit(t, e, tc.keys)
			if got[0] != tc.want || !strings.Contains(out.String(), "(reverse-i-search)") {
				t.Fatalf("lines=%q output=%q", got, out.String())
			}
		})
	}
}

func TestMultilineHistoryRendersEscapesAndReturnsOriginalSource(t *testing.T) {
	const source = "(() => {\n\treturn 42;\n})()"
	e, out := newTestEditor(80)
	e.addHistory(source)
	got := edit(t, e, "\x1b[A\r")
	if !reflect.DeepEqual(got, []string{source, "<EOF>"}) {
		t.Fatalf("history source changed: %q", got)
	}
	if !strings.Contains(out.String(), `(() => {\n\treturn 42;\n})()`) {
		t.Fatalf("multiline history was not rendered visibly: %q", out.String())
	}
}

func TestEditorPropagatesReadFailure(t *testing.T) {
	want := errors.New("input failed")
	e := newEditor(failedReader{want}, io.Discard, nil)
	if _, err := e.readLine("> "); !errors.Is(err, want) {
		t.Fatalf("read error=%v, want %v", err, want)
	}
}

func TestPromptControlsInsideEscapeSequences(t *testing.T) {
	for _, prefix := range []string{"\x1b", "\x1b[", "\x1b[1;", "\x1bO"} {
		t.Run(prefix, func(t *testing.T) {
			e, _ := newTestEditor(80)
			if got := edit(t, e, "discarded"+prefix+"\x03next\r"); !reflect.DeepEqual(got, []string{"discarded<^C>", "next", "<EOF>"}) {
				t.Fatalf("Ctrl+C swallowed by escape: %q", got)
			}
			e, _ = newTestEditor(80)
			if got := edit(t, e, prefix+"\x04"); !reflect.DeepEqual(got, []string{"<EOF>"}) {
				t.Fatalf("Ctrl+D swallowed by escape: %q", got)
			}
		})
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }
