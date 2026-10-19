package markdown

import (
	"strings"
	"testing"
	"unicode/utf8"

	"forge.harakara.site/littleisland/noe/v2/internal/mdcli/highlight"
)

func TestRenderDisabledReturnsRawText(t *testing.T) {
	in := "**bold**\n[docs](https://example.com)"
	if got := Render(in, Options{}); got != in {
		t.Fatalf("Render disabled = %q, want raw %q", got, in)
	}
}

func TestRenderStripsEmphasisWithoutANSI(t *testing.T) {
	got := Render("Use **bold**, *italic*, and ***both***.\n---", Options{Enabled: true})
	want := "Use bold, italic, and both.\n" + HorizontalRule
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}
}

func TestRenderAppliesANSIEmphasisAndHeadings(t *testing.T) {
	got := Render("# Title\nUse **bold** and *italic*.", Options{Enabled: true, ANSI: true})
	for _, want := range []string{
		ansiBoldUnderline + "# Title" + ansiUnderlineOff + ansiBoldOff,
		ansiBold + "bold" + ansiBoldOff,
		ansiItalic + "italic" + ansiItalicOff,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("rendered output missing %q:\n%q", want, got)
		}
	}
}

func TestRenderLinksAndRawURLs(t *testing.T) {
	got := Render("Read [docs](https://example.com/docs) and https://example.com/path.", Options{Enabled: true})
	want := "Read docs <https://example.com/docs> and https://example.com/path."
	if got != want {
		t.Fatalf("Render = %q, want %q", got, want)
	}

	gotANSI := Render("See https://example.com.", Options{Enabled: true, ANSI: true})
	wantANSI := ansiLink + "https://example.com" + ansiColorOff + ansiUnderlineOff + "."
	if gotANSI != "See "+wantANSI {
		t.Fatalf("ANSI URL = %q, want %q", gotANSI, "See "+wantANSI)
	}
}

func TestRenderListsNormalizeMarkersAndWrapContinuations(t *testing.T) {
	input := "* first item has several words\n  + child item\n1. ordered item"
	got := Render(input, Options{Enabled: true, Width: 24})
	want := "- first item has several\n  words\n  - child item\n1. ordered item"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderWrapsParagraphsWithPrefix(t *testing.T) {
	input := "alpha beta gamma delta epsilon zeta eta theta"
	got := Render(input, Options{Enabled: true, Width: 24, Prefix: "  "})
	want := "  alpha beta gamma delta\n  epsilon zeta eta theta"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
}

// Regression: wrapping used to split the source text and render each resulting
// line on its own, so a span crossing a wrap point lost the half of its markers
// that landed on the other line. Neither half then parsed, and the raw markers
// reached the output -- in plain mode as well as colored.
func TestRenderWrapsSpansAcrossLineBreaks(t *testing.T) {
	const input = "Alpha **beta gamma delta** epsilon"

	plain := Render(input, Options{Enabled: true, Width: 20})
	if want := "Alpha beta gamma\ndelta epsilon"; plain != want {
		t.Errorf("plain wrap =\n%q\nwant\n%q", plain, want)
	}
	if strings.Contains(plain, "*") {
		t.Errorf("emphasis markers leaked into plain output: %q", plain)
	}

	got := Render(input, Options{Enabled: true, ANSI: true, Width: 20})
	want := "Alpha " + ansiBold + "beta gamma" + ansiBoldOff + "\n" +
		ansiBold + "delta" + ansiBoldOff + " epsilon"
	if got != want {
		t.Errorf("ANSI wrap =\n%q\nwant\n%q", got, want)
	}
}

// ANSI escapes separated from their visible content by span padding must not
// become zero-width "words" that introduce spaces absent from plain rendering.
func TestRenderWrapIgnoresANSIOnlyFields(t *testing.T) {
	for _, input := range []string{
		"start ` code ` end words",
		"start `` end words",
	} {
		plain := Render(input, Options{Enabled: true, Width: 12})
		colored := Render(input, Options{Enabled: true, ANSI: true, Width: 12})
		if got := stripANSI(colored); got != plain {
			t.Errorf("colored padded span stripped =\n%q\nplain =\n%q\nfor input %q", got, plain, input)
		}
	}
}

// The continuation indent belongs to the list, not to the span being carried
// over the break, so styling closes before the newline and reopens past the
// indent instead of running through it.
func TestRenderWrapKeepsContinuationIndentUnstyled(t *testing.T) {
	got := Render("- **alpha beta gamma delta**", Options{Enabled: true, ANSI: true, Width: 20})
	want := "- " + ansiBold + "alpha beta gamma" + ansiBoldOff + "\n" +
		"  " + ansiBold + "delta" + ansiBoldOff
	if got != want {
		t.Fatalf("wrapped list item =\n%q\nwant\n%q", got, want)
	}
}

// Width counts the columns the reader sees, not the source columns: the eight
// characters of "**one**" occupy three.
func TestRenderWrapsOnRenderedWidth(t *testing.T) {
	got := Render("**one** **two** **three** **four**", Options{Enabled: true, Width: 12})
	if want := "one two\nthree four"; got != want {
		t.Fatalf("wrap by rendered width =\n%q\nwant\n%q", got, want)
	}
}

// Styling decorates, it never rewrites: at any width, stripping the escapes
// from a wrapped colored render must give the wrapped plain render back, and
// no markup may survive into either.
func TestWrappedOutputStripsToPlainAtEveryWidth(t *testing.T) {
	inputs := []string{
		"Alpha **beta gamma delta** epsilon zeta eta theta",
		"Read the *manual at https://example.com/docs today* please",
		"Use `some inline code` and **bold [docs](https://example.com) here** too",
		"Use ` padded inline code ` and an `` empty span across several words",
		"- **alpha beta** gamma `delta epsilon` zeta",
		"***everything emphasized across a long enough line to wrap twice***",
	}
	for _, input := range inputs {
		for width := 12; width <= 40; width++ {
			plain := Render(input, Options{Enabled: true, Width: width})
			colored := Render(input, Options{Enabled: true, ANSI: true, Width: width})
			if got := stripANSI(colored); got != plain {
				t.Errorf("width %d: colored stripped =\n%q\nplain =\n%q\nfor input %q",
					width, got, plain, input)
			}
			for _, marker := range []string{"**", "](", "`"} {
				if strings.Contains(plain, marker) {
					t.Errorf("width %d: %q survived rendering of %q:\n%q",
						width, marker, input, plain)
				}
			}
		}
	}
}

func TestRenderFormatsTables(t *testing.T) {
	input := "| Name | Count |\n| --- | ---: |\n| a | 2 |\n| long | 10 |\n"
	got := Render(input, Options{Enabled: true})
	want := "| Name | Count |\n" +
		"| ---- | ----: |\n" +
		"| a    |     2 |\n" +
		"| long |    10 |\n"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderPrefixesTables(t *testing.T) {
	input := "| Name | Count |\n| --- | ---: |\n| a | 2 |\n"
	got := Render(input, Options{Enabled: true, Prefix: "  "})
	want := "  | Name | Count |\n" +
		"  | ---- | ----: |\n" +
		"  | a    |     2 |\n"
	if got != want {
		t.Fatalf("Render =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderFittingTableWithWidthPreservesBytes(t *testing.T) {
	input := "| **Name** | `Count` |\n| --- | ---: |\n| a | 2 |\n"
	opts := Options{Enabled: true, ANSI: true, Prefix: "  "}
	want := Render(input, opts)
	width := longestVisibleLine(want)
	opts.Width = width
	if got := Render(input, opts); got != want {
		t.Fatalf("fitting table with width %d =\n%q\nwant\n%q", width, got, want)
	}
}

func TestRenderFitsWideFiveColumnTable(t *testing.T) {
	input := "| Source file | Target contexts | Context threshold 80 | Retention actions | Recent messages 3 |\n" +
		"| --- | --- | --- | --- | --- |\n" +
		"| raw.ndjson | turn | at 80% | compact | recent |\n"

	natural := Render(input, Options{Enabled: true})
	if got := longestVisibleLine(natural); got != 96 {
		t.Fatalf("natural table width = %d, want 96:\n%s", got, natural)
	}
	got := Render(input, Options{Enabled: true, Width: 80})
	assertTableLinesFit(t, got, 80)
	if !strings.Contains(got, "|") {
		t.Fatalf("wide five-column table used stacked fallback:\n%s", got)
	}
	for _, want := range []string{"Source file", "Context", "threshold 80", "Retention", "actions", "raw.ndjson", "recent"} {
		if !strings.Contains(got, want) {
			t.Errorf("responsive table lost %q:\n%s", want, got)
		}
	}
}

func TestRenderResponsiveTableAlignsEveryFragment(t *testing.T) {
	input := "| Left | Right | Center |\n" +
		"| --- | ---: | :---: |\n" +
		"| alpha beta gamma | 123456789012 | abcdefghi |\n"
	got := Render(input, Options{Enabled: true, Width: 30})
	assertTableLinesFit(t, got, 30)
	for _, want := range []string{
		"| alpha   | 1234567 | abcdef |",
		"| beta    |   89012 |  ghi   |",
		"| gamma   |         |        |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing aligned fragment %q from:\n%s", want, got)
		}
	}
}

func TestRenderResponsiveTableAlignsWrappedHeaders(t *testing.T) {
	input := "| Left | Long Right Header | Long Center Header |\n" +
		"| --- | ---: | :---: |\n" +
		"| alpha beta gamma | 123456789012 | abcdefghi |\n"
	got := Render(input, Options{Enabled: true, Width: 30})
	assertTableLinesFit(t, got, 30)
	for _, want := range []string{
		"| Left    |    Long |  Long  |",
		"|         |   Right | Center |",
		"|         |  Header | Header |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing aligned header fragment %q from:\n%s", want, got)
		}
	}
}

func TestFitTableWidthsIsDeterministic(t *testing.T) {
	tests := []struct {
		name    string
		natural []int
		budget  int
		want    []int
	}{
		{
			name:    "one extra cell goes to the leftmost eligible column",
			natural: []int{5, 5, 5},
			budget:  10,
			want:    []int{4, 3, 3},
		},
		{
			name:    "saturated columns are skipped in later rounds",
			natural: []int{3, 4, 10},
			budget:  14,
			want:    []int{3, 4, 7},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := fitTableWidths(tt.natural, tt.budget)
			if len(got) != len(tt.want) {
				t.Fatalf("fitTableWidths(%v, %d) = %v, want %v", tt.natural, tt.budget, got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("fitTableWidths(%v, %d) = %v, want %v", tt.natural, tt.budget, got, tt.want)
				}
			}
		})
	}
}

func TestWrapRenderedHardSplitsLongTokensWithoutDataLoss(t *testing.T) {
	for _, input := range []string{
		"`abcdefghijkl`",
		"https://example.com/a-very-long-path",
	} {
		plain := renderInline(input, false, style{})
		fragments := wrapRenderedHard(plain, 3)
		if got := strings.Join(fragments, ""); got != plain {
			t.Errorf("plain fragments for %q = %q, want %q", input, got, plain)
		}
		for _, fragment := range fragments {
			if got := visibleLen(fragment); got > 3 {
				t.Errorf("plain fragment width = %d, want <= 3: %q", got, fragment)
			}
		}

		colored := renderInline(input, true, style{})
		coloredFragments := wrapRenderedHard(colored, 3)
		if got := stripANSI(strings.Join(coloredFragments, "")); got != plain {
			t.Errorf("colored fragments for %q stripped = %q, want %q", input, got, plain)
		}
		for _, fragment := range coloredFragments {
			if got := visibleLen(fragment); got > 3 {
				t.Errorf("colored fragment width = %d, want <= 3: %q", got, fragment)
			}
		}
	}
}

func TestRenderResponsiveTableMinimumGridAndStackedFallback(t *testing.T) {
	input := "| A | B |\n| --- | --- |\n| x | y |\n"
	grid := Render(input, Options{Enabled: true, Width: 13})
	if !strings.HasPrefix(grid, "|") {
		t.Fatalf("minimum grid width did not retain a grid:\n%s", grid)
	}
	assertTableLinesFit(t, grid, 13)

	stacked := Render(input, Options{Enabled: true, Width: 12})
	wantStacked := "A: x\nB: y\n"
	if stacked != wantStacked {
		t.Fatalf("one column below the minimum grid =\n%q\nwant\n%q", stacked, wantStacked)
	}
	assertTableLinesFit(t, stacked, 12)
}

func TestRenderResponsiveTableStackedFieldsPreserveRaggedRows(t *testing.T) {
	input := "| | Known |\n" +
		"| --- | --- |\n" +
		"| first |\n" +
		"| second | value | extra |\n"
	got := Render(input, Options{Enabled: true, Width: 18})
	want := "Column 1: first\n" +
		"Known:\n" +
		"Column 3:\n" +
		"\n" +
		"Column 1: second\n" +
		"Known: value\n" +
		"Column 3: extra\n"
	if got != want {
		t.Fatalf("stacked ragged table =\n%q\nwant\n%q", got, want)
	}
	assertTableLinesFit(t, got, 18)

	headerOnly := Render("| | Name |\n| --- | --- |\n", Options{Enabled: true, Width: 12})
	if want := "- Column 1\n- Name\n"; headerOnly != want {
		t.Fatalf("stacked header-only table =\n%q\nwant\n%q", headerOnly, want)
	}
	headerOnly = Render("| | Name |\n| --- | --- |", Options{Enabled: true, Width: 12})
	if want := "- Column 1\n- Name"; headerOnly != want {
		t.Fatalf("stacked header-only table without trailing newline =\n%q\nwant\n%q", headerOnly, want)
	}
}

func TestRenderResponsiveTableIncludesPrefixInBudget(t *testing.T) {
	input := "| A | B |\n| --- | --- |\n| x | y |\n"
	grid := Render(input, Options{Enabled: true, Prefix: "  ", Width: 15})
	if !strings.HasPrefix(grid, "  |") {
		t.Fatalf("prefix-inclusive minimum width did not retain grid:\n%s", grid)
	}
	assertTableLinesFit(t, grid, 15)

	stacked := Render(input, Options{Enabled: true, Prefix: "  ", Width: 14})
	if strings.Contains(stacked, "|") {
		t.Fatalf("prefix was not counted in stacked fallback budget:\n%s", stacked)
	}
	assertTableLinesFit(t, stacked, 14)
}

func TestRenderResponsiveTableProgressesWithWidePrefix(t *testing.T) {
	input := "| A | B |\n| --- | --- |\n| x | y |"
	got := Render(input, Options{Enabled: true, Prefix: "wide", Width: 3})
	if strings.Contains(got, "|") {
		t.Fatalf("wide prefix did not use stacked fallback:\n%s", got)
	}
	for _, want := range []string{"wideA", "wideB", "widex", "widey"} {
		if !strings.Contains(got, want) {
			t.Errorf("wide-prefix fallback did not make progress through %q:\n%s", want, got)
		}
	}
}

func TestRenderResponsiveTablePreservesFinalNewline(t *testing.T) {
	base := "| A | B |\n| --- | --- |\n| long value | z |"
	for _, layout := range []struct {
		name  string
		width int
	}{
		{name: "wrapped grid", width: 13},
		{name: "stacked fallback", width: 12},
	} {
		for _, trailingNewline := range []bool{false, true} {
			t.Run(layout.name+"/"+map[bool]string{false: "without trailing newline", true: "with trailing newline"}[trailingNewline], func(t *testing.T) {
				input := base
				if trailingNewline {
					input += "\n"
				}
				got := Render(input, Options{Enabled: true, Width: layout.width})
				if strings.HasSuffix(got, "\n") != trailingNewline {
					t.Fatalf("trailing newline = %t, want %t: %q", strings.HasSuffix(got, "\n"), trailingNewline, got)
				}
				assertTableLinesFit(t, got, layout.width)
			})
		}
	}
}

func TestRenderResponsiveTablesStripANSIToPlain(t *testing.T) {
	input := "| Item | Value |\n" +
		"| --- | ---: |\n" +
		"| **alpha beta gamma** | `123456` |\n"
	for _, tt := range []struct {
		name  string
		width int
	}{
		{name: "wrapped grid", width: 15},
		{name: "stacked fallback", width: 12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plain := Render(input, Options{Enabled: true, Width: tt.width})
			colored := Render(input, Options{Enabled: true, ANSI: true, Width: tt.width})
			if got := stripANSI(colored); got != plain {
				t.Fatalf("ANSI table stripped =\n%q\nplain =\n%q", got, plain)
			}
			if !strings.Contains(colored, "\x1b[") {
				t.Fatalf("ANSI table contains no styling: %q", colored)
			}
			assertTableLinesFit(t, colored, tt.width)
		})
	}
}

func TestWrapRenderedHardPreservesUTF8(t *testing.T) {
	input := "é界🙂abcdef"
	fragments := wrapRenderedHard(input, 2)
	if got := strings.Join(fragments, ""); got != input {
		t.Fatalf("joined UTF-8 fragments = %q, want %q", got, input)
	}
	for _, fragment := range fragments {
		if !utf8.ValidString(fragment) {
			t.Errorf("fragment is invalid UTF-8: %q", fragment)
		}
		if got := visibleLen(fragment); got > 2 {
			t.Errorf("fragment width = %d, want <= 2: %q", got, fragment)
		}
	}
}

func TestStreamResponsiveTableMatchesRenderAcrossDeltas(t *testing.T) {
	input := "| **Source file** | Count |\n" +
		"| --- | ---: |\n" +
		"| alpha beta gamma delta | `123456789` |\n"
	for _, tt := range []struct {
		name  string
		width int
	}{
		{name: "wrapped grid", width: 20},
		{name: "stacked fallback", width: 12},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := Options{Enabled: true, ANSI: true, Width: tt.width}
			want := Render(input, opts)

			stream := NewStream(opts)
			var got strings.Builder
			for _, delta := range []string{"| **Sou", "rce file** | Co", "unt |\n| --- | ---: |\n| alpha beta ", "gamma delta | `123", "456789` |\n"} {
				if text := stream.Write(delta); text != "" {
					t.Fatalf("table stream emitted before termination: %q", text)
				}
			}
			got.WriteString(stream.Flush())
			if got.String() != want {
				t.Fatalf("streamed responsive table =\n%q\nwant\n%q", got.String(), want)
			}
		})
	}
}

func TestRenderPreservesCodeFences(t *testing.T) {
	input := "```\n**raw** https://example.com\n```\n"
	want := "  ```\n  **raw** https://example.com\n  ```\n"
	if got := Render(input, Options{Enabled: true, ANSI: true}); got != want {
		t.Fatalf("code fence rendered = %q, want %q", got, want)
	}
}

func TestRenderInlineCodeStripsBackticksWithoutANSI(t *testing.T) {
	got := Render("Use `foo` here.", Options{Enabled: true})
	want := "Use foo here."
	if got != want {
		t.Fatalf("inline code no-ANSI = %q, want %q", got, want)
	}
}

func TestRenderInlineCodeAppliesANSI(t *testing.T) {
	got := Render("Use `foo` here.", Options{Enabled: true, ANSI: true})
	want := "Use " + ansiCode + "foo" + ansiColorOff + " here."
	if got != want {
		t.Fatalf("inline code ANSI = %q, want %q", got, want)
	}
}

// Regression: spans used to close with a blanket "\x1b[0m", so an inline span
// nested inside a heading or emphasis cancelled the style wrapping it and the
// rest of the line rendered unstyled.
func TestRenderNestedSpansPreserveOuterStyle(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "code inside heading keeps heading bold",
			input: "# Heading with `code` and more",
			want:  ansiBoldUnderline + "# Heading with " + ansiCode + "code" + ansiColorOff + " and more" + ansiUnderlineOff + ansiBoldOff,
		},
		{
			name:  "code inside bold keeps bold",
			input: "**bold with `code` inside**",
			want:  ansiBold + "bold with " + ansiCode + "code" + ansiColorOff + " inside" + ansiBoldOff,
		},
		{
			name:  "code inside italic keeps italic",
			input: "*italic with `code` inside*",
			want:  ansiItalic + "italic with " + ansiCode + "code" + ansiColorOff + " inside" + ansiItalicOff,
		},
		{
			name:  "link inside bold keeps bold",
			input: "**see https://example.com now**",
			want:  ansiBold + "see " + ansiLink + "https://example.com" + ansiColorOff + ansiUnderlineOff + " now" + ansiBoldOff,
		},
		{
			name:  "bold inside heading does not end the heading",
			input: "## Heading with **bold** and more",
			want:  ansiBold + "## Heading with " + ansiBold + "bold" + " and more" + ansiBoldOff,
		},
		{
			name:  "link inside h1 keeps the heading underline",
			input: "# See https://example.com now",
			want:  ansiBoldUnderline + "# See " + ansiLink + "https://example.com" + ansiColorOff + " now" + ansiUnderlineOff + ansiBoldOff,
		},
		{
			name:  "bold italic closes both attributes",
			input: "***both***",
			want:  ansiBold + ansiItalic + "both" + ansiItalicOff + ansiBoldOff,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Render(tt.input, Options{Enabled: true, ANSI: true})
			if got != tt.want {
				t.Fatalf("Render(%q) =\n%q\nwant\n%q", tt.input, got, tt.want)
			}
			if strings.Contains(got, "\x1b[0m") {
				t.Errorf("Render(%q) used a blanket reset, which cancels the enclosing style: %q", tt.input, got)
			}
		})
	}
}

// Inline code is cyan, not yellow: yellow falls under 3:1 contrast against most
// light terminal backgrounds, and cyan is the terminal convention for code.
func TestInlineCodeUsesCyan(t *testing.T) {
	if ansiCode != "\x1b[36m" {
		t.Fatalf("inline code style = %q, want cyan \\x1b[36m", ansiCode)
	}
}

func TestRenderCodeFenceIsIndented(t *testing.T) {
	input := "```go\nfmt.Println()\n```\n"
	want := "  ```go\n  fmt.Println()\n  ```\n"
	if got := Render(input, Options{Enabled: true}); got != want {
		t.Fatalf("code fence indent = %q, want %q", got, want)
	}
}

func TestRenderInlineCodeUnterminated(t *testing.T) {
	// A lone backtick with no closing backtick must pass through unchanged.
	got := Render("price is 5`", Options{Enabled: true, ANSI: true})
	want := "price is 5`"
	if got != want {
		t.Fatalf("unterminated backtick = %q, want %q", got, want)
	}
}

func TestStreamHandlesSplitInlineMarkdown(t *testing.T) {
	stream := NewStream(Options{Enabled: true})
	if got := stream.Write("**bo"); got != "" {
		t.Fatalf("first split Write = %q, want empty", got)
	}
	if got := stream.Write("ld**\n"); got != "bold\n" {
		t.Fatalf("second split Write = %q, want bold line", got)
	}
	if got := stream.Flush(); got != "" {
		t.Fatalf("Flush = %q, want empty", got)
	}
}

func TestStreamBuffersTablesUntilBlockEnds(t *testing.T) {
	stream := NewStream(Options{Enabled: true})
	if got := stream.Write("| A | B |\n| --- | --- |\n| x | yy |\n"); got != "" {
		t.Fatalf("table should be buffered, got %q", got)
	}
	got := stream.Write("after\n")
	want := "| A   | B   |\n" +
		"| --- | --- |\n" +
		"| x   | yy  |\n" +
		"after\n"
	if got != want {
		t.Fatalf("table flush =\n%q\nwant\n%q", got, want)
	}
}

func TestRenderHighlightsLabeledCodeFence(t *testing.T) {
	input := "```go\nfunc main() {\n```\n"
	got := Render(input, Options{Enabled: true, ANSI: true})
	body := strings.Split(got, "\n")[1]
	if !strings.Contains(body, "\x1b[") {
		t.Fatalf("go fence body was not highlighted: %q", body)
	}
	if !strings.Contains(body, "func") {
		t.Fatalf("go fence body lost its text: %q", body)
	}
}

// TestRenderHighlightingIsAdditive holds the renderer to the same contract the
// highlighter has: color decorates the code, it never rewrites it, so a
// stripped colored render equals the plain one.
func TestRenderHighlightingIsAdditive(t *testing.T) {
	input := "```python\n# a comment\ndef f(x):\n    return x * 2\n```\n"
	plain := Render(input, Options{Enabled: true})
	colored := Render(input, Options{Enabled: true, ANSI: true})
	if colored == plain {
		t.Fatal("colored render is identical to plain; highlighting did not run")
	}
	if got := stripANSI(colored); got != plain {
		t.Fatalf("stripped colored render = %q, want %q", got, plain)
	}
}

func TestRenderUsesSelectedFenceThemeWithoutEnablingANSI(t *testing.T) {
	input := "```go\nfunc main() {}\n```\n"
	light := Render(input, Options{Enabled: true, ANSI: true, ColorTheme: highlight.ThemeLight})
	if !strings.Contains(light, "\x1b[38;2;0;0;255mfunc") {
		t.Fatalf("light fence missing keyword palette: %q", light)
	}
	if strings.Contains(light, "\x1b[38;2;101;169;224mfunc") {
		t.Fatalf("light fence used dark keyword palette: %q", light)
	}

	stream := NewStream(Options{Enabled: true, ANSI: true, ColorTheme: highlight.ThemeLight})
	var streamed strings.Builder
	for _, delta := range []string{"```go\nfu", "nc main() {}\n```\n"} {
		streamed.WriteString(stream.Write(delta))
	}
	streamed.WriteString(stream.Flush())
	if got := streamed.String(); !strings.Contains(got, "\x1b[38;2;0;0;255mfunc") {
		t.Fatalf("streamed light fence missing keyword palette: %q", got)
	}

	plain := Render(input, Options{Enabled: true, ColorTheme: highlight.ThemeLight})
	if strings.Contains(plain, "\x1b[") {
		t.Fatalf("theme enabled ANSI on its own: %q", plain)
	}
}

func TestRenderLeavesUnknownLanguageUntouched(t *testing.T) {
	for _, info := range []string{"", "brainfuck", "text"} {
		input := "```" + info + "\nfunc main() {\n```\n"
		want := Render(input, Options{Enabled: true})
		if got := Render(input, Options{Enabled: true, ANSI: true}); got != want {
			t.Errorf("fence %q was styled: %q, want %q", info, got, want)
		}
	}
}

func TestRenderLeavesFenceDelimitersUnstyled(t *testing.T) {
	got := Render("```go\nfunc main() {\n```\n", Options{Enabled: true, ANSI: true})
	lines := strings.Split(got, "\n")
	for _, i := range []int{0, 2} {
		if strings.Contains(lines[i], "\x1b[") {
			t.Errorf("fence delimiter line %d was styled: %q", i, lines[i])
		}
	}
}

func TestRenderDoesNotHighlightWithoutANSI(t *testing.T) {
	input := "```go\nfunc main() {\n```\n"
	want := "  ```go\n  func main() {\n  ```\n"
	if got := Render(input, Options{Enabled: true}); got != want {
		t.Fatalf("no-ANSI render = %q, want %q", got, want)
	}
}

// TestRenderResetsHighlighterBetweenFences catches the highlighter outliving
// its block and coloring an unlabeled one that follows.
func TestRenderResetsHighlighterBetweenFences(t *testing.T) {
	input := "```go\nfunc main() {\n```\n\n```\nfunc main() {\n```\n"
	got := Render(input, Options{Enabled: true, ANSI: true})
	lines := strings.Split(got, "\n")
	if !strings.Contains(lines[1], "\x1b[") {
		t.Fatalf("first fence body was not highlighted: %q", lines[1])
	}
	if strings.Contains(lines[5], "\x1b[") {
		t.Fatalf("unlabeled second fence was highlighted: %q", lines[5])
	}
}

// TestRenderCarriesHighlightStateAcrossFenceLines covers a construct that
// spans lines: the middle line of a block comment has no delimiter of its own
// and is only recognizable from the line before it.
func TestRenderCarriesHighlightStateAcrossFenceLines(t *testing.T) {
	input := "```go\n/* one\ntwo\nthree */\n```\n"
	got := Render(input, Options{Enabled: true, ANSI: true})
	middle := strings.Split(got, "\n")[2]
	if !strings.Contains(middle, "\x1b[") {
		t.Fatalf("continuation line of a block comment was not styled: %q", middle)
	}
}

// TestStreamHighlightsAcrossWrites checks that highlighting survives deltas
// that split a code line, which is how the stream receives model output.
func TestStreamHighlightsAcrossWrites(t *testing.T) {
	opts := Options{Enabled: true, ANSI: true}
	want := Render("```go\nfunc main() {\n```\n", opts)

	stream := NewStream(opts)
	var got strings.Builder
	for _, delta := range []string{"```go\nfu", "nc ma", "in() {\n``", "`\n"} {
		got.WriteString(stream.Write(delta))
	}
	got.WriteString(stream.Flush())
	if got.String() != want {
		t.Fatalf("streamed render =\n%q\nwant\n%q", got.String(), want)
	}
}

func TestStreamBoundaryQueryPreservesHighlightedFenceState(t *testing.T) {
	stream := NewStream(Options{Enabled: true, ANSI: true})
	if got := stream.Write("```go\n"); got != "  ```go\n" {
		t.Fatalf("opening fence = %q", got)
	}
	if !stream.AtLineBoundary() {
		t.Fatal("complete opening fence should be a safe boundary")
	}
	if got := stream.Write("/* partial"); got != "" {
		t.Fatalf("partial highlighted line = %q, want buffered", got)
	}
	if stream.AtLineBoundary() {
		t.Fatal("incomplete highlighted code line reported as a safe boundary")
	}

	first := stream.Write("\n")
	if !stream.AtLineBoundary() {
		t.Fatal("newline-complete highlighted code line should be a safe boundary")
	}
	continued := stream.Write("continued */\n```\n")
	if !strings.Contains(first, "\x1b[") || !strings.Contains(continued, "\x1b[") {
		t.Fatalf("boundary query flushed or reset multiline highlight state: first=%q continued=%q", first, continued)
	}
	if got, want := stripANSI(first+continued), "  /* partial\n  continued */\n  ```\n"; got != want {
		t.Fatalf("highlighted fence source changed:\n got %q\nwant %q", got, want)
	}
}

func TestStreamBoundaryQueryDoesNotFlushPendingTextOrTable(t *testing.T) {
	stream := NewStream(Options{Enabled: true})
	if got := stream.Write("partial"); got != "" {
		t.Fatalf("partial write = %q, want buffered", got)
	}
	if stream.AtLineBoundary() {
		t.Fatal("incomplete source line reported as a safe boundary")
	}
	if got := stream.Write("\n| A | B |\n| --- | --- |\n"); got != "partial\n" {
		t.Fatalf("complete line/table write = %q, want only partial line", got)
	}
	if !stream.AtLineBoundary() {
		t.Fatal("newline-complete buffered table should be a safe boundary")
	}
	if got := stream.Flush(); !strings.Contains(got, "| A") {
		t.Fatalf("boundary query flushed or lost table: %q", got)
	}
}

func longestVisibleLine(text string) int {
	longest := 0
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if width := visibleLen(line); width > longest {
			longest = width
		}
	}
	return longest
}

func assertTableLinesFit(t *testing.T, text string, width int) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if visible := visibleLen(line); visible > width {
			t.Fatalf("physical line width = %d, want <= %d: %q", visible, width, line)
		}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
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
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
