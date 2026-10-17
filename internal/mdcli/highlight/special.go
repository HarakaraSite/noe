package highlight

import (
	"strconv"
	"strings"
)

// Languages whose structure is carried by line shape rather than by tokens get
// their own small lexer instead of the generic scanner. Each may still delegate
// to the scanner for the parts that are ordinary code.

func init() {
	languages["diff"] = &Language{lex: lexDiff}

	languages["yaml"] = &Language{
		LineComments: []string{"#"},
		Quotes:       `"'`,
		Escape:       '\\',
		StartExtra:   "&*",
		VarPrefix:    "&*",
		Builtins:     words(`true false null yes no on off True False Null None ~`),
		lex:          lexYAML,
	}

	languages["toml"] = &Language{
		LineComments: []string{"#", ";"},
		Quotes:       `"'`,
		RawStrings:   []string{`"""`, `'''`},
		Escape:       '\\',
		PartExtra:    "-",
		Builtins:     words(`true false inf nan`),
		lex:          lexTOML,
	}

	languages["html"] = &Language{lex: lexHTML}

	languages["css"] = func() *Language {
		l := cFamily(
			`@charset @font-face @import @keyframes @layer @media @supports and from
			 important not only to`,
			`align-items animation background background-color border border-radius bottom
			 box-shadow box-sizing color content cursor display flex flex-direction
			 flex-wrap float font font-family font-size font-style font-weight gap grid
			 grid-template-columns grid-template-rows height justify-content left
			 letter-spacing line-height list-style margin margin-bottom margin-left
			 margin-right margin-top max-height max-width min-height min-width opacity
			 outline overflow padding padding-bottom padding-left padding-right
			 padding-top position right text-align text-decoration text-transform top
			 transform transition user-select vertical-align visibility white-space width
			 word-break z-index`,
		)
		l.PartExtra = "-"
		l.StartExtra = "@"
		return l
	}()
}

// lexDiff colors unified diffs by line prefix. Its phase state distinguishes
// file headers from body lines whose source happens to begin with ++ or --.
func lexDiff(s *State, line string) string {
	if style, _ := s.diffHeaders.headerStyle(line, s.palette); style != "" {
		return styled(style, line)
	}
	switch {
	case strings.HasPrefix(line, "+"):
		s.diffHeaders.markContent('+')
		return styled(s.palette.added, line)
	case strings.HasPrefix(line, "-"):
		s.diffHeaders.markContent('-')
		return styled(s.palette.removed, line)
	case strings.HasPrefix(line, " "):
		s.diffHeaders.markContent(' ')
	}
	return line
}

// diffLineState tracks whether a unified diff has reached file content. Once it
// has, +++ and --- are body lines rather than file headers. Hunk counts return
// the state to header mode between hunks or files; a git preamble also resets it.
// DiffState shares this classification with fenced diffs.
type diffLineState struct {
	content      bool
	countedHunk  bool
	oldRemaining int
	newRemaining int
}

func (s *diffLineState) markContent(prefix byte) {
	s.content = true
	if !s.countedHunk {
		return
	}
	switch prefix {
	case ' ':
		if s.oldRemaining > 0 {
			s.oldRemaining--
		}
		if s.newRemaining > 0 {
			s.newRemaining--
		}
	case '-':
		if s.oldRemaining > 0 {
			s.oldRemaining--
		}
	case '+':
		if s.newRemaining > 0 {
			s.newRemaining--
		}
	}
	if s.oldRemaining == 0 && s.newRemaining == 0 {
		s.content = false
		s.countedHunk = false
	}
}

// headerStyle returns the whole-line style for a unified-diff header or git
// metadata line, plus whether line begins a new file. It returns an empty style
// when line is diff content.
func (s *diffLineState) headerStyle(line string, p palette) (style string, fileStart bool) {
	switch {
	case strings.HasPrefix(line, "diff "):
		*s = diffLineState{}
		return p.comment, true
	case strings.HasPrefix(line, "@@"):
		s.content = true
		s.countedHunk = false
		s.oldRemaining, s.newRemaining = 0, 0
		if oldCount, newCount, ok := diffHunkCounts(line); ok {
			s.oldRemaining, s.newRemaining = oldCount, newCount
			s.countedHunk = true
			if oldCount == 0 && newCount == 0 {
				s.content = false
			}
		}
		return p.keyword, false
	case !s.content && diffFileHeader(line, "---"):
		return p.builtin, true
	case !s.content && diffFileHeader(line, "+++"):
		s.content = true
		return p.builtin, false
	case strings.HasPrefix(line, "index "),
		strings.HasPrefix(line, "new file"), strings.HasPrefix(line, "deleted file"),
		strings.HasPrefix(line, "old mode"), strings.HasPrefix(line, "new mode"),
		strings.HasPrefix(line, "similarity "), strings.HasPrefix(line, "rename "):
		return p.comment, false
	}
	return "", false
}

func diffFileHeader(line, marker string) bool {
	return strings.HasPrefix(line, marker+" ") || strings.HasPrefix(line, marker+"\t")
}

func diffHunkCounts(line string) (oldCount, newCount int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 4 || fields[0] != "@@" {
		return 0, 0, false
	}
	oldCount, oldOK := diffRangeCount(fields[1], '-')
	newCount, newOK := diffRangeCount(fields[2], '+')
	return oldCount, newCount, oldOK && newOK
}

func diffRangeCount(field string, sign byte) (int, bool) {
	if len(field) < 2 || field[0] != sign {
		return 0, false
	}
	rangeText := field[1:]
	startText, countText, hasCount := strings.Cut(rangeText, ",")
	start, err := strconv.Atoi(startText)
	if err != nil || start < 0 {
		return 0, false
	}
	if !hasCount {
		return 1, true
	}
	count, err := strconv.Atoi(countText)
	if err != nil || count < 0 {
		return 0, false
	}
	return count, true
}

// lexYAML colors mapping keys distinctly and hands the value to the scanner.
func lexYAML(s *State, line string) string {
	var b strings.Builder
	b.Grow(len(line) + 16)

	i := skipSpace(line, 0)
	// Sequence markers, which may nest on one line: "- - item".
	for i < len(line) && line[i] == '-' && (i+1 == len(line) || isSpace(line[i+1])) {
		i = skipSpace(line, i+1)
	}
	b.WriteString(line[:i])

	rest := line[i:]
	if k := yamlKeyEnd(rest); k > 0 {
		span(&b, s.palette.builtin, rest[:k])
		b.WriteByte(':')
		b.WriteString(s.scan(rest[k+1:]))
		return b.String()
	}
	b.WriteString(s.scan(rest))
	return b.String()
}

// yamlKeyEnd returns the offset of the colon ending a mapping key, or 0 when
// the line does not open one.
func yamlKeyEnd(s string) int {
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ':':
			if i+1 == len(s) || isSpace(s[i+1]) {
				return i
			}
		case '#':
			return 0
		}
	}
	return 0
}

// lexTOML colors [section] headers and hands everything else to the scanner.
func lexTOML(s *State, line string) string {
	trimmed := strings.TrimSpace(line)
	if s.mode == modeNormal && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
		return styled(s.palette.keyword, line)
	}
	return s.scan(line)
}

// lexHTML colors tags, attribute names and attribute values, leaving the text
// between tags plain.
func lexHTML(s *State, line string) string {
	var b strings.Builder
	b.Grow(len(line) + 16)

	i, done := s.resume(&b, line)
	if done {
		return b.String()
	}

	for i < len(line) {
		if strings.HasPrefix(line[i:], "<!--") {
			end, ok := closes(line, i+len("<!--"), "-->")
			if !ok {
				span(&b, s.palette.comment, line[i:])
				s.suspend(modeComment, "-->")
				return b.String()
			}
			span(&b, s.palette.comment, line[i:end])
			i = end
			continue
		}
		if line[i] == '<' && i+1 < len(line) && isTagStart(line[i+1]) {
			i = htmlTag(&b, line, i, s.palette)
			continue
		}
		b.WriteByte(line[i])
		i++
	}
	return b.String()
}

func isTagStart(c byte) bool {
	return isLetter(c) || c == '/' || c == '!' || c == '?'
}

// htmlTag styles the tag opening at line[i] and returns the offset past it.
func htmlTag(b *strings.Builder, line string, i int, p palette) int {
	b.WriteByte('<')
	j := i + 1
	if line[j] == '/' || line[j] == '!' || line[j] == '?' {
		b.WriteByte(line[j])
		j++
	}
	name := j
	for j < len(line) && isNameByte(line[j]) {
		j++
	}
	span(b, p.keyword, line[name:j])

	for j < len(line) && line[j] != '>' {
		switch c := line[j]; {
		case c == '"' || c == '\'':
			end, ok := scanQuoted(line, j, 0)
			if !ok {
				end = len(line)
			}
			span(b, p.string, line[j:end])
			j = end
		case isLetter(c) || c == '_':
			k := j
			for k < len(line) && isNameByte(line[k]) {
				k++
			}
			span(b, p.builtin, line[j:k])
			j = k
		default:
			b.WriteByte(c)
			j++
		}
	}
	if j < len(line) {
		b.WriteByte('>')
		j++
	}
	return j
}

// isNameByte reports whether c may appear in a tag or attribute name.
func isNameByte(c byte) bool {
	return isLetter(c) || isDigit(c) || strings.IndexByte("-_:.@", c) >= 0
}

func skipSpace(s string, i int) int {
	for i < len(s) && isSpace(s[i]) {
		i++
	}
	return i
}
