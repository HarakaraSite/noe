// Package highlight applies terminal syntax colors to source code using only
// the standard library.
//
// It is a scanner, not a parser: it recognizes comments, strings, numbers,
// keywords and call sites well enough to make a code block readable, and
// degrades to plain text on anything it does not understand. Mis-coloring a
// token is acceptable; corrupting the text is not.
//
// Highlighting is strictly additive. A returned line is the input line with
// ANSI escape sequences inserted around byte ranges, so stripping the escapes
// always yields the original bytes back and code stays copy-pasteable.
//
// Input arrives one line at a time so it can be highlighted as it streams, so
// constructs that span lines (block comments, raw strings) are carried across
// calls in State.
package highlight

import (
	"strings"
	"unicode/utf8"
)

const styleReset = "\x1b[0m"

// maxCharLiteral bounds how far a single-quote scan will look for its closing
// quote. Beyond this the quote is treated as punctuation, which keeps Rust
// lifetimes ('a) and apostrophes in shell comments from staining the line.
const maxCharLiteral = 12

// mode records what a line left open for the next one.
type mode uint8

const (
	modeNormal mode = iota
	modeComment
	modeString
)

// State highlights the lines of a single code block. The zero value is not
// useful; call New. A nil *State is valid and highlights nothing, so callers
// can hold one unconditionally.
type State struct {
	lang    *Language
	palette palette
	mode    mode
	// end is the delimiter that closes the construct left open by the
	// previous line.
	end         string
	diffHeaders diffLineState
}

// New returns a dark-theme highlighter for a fenced code block's info string,
// or nil when the language is unknown or absent.
func New(info string) *State {
	return NewWithTheme(info, ThemeDark)
}

// NewWithTheme returns a highlighter using theme for a fenced code block's info
// string, or nil when the language is unknown or absent.
func NewWithTheme(info string, theme Theme) *State {
	lang, ok := Lookup(info)
	if !ok {
		return nil
	}
	return &State{lang: lang, palette: paletteFor(theme)}
}

// Line returns line with ANSI styling applied, advancing any multi-line state.
func (s *State) Line(line string) string {
	if s == nil || line == "" {
		return line
	}
	if s.lang.lex != nil {
		return s.lang.lex(s, line)
	}
	return s.scan(line)
}

// scan is the generic tokenizer used by every table-driven language.
func (s *State) scan(line string) string {
	var b strings.Builder
	b.Grow(len(line) + 16)

	i, done := s.resume(&b, line)
	if done {
		return b.String()
	}

	l := s.lang
	for i < len(line) {
		c := line[i]

		if l.BlockOpen != "" && strings.HasPrefix(line[i:], l.BlockOpen) {
			end, ok := closes(line, i+len(l.BlockOpen), l.BlockClose)
			if !ok {
				span(&b, s.palette.comment, line[i:])
				s.suspend(modeComment, l.BlockClose)
				return b.String()
			}
			span(&b, s.palette.comment, line[i:end])
			i = end
			continue
		}

		if l.lineCommentAt(line, i) {
			span(&b, s.palette.comment, line[i:])
			return b.String()
		}

		if d := l.rawStringAt(line, i); d != "" {
			end, ok := closes(line, i+len(d), d)
			if !ok {
				span(&b, s.palette.string, line[i:])
				s.suspend(modeString, d)
				return b.String()
			}
			span(&b, s.palette.string, line[i:end])
			i = end
			continue
		}

		if strings.IndexByte(l.Chars, c) >= 0 {
			if end, ok := scanChar(line, i, l.Escape); ok {
				span(&b, s.palette.string, line[i:end])
				i = end
				continue
			}
			// Not a character literal, so it is punctuation: a Rust lifetime,
			// a C++ digit separator, an apostrophe in prose.
			b.WriteByte(c)
			i++
			continue
		}

		if strings.IndexByte(l.Quotes, c) >= 0 {
			if end, ok := scanQuoted(line, i, l.Escape); ok {
				span(&b, s.palette.string, line[i:end])
				i = end
				continue
			}
			// An unterminated quote that is not plausibly a string: emit it as
			// punctuation and keep scanning the rest of the line normally.
			b.WriteByte(c)
			i++
			continue
		}

		if l.isIdentStart(c) {
			j := i + 1
			for j < len(line) && l.isIdentPart(line[j]) {
				j++
			}
			span(&b, l.classify(line, i, j, s.palette), line[i:j])
			i = j
			continue
		}

		if isDigit(c) {
			j := scanNumber(line, i)
			span(&b, s.palette.number, line[i:j])
			i = j
			continue
		}

		b.WriteByte(c)
		i++
	}
	return b.String()
}

// resume closes out a construct carried over from the previous line. It
// reports the offset to continue scanning from, and whether the whole line was
// consumed.
func (s *State) resume(b *strings.Builder, line string) (int, bool) {
	if s.mode == modeNormal {
		return 0, false
	}
	style := s.palette.comment
	if s.mode == modeString {
		style = s.palette.string
	}
	end, ok := closes(line, 0, s.end)
	if !ok {
		span(b, style, line)
		return len(line), true
	}
	span(b, style, line[:end])
	s.mode, s.end = modeNormal, ""
	return end, false
}

func (s *State) suspend(m mode, end string) {
	s.mode, s.end = m, end
}

// classify picks the style for the identifier at line[i:j].
func (l *Language) classify(line string, i, j int, p palette) string {
	word := line[i:j]
	if l.VarPrefix != "" && strings.IndexByte(l.VarPrefix, line[i]) >= 0 {
		return p.builtin
	}
	key := word
	if l.Fold {
		key = strings.ToLower(word)
	}
	switch {
	case l.Keywords[key]:
		return p.keyword
	case l.Builtins[key]:
		return p.builtin
	case j < len(line) && line[j] == '(':
		return p.function
	}
	return ""
}

// closes finds the delimiter starting at or after from and returns the offset
// just past it.
func closes(line string, from int, delim string) (int, bool) {
	if delim == "" || from > len(line) {
		return 0, false
	}
	j := strings.Index(line[from:], delim)
	if j < 0 {
		return 0, false
	}
	return from + j + len(delim), true
}

// scanQuoted consumes the quoted run starting at line[i]. It reports failure
// for an unterminated single-quoted run so apostrophes and Rust lifetimes are
// not mistaken for strings; an unterminated double quote is assumed to be a
// real string that continues past the visible line.
func scanQuoted(line string, i int, esc byte) (int, bool) {
	q := line[i]
	for j := i + 1; j < len(line); j++ {
		if esc != 0 && line[j] == esc {
			j++
			continue
		}
		if line[j] == q {
			return j + 1, true
		}
	}
	if q == '\'' && len(line)-i > maxCharLiteral {
		return 0, false
	}
	return len(line), q != '\''
}

// scanChar consumes a character literal — 'a', '\n', '\u{1F600}' — and reports
// failure on anything else. Requiring the literal to be well formed is what
// keeps a Rust lifetime ('a) or a quote in prose from opening a string that
// runs to the end of the line.
func scanChar(line string, i int, esc byte) (int, bool) {
	j := i + 1
	if j >= len(line) {
		return 0, false
	}
	if esc != 0 && line[j] == esc {
		for j++; j < len(line) && j-i <= maxCharLiteral; j++ {
			if line[j] == line[i] {
				return j + 1, true
			}
		}
		return 0, false
	}
	_, size := utf8.DecodeRuneInString(line[j:])
	if j += size; j < len(line) && line[j] == line[i] {
		return j + 1, true
	}
	return 0, false
}

// scanNumber consumes a numeric literal: an optional base prefix, digits with
// separators, a fraction, an exponent, and any trailing unit or type suffix
// (10px, 3.0f, 5i32).
func scanNumber(line string, i int) int {
	j := i
	if line[j] == '0' && j+1 < len(line) && strings.IndexByte("xXbBoO", line[j+1]) >= 0 {
		j += 2
		for j < len(line) && (isHexDigit(line[j]) || line[j] == '_') {
			j++
		}
		return j
	}
	for j < len(line) && (isDigit(line[j]) || line[j] == '_') {
		j++
	}
	if j+1 < len(line) && line[j] == '.' && isDigit(line[j+1]) {
		j++
		for j < len(line) && (isDigit(line[j]) || line[j] == '_') {
			j++
		}
	}
	if j < len(line) && (line[j] == 'e' || line[j] == 'E') {
		k := j + 1
		if k < len(line) && (line[k] == '+' || line[k] == '-') {
			k++
		}
		if k < len(line) && isDigit(line[k]) {
			for k < len(line) && isDigit(line[k]) {
				k++
			}
			j = k
		}
	}
	for j < len(line) && (isLetter(line[j]) || line[j] == '_') {
		j++
	}
	return j
}

// span writes text wrapped in style. An empty style writes the text plain.
func span(b *strings.Builder, style, text string) {
	b.WriteString(styled(style, text))
}

// styled wraps text in a style and its reset. Empty text and an empty style
// both pass through untouched, so no escape is ever emitted around nothing.
func styled(style, text string) string {
	if text == "" || style == "" {
		return text
	}
	return style + text + styleReset
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isLetter(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isHexDigit(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }
