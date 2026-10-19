// Package markdown renders a small, terminal-friendly subset of Markdown using
// only the standard library. It is intentionally not a CommonMark parser; it
// focuses on the model-output shapes that improve terminal readability.
package markdown

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/mdcli/highlight"
)

// Styling stays inside the sixteen ANSI colors so it renders in the reader's
// own theme rather than imposing one. Cyan for code and links is the terminal
// convention (Codex, rich, glow all land there); yellow reads at under 3:1
// against most light backgrounds, which is why it is not used.
//
// Spans close with the specific "off" code for what they turned on rather than
// a blanket reset, so nesting survives: see style.
const (
	ansiBold          = "\x1b[1m"
	ansiBoldOff       = "\x1b[22m"
	ansiBoldUnderline = "\x1b[1;4m"
	ansiItalic        = "\x1b[3m"
	ansiItalicOff     = "\x1b[23m"
	ansiUnderline     = "\x1b[4m"
	ansiUnderlineOff  = "\x1b[24m"
	ansiLink          = "\x1b[36;4m"
	ansiCode          = "\x1b[36m"
	ansiColorOff      = "\x1b[39m"
	minTableRule      = 3
	codeFenceTick     = "```"
	codeFenceTilde    = "~~~"
)

// style records the attributes the surrounding context has already switched on.
// A nested span consults it before closing: turning bold off at the end of an
// inline span would also end the bold of the heading containing it, so any
// attribute the outer context still needs is left alone.
type style struct {
	bold      bool
	italic    bool
	underline bool
}

func (s style) boldOff() string {
	if s.bold {
		return ""
	}
	return ansiBoldOff
}

func (s style) italicOff() string {
	if s.italic {
		return ""
	}
	return ansiItalicOff
}

func (s style) underlineOff() string {
	if s.underline {
		return ""
	}
	return ansiUnderlineOff
}

// DefaultWidth is the fallback terminal width used by callers that want wrapping
// even when the terminal size cannot be determined.
const DefaultWidth = 80

// HorizontalRule is the fixed-width line used to render thematic breaks.
const HorizontalRule = "────────────────────"

// Options controls Markdown rendering.
type Options struct {
	// Enabled leaves text byte-for-byte unchanged when false.
	Enabled bool
	// ANSI applies terminal styling for emphasis and links. When false, supported
	// Markdown markers are still normalized or stripped.
	ANSI bool
	// ColorTheme selects syntax colors when ANSI is enabled. Its zero value is dark.
	ColorTheme highlight.Theme
	// Width enables visible-width wrapping for paragraphs, list item bodies, and
	// tables when positive.
	Width int
	// Prefix is prepended to each non-empty rendered line.
	Prefix string
}

// Render formats a complete text block.
func Render(text string, opts Options) string {
	if !opts.Enabled || text == "" {
		return text
	}
	stream := NewStream(opts)
	return stream.Write(text) + stream.Flush()
}

// Stream renders Markdown from incremental text deltas. Complete lines are
// emitted as soon as possible; tables are buffered until the table block ends so
// column widths can be calculated.
type Stream struct {
	opts        Options
	pending     string
	table       []tableLine
	inFence     bool
	fenceMarker string
	// code highlights the body of the open fence. It is nil for an unlabeled
	// fence, an unrecognized language, or when ANSI is off, and a nil
	// highlighter leaves lines untouched.
	code     *highlight.State
	lineOpen bool
}

// NewStream returns a new line-buffered renderer.
func NewStream(opts Options) *Stream {
	return &Stream{opts: opts}
}

// Write consumes a text delta and returns any display-ready text.
func (s *Stream) Write(text string) string {
	if text == "" {
		return ""
	}
	if !s.opts.Enabled {
		s.lineOpen = !strings.HasSuffix(text, "\n")
		return text
	}

	s.pending += text
	var out strings.Builder
	for {
		i := strings.IndexByte(s.pending, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimSuffix(s.pending[:i], "\r")
		s.pending = s.pending[i+1:]
		s.renderLine(&out, line, true)
	}
	return out.String()
}

// Flush renders any buffered incomplete line or pending table block.
func (s *Stream) Flush() string {
	if !s.opts.Enabled {
		return ""
	}
	var out strings.Builder
	if s.pending != "" {
		line := strings.TrimSuffix(s.pending, "\r")
		s.pending = ""
		s.renderLine(&out, line, false)
	}
	s.flushTable(&out)
	return out.String()
}

// LineOpen reports whether the last emitted display line lacks a trailing
// newline.
func (s *Stream) LineOpen() bool {
	return s.lineOpen
}

// AtLineBoundary reports whether external scrolling output can be inserted
// without flushing buffered source or splitting a physical output line. A
// complete buffered table/fence line remains a safe boundary.
func (s *Stream) AtLineBoundary() bool {
	return s.pending == "" && !s.lineOpen
}

// CloseLine tells the stream that the caller wrote an external newline after an
// open rendered line.
func (s *Stream) CloseLine() {
	s.lineOpen = false
}

func (s *Stream) renderLine(out *strings.Builder, line string, newline bool) {
	line = strings.TrimRight(strings.ReplaceAll(line, "\t", "    "), " \r")

	if s.inFence {
		s.flushTable(out)
		// The closing delimiter is not code, so it is written unstyled.
		if strings.HasPrefix(strings.TrimSpace(line), s.fenceMarker) {
			s.inFence = false
			s.fenceMarker = ""
			s.code = nil
			s.writeLine(out, s.opts.Prefix+"  "+line, newline)
			return
		}
		s.writeLine(out, s.opts.Prefix+"  "+s.code.Line(line), newline)
		return
	}

	if marker, info, ok := fenceMarker(line); ok {
		s.flushTable(out)
		s.inFence = true
		s.fenceMarker = marker
		if s.opts.ANSI {
			s.code = highlight.NewWithTheme(info, s.opts.ColorTheme)
		}
		s.writeLine(out, s.opts.Prefix+"  "+line, newline)
		return
	}

	if tableCandidate(line) {
		s.table = append(s.table, tableLine{text: line, newline: newline})
		return
	}

	s.flushTable(out)
	s.renderNonTableLine(out, line, newline)
}

func (s *Stream) renderNonTableLine(out *strings.Builder, line string, newline bool) {
	if strings.TrimSpace(line) == "" {
		s.writeLine(out, "", newline)
		return
	}
	if isHorizontalRule(line) {
		s.writeLine(out, s.opts.Prefix+HorizontalRule, newline)
		return
	}
	if rendered, ok := s.renderHeading(line); ok {
		s.writeLine(out, rendered, newline)
		return
	}
	if s.renderListItem(out, line, newline) {
		return
	}
	s.renderParagraph(out, line, newline)
}

func (s *Stream) renderHeading(line string) (string, bool) {
	leading, rest := splitLeadingWhitespace(line)
	if len(leading) > 3 {
		return "", false
	}
	n := 0
	for n < len(rest) && n < 6 && rest[n] == '#' {
		n++
	}
	if n == 0 || n < len(rest) && rest[n] != ' ' && rest[n] != '\t' {
		return "", false
	}
	// A heading is the outermost span on its line, so it closes unconditionally
	// rather than consulting an enclosing style.
	outer := style{bold: true, underline: n == 1}
	on, off := ansiBold, ansiBoldOff
	if n == 1 {
		on, off = ansiBoldUnderline, ansiUnderlineOff+ansiBoldOff
	}
	rendered := s.opts.Prefix + leading + s.renderInline(rest, outer)
	if s.opts.ANSI {
		rendered = on + rendered + off
	}
	return rendered, true
}

func (s *Stream) renderListItem(out *strings.Builder, line string, newline bool) bool {
	leading, rest := splitLeadingWhitespace(line)
	marker, body, ok := listMarker(rest)
	if !ok {
		return false
	}
	firstPrefix := s.opts.Prefix + leading + marker + " "
	nextPrefix := s.opts.Prefix + leading + strings.Repeat(" ", len(marker)+1)
	lines := wrapBody(body, firstPrefix, nextPrefix, s.opts.Width, s.opts.ANSI)
	s.writeLines(out, lines, newline)
	return true
}

func (s *Stream) renderParagraph(out *strings.Builder, line string, newline bool) {
	leading, rest := splitLeadingWhitespace(line)
	prefix := s.opts.Prefix + leading
	lines := wrapBody(rest, prefix, prefix, s.opts.Width, s.opts.ANSI)
	s.writeLines(out, lines, newline)
}

func (s *Stream) flushTable(out *strings.Builder) {
	if len(s.table) == 0 {
		return
	}
	lines := s.table
	s.table = nil
	if !validTable(lines) {
		for _, line := range lines {
			s.renderNonTableLine(out, line.text, line.newline)
		}
		return
	}
	for _, line := range s.formatTable(lines) {
		s.writeLine(out, line.text, line.newline)
	}
}

func (s *Stream) formatTable(lines []tableLine) []tableLine {
	aligns := parseAlignments(parseCells(lines[1].text))
	rawRows := [][]string{parseCells(lines[0].text)}
	for _, line := range lines[2:] {
		rawRows = append(rawRows, parseCells(line.text))
	}

	cols := len(aligns)
	for _, row := range rawRows {
		if len(row) > cols {
			cols = len(row)
		}
	}
	for len(aligns) < cols {
		aligns = append(aligns, alignLeft)
	}

	rows := make([][]string, len(rawRows))
	widths := make([]int, cols)
	for i, row := range rawRows {
		rows[i] = make([]string, cols)
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = s.renderInline(strings.TrimSpace(row[c]), style{})
			}
			rows[i][c] = cell
			if w := visibleLen(cell); w > widths[c] {
				widths[c] = w
			}
		}
	}
	for i := range widths {
		if widths[i] < minTableRule {
			widths[i] = minTableRule
		}
	}

	if s.opts.Width <= 0 || tableGridWidth(s.opts.Prefix, widths) <= s.opts.Width {
		return s.formatNaturalTable(rows, widths, aligns, lines)
	}

	contentBudget := s.opts.Width - visibleLen(s.opts.Prefix) - 1 - 3*cols
	if contentBudget < minTableRule*cols {
		return s.formatStackedTable(rows, lines)
	}
	return s.formatWrappedTable(rows, fitTableWidths(widths, contentBudget), aligns, lines)
}

// tableGridWidth includes the leading border and the two spaces plus trailing
// border that surround every cell.
func tableGridWidth(prefix string, widths []int) int {
	width := visibleLen(prefix) + 1
	for _, columnWidth := range widths {
		width += columnWidth + 3
	}
	return width
}

// fitTableWidths starts at the smallest valid separator width, then gives each
// still-growing column one cell at a time from left to right. Keeping the loop
// round-robin makes one-cell remainders deterministic.
func fitTableWidths(natural []int, contentBudget int) []int {
	widths := make([]int, len(natural))
	for i := range widths {
		widths[i] = minTableRule
	}
	remaining := contentBudget - minTableRule*len(widths)
	for remaining > 0 {
		grew := false
		for i := range widths {
			if remaining == 0 {
				break
			}
			if widths[i] >= natural[i] {
				continue
			}
			widths[i]++
			remaining--
			grew = true
		}
		if !grew {
			break
		}
	}
	return widths
}

// formatNaturalTable intentionally retains the original table formatter. The
// width-aware paths below must not change fitting tables' bytes, including ANSI
// escape ordering and the source line-ending state.
func (s *Stream) formatNaturalTable(rows [][]string, widths []int, aligns []alignment, lines []tableLine) []tableLine {
	out := make([]tableLine, 0, len(lines))
	out = append(out, tableLine{text: s.opts.Prefix + formatTableRow(rows[0], widths, aligns), newline: lines[0].newline})
	out = append(out, tableLine{text: s.opts.Prefix + formatTableRule(widths, aligns), newline: lines[1].newline})
	for i := 1; i < len(rows); i++ {
		newline := true
		if i+1 < len(lines) {
			newline = lines[i+1].newline
		}
		out = append(out, tableLine{text: s.opts.Prefix + formatTableRow(rows[i], widths, aligns), newline: newline})
	}
	return out
}

func (s *Stream) formatWrappedTable(rows [][]string, widths []int, aligns []alignment, lines []tableLine) []tableLine {
	out := make([]tableLine, 0, len(lines))
	s.appendWrappedTableRow(&out, rows[0], widths, aligns, lines[0].newline)
	out = append(out, tableLine{text: s.opts.Prefix + formatTableRule(widths, aligns), newline: lines[1].newline})
	for i := 1; i < len(rows); i++ {
		newline := true
		if i+1 < len(lines) {
			newline = lines[i+1].newline
		}
		s.appendWrappedTableRow(&out, rows[i], widths, aligns, newline)
	}
	return out
}

func (s *Stream) appendWrappedTableRow(out *[]tableLine, row []string, widths []int, aligns []alignment, newline bool) {
	fragments := make([][]string, len(widths))
	height := 1
	for c, width := range widths {
		cell := ""
		if c < len(row) {
			cell = row[c]
		}
		fragments[c] = wrapRenderedHard(cell, width)
		if len(fragments[c]) > height {
			height = len(fragments[c])
		}
	}
	for line := 0; line < height; line++ {
		physical := make([]string, len(widths))
		for c := range physical {
			if line < len(fragments[c]) {
				physical[c] = fragments[c][line]
			}
		}
		*out = append(*out, tableLine{
			text:    s.opts.Prefix + formatTableRow(physical, widths, aligns),
			newline: line+1 < height || newline,
		})
	}
}

func (s *Stream) formatStackedTable(rows [][]string, lines []tableLine) []tableLine {
	labels := make([]string, len(rows[0]))
	for c, header := range rows[0] {
		if visibleLen(header) == 0 {
			labels[c] = fmt.Sprintf("Column %d", c+1)
			continue
		}
		labels[c] = header
	}

	available := s.opts.Width - visibleLen(s.opts.Prefix)
	if available < 1 {
		available = 1
	}
	var out []tableLine
	if len(rows) == 1 {
		for c, label := range labels {
			newline := c+1 < len(labels) || lines[len(lines)-1].newline
			s.appendStackedTableText(&out, "- "+label, available, newline)
		}
		return out
	}

	for r := 1; r < len(rows); r++ {
		for c, label := range labels {
			field := label + ":"
			if rows[r][c] != "" {
				field += " " + rows[r][c]
			}
			newline := true
			if r+1 == len(rows) && c+1 == len(labels) {
				newline = lines[r+1].newline
			}
			s.appendStackedTableText(&out, field, available, newline)
		}
		if r+1 < len(rows) {
			out = append(out, tableLine{newline: true})
		}
	}
	return out
}

func (s *Stream) appendStackedTableText(out *[]tableLine, text string, available int, newline bool) {
	continuation := ""
	contentWidth := available
	if available > 2 {
		continuation = "  "
		contentWidth -= visibleLen(continuation)
	}
	fragments := wrapRenderedHard(text, contentWidth)
	for i, fragment := range fragments {
		line := s.opts.Prefix
		if i > 0 {
			line += continuation
		}
		line += fragment
		*out = append(*out, tableLine{text: line, newline: i+1 < len(fragments) || newline})
	}
}

func (s *Stream) writeLines(out *strings.Builder, lines []string, newline bool) {
	for i, line := range lines {
		s.writeLine(out, line, newline || i+1 < len(lines))
	}
}

func (s *Stream) writeLine(out *strings.Builder, line string, newline bool) {
	out.WriteString(line)
	if newline {
		out.WriteByte('\n')
		s.lineOpen = false
		return
	}
	s.lineOpen = line != ""
}

func (s *Stream) renderInline(text string, outer style) string {
	return renderInline(text, s.opts.ANSI, outer)
}

// renderInline styles the spans within a line. outer describes the attributes
// already active around text so nested spans can close without cancelling them.
func renderInline(text string, ansi bool, outer style) string {
	var out strings.Builder
	for i := 0; i < len(text); {
		if text[i] == '`' {
			if end := strings.IndexByte(text[i+1:], '`'); end >= 0 {
				end += i + 1
				inner := text[i+1 : end]
				if ansi {
					out.WriteString(ansiCode + inner + ansiColorOff)
				} else {
					out.WriteString(inner)
				}
				i = end + 1
				continue
			}
		}
		if label, url, n, ok := markdownLink(text[i:]); ok {
			renderedURL := renderURL(url, ansi, outer)
			if strings.TrimSpace(label) == "" || strings.TrimSpace(label) == url {
				out.WriteString(renderedURL)
			} else {
				out.WriteString(renderInline(label, ansi, outer))
				out.WriteString(" <")
				out.WriteString(renderedURL)
				out.WriteByte('>')
			}
			i += n
			continue
		}
		if url, trailing, n, ok := rawURL(text[i:]); ok {
			out.WriteString(renderURL(url, ansi, outer))
			out.WriteString(trailing)
			i += n
			continue
		}
		if rendered, n, ok := emphasis(text, i, ansi, outer); ok {
			out.WriteString(rendered)
			i += n
			continue
		}
		out.WriteByte(text[i])
		i++
	}
	return out.String()
}

func renderURL(url string, ansi bool, outer style) string {
	if !ansi {
		return url
	}
	return ansiLink + url + ansiColorOff + outer.underlineOff()
}

func markdownLink(s string) (label, url string, n int, ok bool) {
	if !strings.HasPrefix(s, "[") {
		return "", "", 0, false
	}
	closeLabel := strings.Index(s, "](")
	if closeLabel <= 1 {
		return "", "", 0, false
	}
	closeURL := strings.IndexByte(s[closeLabel+2:], ')')
	if closeURL < 0 {
		return "", "", 0, false
	}
	closeURL += closeLabel + 2
	url = strings.TrimSpace(s[closeLabel+2 : closeURL])
	if !isURLStart(url) {
		return "", "", 0, false
	}
	return s[1:closeLabel], url, closeURL + 1, true
}

func rawURL(s string) (url, trailing string, n int, ok bool) {
	if !isURLStart(s) {
		return "", "", 0, false
	}
	end := 0
	for end < len(s) {
		r := rune(s[end])
		if unicode.IsSpace(r) || strings.ContainsRune("<>()", r) {
			break
		}
		end++
	}
	url = s[:end]
	trimmed := strings.TrimRight(url, ".,;:!?")
	trailing = url[len(trimmed):]
	return trimmed, trailing, end, trimmed != ""
}

func isURLStart(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "mailto:")
}

func emphasis(s string, i int, ansi bool, outer style) (string, int, bool) {
	for _, marker := range []string{"***", "___", "**", "__", "*", "_"} {
		if !strings.HasPrefix(s[i:], marker) || !emphasisBoundaryStart(s, i, marker) {
			continue
		}
		start := i + len(marker)
		endRel := strings.Index(s[start:], marker)
		if endRel < 0 {
			continue
		}
		end := start + endRel
		if end == start || !emphasisBoundaryEnd(s, end, marker) {
			continue
		}
		inner, on, off := outer, "", ""
		switch marker {
		case "***", "___":
			inner.bold, inner.italic = true, true
			on, off = ansiBold+ansiItalic, outer.italicOff()+outer.boldOff()
		case "**", "__":
			inner.bold = true
			on, off = ansiBold, outer.boldOff()
		default:
			inner.italic = true
			on, off = ansiItalic, outer.italicOff()
		}
		content := renderInline(s[start:end], ansi, inner)
		if ansi {
			content = on + content + off
		}
		return content, end + len(marker) - i, true
	}
	return "", 0, false
}

func emphasisBoundaryStart(s string, i int, marker string) bool {
	if marker[0] == '*' {
		return i+len(marker) < len(s) && !unicode.IsSpace(rune(s[i+len(marker)]))
	}
	if i > 0 && isAlphaNum(rune(s[i-1])) {
		return false
	}
	return i+len(marker) < len(s) && !unicode.IsSpace(rune(s[i+len(marker)]))
}

func emphasisBoundaryEnd(s string, end int, marker string) bool {
	if end == 0 || unicode.IsSpace(rune(s[end-1])) {
		return false
	}
	after := end + len(marker)
	if marker[0] == '_' && after < len(s) && isAlphaNum(rune(s[after])) {
		return false
	}
	return true
}

func isAlphaNum(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// fenceMarker reports whether line opens a fenced code block, returning the
// fence delimiter and the info string that names the language.
func fenceMarker(line string) (marker, info string, ok bool) {
	trimmed := strings.TrimSpace(line)
	for _, m := range []string{codeFenceTick, codeFenceTilde} {
		if strings.HasPrefix(trimmed, m) {
			return m, strings.TrimSpace(strings.TrimLeft(trimmed, string(m[0]))), true
		}
	}
	return "", "", false
}

func splitLeadingWhitespace(s string) (string, string) {
	i := 0
	for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
		i++
	}
	return s[:i], s[i:]
}

func isHorizontalRule(line string) bool {
	leading, rest := splitLeadingWhitespace(line)
	if len(leading) > 3 {
		return false
	}
	rest = strings.TrimSpace(rest)
	if rest == "" || rest[0] != '-' && rest[0] != '*' && rest[0] != '_' {
		return false
	}
	marker := rest[0]
	count := 0
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case marker:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

func listMarker(s string) (marker, body string, ok bool) {
	if len(s) >= 2 && (s[0] == '-' || s[0] == '*' || s[0] == '+') && unicode.IsSpace(rune(s[1])) {
		return "-", strings.TrimLeft(s[2:], " \t"), true
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s)-1 || (s[i] != '.' && s[i] != ')') || !unicode.IsSpace(rune(s[i+1])) {
		return "", "", false
	}
	return s[:i+1], strings.TrimLeft(s[i+2:], " \t"), true
}

func wrapBody(body, firstPrefix, nextPrefix string, width int, ansi bool) []string {
	if body == "" {
		return []string{strings.TrimRight(firstPrefix, " ")}
	}
	return wrapRenderedSoft(renderInline(body, ansi, style{}), firstPrefix, nextPrefix, width)
}

// wrapRenderedSoft preserves the established paragraph and list behavior: it
// wraps at words when there is room for useful prose, but leaves an overlong
// word alone. Table cells use wrapRenderedHard instead because a grid cannot
// exceed its assigned column width.
func wrapRenderedSoft(rendered, firstPrefix, nextPrefix string, width int) []string {
	if width <= visibleLen(firstPrefix)+8 || visibleLen(firstPrefix)+visibleLen(rendered) <= width {
		return []string{firstPrefix + rendered}
	}
	// Wrap the rendered text, not the source. Breaking the source first and
	// rendering each line separately splits a span that straddles the break
	// into two halves, neither of which parses: "**a b**" wrapped between the
	// words leaves a literal "**a" and "b**". It also measured width in source
	// columns rather than the columns the reader sees.
	words := renderedFields(rendered)
	if len(words) == 0 {
		return []string{strings.TrimRight(firstPrefix, " ")}
	}
	var (
		out    []string
		active spanState
		prefix = firstPrefix
		line   = words[0]
		limit  = width - visibleLen(firstPrefix)
	)
	active.scan(words[0])
	for _, word := range words[1:] {
		if visibleLen(line)+1+visibleLen(word) > limit {
			// Close whatever is still open before the break so the next line's
			// indent does not inherit it, then reopen past the indent.
			out = append(out, prefix+line+active.off())
			prefix = nextPrefix
			limit = width - visibleLen(prefix)
			line = active.on() + word
			active.scan(word)
			continue
		}
		line += " " + word
		active.scan(word)
	}
	return append(out, prefix+line+active.off())
}

// renderedFields splits rendered text on visible whitespace without treating an
// ANSI-only segment as a word. SGR sequences separated from visible text by
// whitespace can occur around padded or empty spans (for example, "` code `").
// Attach those sequences to a neighboring visible word so wrapping cannot add a
// space for a zero-width token.
func renderedFields(rendered string) []string {
	fields := strings.Fields(rendered)
	words := make([]string, 0, len(fields))
	var leading strings.Builder
	for _, field := range fields {
		if visibleLen(field) == 0 {
			if len(words) == 0 {
				leading.WriteString(field)
			} else {
				words[len(words)-1] += field
			}
			continue
		}
		if leading.Len() != 0 {
			field = leading.String() + field
			leading.Reset()
		}
		words = append(words, field)
	}
	return words
}

// renderedUnit is either one visible UTF-8 rune or a complete ANSI CSI
// sequence. Keeping CSI sequences indivisible lets constrained table cells
// wrap safely without exposing escape bytes or splitting a rune encoding.
type renderedUnit struct {
	text       string
	visible    bool
	columns    int
	whitespace bool
}

func renderedUnits(rendered string) []renderedUnit {
	units := make([]renderedUnit, 0, len(rendered))
	for i := 0; i < len(rendered); {
		if rendered[i] == '\x1b' && i+1 < len(rendered) && rendered[i+1] == '[' {
			end := i + 2
			for end < len(rendered) && (rendered[end] < '@' || rendered[end] > '~') {
				end++
			}
			if end < len(rendered) {
				end++
				units = append(units, renderedUnit{text: rendered[i:end]})
				i = end
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(rendered[i:])
		if size == 0 {
			size = 1
		}
		units = append(units, renderedUnit{
			text:       rendered[i : i+size],
			visible:    true,
			columns:    runeColumns(r),
			whitespace: unicode.IsSpace(r),
		})
		i += size
	}
	return units
}

// wrapRenderedHard wraps already-rendered inline text to a visible-column
// limit. It prefers the last whitespace that fits, and otherwise hard-splits a
// token. Each emitted fragment closes active SGR attributes; continuations
// restore the source state before consuming more content, so cell padding and
// borders never inherit inline styling.
func wrapRenderedHard(rendered string, width int) []string {
	if width < 1 {
		width = 1
	}
	units := renderedUnits(rendered)
	if len(units) == 0 || visibleLen(rendered) == 0 {
		return []string{""}
	}

	var (
		out          []string
		state        spanState
		pos          int
		continuation bool
		trimLeading  bool
	)
	for pos < len(units) {
		startState := state
		fragment := make([]renderedUnit, 0, width)
		visible := 0
		hasContent := false
		lastBreakFragment := -1
		lastBreakPos := -1
		var lastBreakState spanState

		for pos < len(units) {
			unit := units[pos]
			if unit.visible && unit.whitespace && trimLeading && !hasContent {
				pos++
				continue
			}
			if unit.visible && unit.columns > 0 && visible+unit.columns > width && hasContent {
				if lastBreakFragment >= 0 {
					fragment = fragment[:lastBreakFragment]
					state = lastBreakState
					// The whitespace becomes the line break. Any later ANSI sequence
					// is reprocessed with the continuation so it stays adjacent to
					// visible content.
					pos = lastBreakPos + 1
				}
				break
			}

			fragment = append(fragment, unit)
			pos++
			if !unit.visible {
				state.scan(unit.text)
				continue
			}
			visible += unit.columns
			if unit.whitespace {
				if hasContent {
					lastBreakFragment = len(fragment) - 1
					lastBreakPos = pos - 1
					lastBreakState = state
				}
				continue
			}
			hasContent = true
		}

		trimLeading = true
		if !hasContent {
			continue
		}
		var line strings.Builder
		if continuation {
			line.WriteString(startState.on())
		}
		for _, unit := range fragment {
			line.WriteString(unit.text)
		}
		line.WriteString(state.off())
		out = append(out, line.String())
		continuation = true
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

// spanState is the styling left open at some point in already-rendered text.
// A wrap point falls wherever the width runs out, which may be inside an
// emphasis or link span, so the wrapper replays the escapes it has passed to
// know what to close at the break and restore after it.
type spanState struct {
	bold      bool
	italic    bool
	underline bool
	color     string
}

// scan advances st over s, applying every SGR sequence it contains.
func (st *spanState) scan(s string) {
	for i := 0; i < len(s); i++ {
		if s[i] != '\x1b' || i+1 >= len(s) || s[i+1] != '[' {
			continue
		}
		end := i + 2
		for end < len(s) && (s[end] == ';' || s[end] >= '0' && s[end] <= '9') {
			end++
		}
		if end >= len(s) || s[end] != 'm' {
			i = end
			continue
		}
		for param := range strings.SplitSeq(s[i+2:end], ";") {
			st.apply(param)
		}
		i = end
	}
}

func (st *spanState) apply(param string) {
	switch param {
	case "", "0":
		*st = spanState{}
	case "1":
		st.bold = true
	case "3":
		st.italic = true
	case "4":
		st.underline = true
	case "22":
		st.bold = false
	case "23":
		st.italic = false
	case "24":
		st.underline = false
	case "39":
		st.color = ""
	default:
		// 30-37 and 90-97 set the foreground. Nothing else is emitted here, so
		// anything unrecognized is left alone rather than guessed at.
		if len(param) == 2 && (param[0] == '3' || param[0] == '9') && param[1] <= '7' {
			st.color = param
		}
	}
}

// on returns the escapes that re-establish the state after a break.
func (st spanState) on() string {
	var b strings.Builder
	if st.bold {
		b.WriteString(ansiBold)
	}
	if st.italic {
		b.WriteString(ansiItalic)
	}
	if st.underline {
		b.WriteString(ansiUnderline)
	}
	if st.color != "" {
		b.WriteString("\x1b[" + st.color + "m")
	}
	return b.String()
}

// off returns the escapes that clear it again.
func (st spanState) off() string {
	var b strings.Builder
	if st.color != "" {
		b.WriteString(ansiColorOff)
	}
	if st.underline {
		b.WriteString(ansiUnderlineOff)
	}
	if st.italic {
		b.WriteString(ansiItalicOff)
	}
	if st.bold {
		b.WriteString(ansiBoldOff)
	}
	return b.String()
}

type tableLine struct {
	text    string
	newline bool
}

type alignment int

const (
	alignLeft alignment = iota
	alignRight
	alignCenter
)

func tableCandidate(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed != "" && strings.Contains(trimmed, "|")
}

func validTable(lines []tableLine) bool {
	if len(lines) < 2 {
		return false
	}
	header := parseCells(lines[0].text)
	separator := parseCells(lines[1].text)
	return len(header) > 0 && len(separator) > 0 && isSeparatorRow(separator)
}

func parseCells(line string) []string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "|") {
		line = line[1:]
	}
	if strings.HasSuffix(line, "|") {
		line = line[:len(line)-1]
	}
	parts := strings.Split(line, "|")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func isSeparatorRow(cells []string) bool {
	for _, cell := range cells {
		if !isSeparatorCell(cell) {
			return false
		}
	}
	return true
}

func isSeparatorCell(cell string) bool {
	cell = strings.TrimSpace(cell)
	if cell == "" {
		return false
	}
	dashes := 0
	for i, r := range cell {
		switch r {
		case '-':
			dashes++
		case ':':
			if i != 0 && i != len(cell)-1 {
				return false
			}
		default:
			return false
		}
	}
	return dashes >= minTableRule
}

func parseAlignments(cells []string) []alignment {
	out := make([]alignment, len(cells))
	for i, cell := range cells {
		cell = strings.TrimSpace(cell)
		left := strings.HasPrefix(cell, ":")
		right := strings.HasSuffix(cell, ":")
		switch {
		case left && right:
			out[i] = alignCenter
		case right:
			out[i] = alignRight
		default:
			out[i] = alignLeft
		}
	}
	return out
}

func formatTableRow(row []string, widths []int, aligns []alignment) string {
	var b strings.Builder
	b.WriteByte('|')
	for i, width := range widths {
		cell := ""
		if i < len(row) {
			cell = row[i]
		}
		fmt.Fprintf(&b, " %s |", padCell(cell, width, aligns[i]))
	}
	return b.String()
}

func formatTableRule(widths []int, aligns []alignment) string {
	var b strings.Builder
	b.WriteByte('|')
	for i, width := range widths {
		ruleWidth := width
		if ruleWidth < minTableRule {
			ruleWidth = minTableRule
		}
		rule := strings.Repeat("-", ruleWidth)
		switch aligns[i] {
		case alignRight:
			rule = strings.Repeat("-", ruleWidth-1) + ":"
		case alignCenter:
			if ruleWidth <= 3 {
				rule = ":-:"
			} else {
				rule = ":" + strings.Repeat("-", ruleWidth-2) + ":"
			}
		}
		fmt.Fprintf(&b, " %s |", rule)
	}
	return b.String()
}

func padCell(cell string, width int, align alignment) string {
	pad := width - visibleLen(cell)
	if pad <= 0 {
		return cell
	}
	switch align {
	case alignRight:
		return strings.Repeat(" ", pad) + cell
	case alignCenter:
		left := pad / 2
		right := pad - left
		return strings.Repeat(" ", left) + cell + strings.Repeat(" ", right)
	default:
		return cell + strings.Repeat(" ", pad)
	}
}

func visibleLen(s string) int {
	n := 0
	for i := 0; i < len(s); {
		if s[i] == '\x1b' && i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < '@' || s[i] > '~') {
				i++
			}
			if i < len(s) {
				i++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if size == 0 {
			size = 1
		}
		i += size
		n += runeColumns(r)
	}
	return n
}

// Retain the qjs-derived width rules for Japanese and combining marks.
func runeColumns(r rune) int {
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || r == 0x200B || r == 0x200D {
		return 0
	}
	if r >= 0x1100 && (r <= 0x115F ||
		r >= 0x2E80 && r <= 0xA4CF && r != 0x303F ||
		r >= 0xAC00 && r <= 0xD7A3 ||
		r >= 0xF900 && r <= 0xFAFF ||
		r >= 0xFE30 && r <= 0xFE4F ||
		r >= 0xFF00 && r <= 0xFF60 ||
		r >= 0xFFE0 && r <= 0xFFE6 ||
		r >= 0x1F300 && r <= 0x1F64F ||
		r >= 0x1F900 && r <= 0x1F9FF ||
		r >= 0x20000 && r <= 0x3FFFD) {
		return 2
	}
	return 1
}
