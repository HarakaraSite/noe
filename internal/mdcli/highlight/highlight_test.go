package highlight

import (
	"strings"
	"testing"
)

// corpus holds a representative snippet per language. Every registered
// language must appear here so the invariant tests cover all of them.
var corpus = map[string][]string{
	"go": {
		"package main",
		"",
		"// count returns the number of items.",
		"func count(items []string) int {",
		"\tconst limit = 0x1F",
		"\ts := `raw",
		"\tstring`",
		`	fmt.Printf("%d items\n", len(items))`,
		"\t/* a block",
		"\t   comment */",
		"\treturn len(items) + 1_000",
		"}",
	},
	"rust": {
		"// a lifetime must not stain the line",
		"fn longest<'a>(x: &'a str, y: &'a str) -> &'a str {",
		"    let n: u32 = 0xFF;",
		`    println!("{} {}", x, 'c');`,
		"    if x.len() > y.len() { x } else { y }",
		"}",
	},
	"c": {
		"#include <stdio.h>",
		"int main(void) {",
		"    unsigned long n = 42UL;",
		`    printf("hello\n");`,
		"    return 0;",
		"}",
	},
	"cpp": {
		"template <typename T>",
		"class Buffer {",
		" public:",
		"  explicit Buffer(std::size_t n) : data_(n) {}",
		"  constexpr bool empty() const noexcept { return data_.empty(); }",
		"};",
	},
	"java": {
		"public final class Greeter {",
		"    private static final int MAX = 10;",
		"    public String greet(String name) {",
		`        return "hi " + name;`,
		"    }",
		"}",
	},
	"kotlin": {
		"data class User(val id: Int, val name: String)",
		"fun main() {",
		`    val u = User(1, "ada")`,
		"    println(u.name)",
		"}",
	},
	"csharp": {
		"public sealed record Point(int X, int Y) {",
		"    public double Length => Math.Sqrt(X * X + Y * Y);",
		"}",
	},
	"swift": {
		"struct Point {",
		"    let x: Double",
		"    func scaled(by f: Double) -> Point {",
		"        return Point(x: x * f)",
		"    }",
		"}",
	},
	"javascript": {
		"// fetch and log",
		"async function main() {",
		"  const res = await fetch(`${base}/items`);",
		"  const data = await res.json();",
		"  console.log(data.length, 3.14, 0b1010);",
		"}",
	},
	"typescript": {
		"interface User { id: number; name: string }",
		"export const find = (users: User[], id: number): User | undefined =>",
		"  users.find((u) => u.id === id);",
	},
	"php": {
		"<?php",
		"// greet the user",
		"function greet(string $name): string {",
		`    return "hello $name";`,
		"}",
	},
	"python": {
		"# compute a total",
		"import math",
		"",
		"def total(items: list[int]) -> int:",
		`    """Return the sum.`,
		`    Multi-line docstring."""`,
		"    return sum(x for x in items if x > 0)",
	},
	"ruby": {
		"# a small class",
		"class Greeter",
		"  attr_reader :name",
		"  def initialize(name)",
		"    @name = name",
		"  end",
		"  def greet?",
		`    puts "hi #{@name}"`,
		"  end",
		"end",
	},
	"bash": {
		"#!/usr/bin/env bash",
		"set -euo pipefail",
		"",
		"# don't let an apostrophe eat the line",
		`for f in "$@"; do`,
		`  echo "processing ${f}"`,
		"  grep -c . \"$f\" || true",
		"done",
	},
	"sql": {
		"-- recent orders",
		"SELECT id, total",
		"  FROM orders",
		" WHERE created_at > '2024-01-01'",
		"   AND total >= 100.50",
		" ORDER BY total DESC",
		" LIMIT 10;",
	},
	"lua": {
		"-- a counter",
		"local function counter()",
		"  local n = 0",
		"  return function() n = n + 1; return n end",
		"end",
	},
	"json": {
		"{",
		`  "name": "mdcli",`,
		`  "version": 2,`,
		`  "stable": true,`,
		`  "tags": ["cli", "markdown"]`,
		"}",
	},
	"dockerfile": {
		"# build stage",
		"FROM golang:1.26 AS build",
		"WORKDIR /src",
		"COPY . .",
		"RUN go build -o /out/mdcli .",
		"",
		"FROM scratch",
		"ENTRYPOINT [\"/mdcli\"]",
	},
	"makefile": {
		"BINARY := mdcli",
		"",
		".PHONY: build test",
		"",
		"build:",
		"\tgo build -o $(BINARY) .",
		"",
		"test:",
		"\tgo test ./...",
	},
	"yaml": {
		"# a service",
		"name: mdcli",
		"version: 1.2",
		"enabled: true",
		"args:",
		"  - --color=always",
		"  - --width",
		"env:",
		"  HOME: /root  # trailing comment",
		`  URL: "http://example.com/#anchor"`,
	},
	"toml": {
		"# package metadata",
		"[package]",
		`name = "mdcli"`,
		"version = 2",
		"stable = true",
		"",
		"[dependencies]",
		"none = []",
	},
	"html": {
		"<!-- a fragment",
		"     spanning lines -->",
		`<div class="card" data-id="3">`,
		"  <p>Hello &amp; welcome</p>",
		`  <img src="x.png" alt='a picture'/>`,
		"</div>",
	},
	"css": {
		"/* layout",
		"   rules */",
		".card, #main > a:hover {",
		"  display: flex;",
		"  margin-top: 1.5rem;",
		"  color: #fff;",
		`  font-family: "Inter", sans-serif;`,
		"}",
	},
	"diff": {
		"diff --git a/main.go b/main.go",
		"index 1234567..89abcde 100644",
		"--- a/main.go",
		"+++ b/main.go",
		"@@ -1,4 +1,5 @@",
		" package main",
		"-func old() {}",
		"+func new() {}",
		" // trailing context",
	},
}

// TestLineIsAdditive is the invariant the whole package rests on: highlighting
// may only insert escape sequences, never touch the text. Stripping the
// escapes must return the input byte for byte, or code stops being
// copy-pasteable and the renderer's plain and colored output diverge.
func TestLineIsAdditive(t *testing.T) {
	for name, lines := range corpus {
		t.Run(name, func(t *testing.T) {
			s := New(name)
			if s == nil {
				t.Fatalf("New(%q) = nil, want a highlighter", name)
			}
			for i, line := range lines {
				got := s.Line(line)
				if stripANSI(got) != line {
					t.Errorf("line %d altered the text:\n in:  %q\n out: %q", i+1, line, stripANSI(got))
				}
			}
		})
	}
}

// TestEveryLanguageIsCovered keeps the corpus honest: a language added to the
// table without a snippet here would silently escape every invariant test.
func TestEveryLanguageIsCovered(t *testing.T) {
	for name := range languages {
		if _, ok := corpus[name]; !ok {
			t.Errorf("language %q has no corpus snippet", name)
		}
	}
	for name := range corpus {
		if _, ok := languages[name]; !ok {
			t.Errorf("corpus has snippet for unregistered language %q", name)
		}
	}
}

// TestEscapesAreBalanced checks that every style introduced is closed, so no
// color ever leaks past the span it was meant for.
func TestEscapesAreBalanced(t *testing.T) {
	for name, lines := range corpus {
		s := New(name)
		for i, line := range lines {
			got := s.Line(line)
			opens := strings.Count(got, "\x1b[") - strings.Count(got, styleReset)
			if resets := strings.Count(got, styleReset); opens != resets {
				t.Errorf("%s line %d: %d styles opened, %d resets: %q",
					name, i+1, opens, resets, got)
			}
		}
	}
}

// TestLineColorsSomething guards against a language whose config is present
// but so wrong that it never colors anything.
func TestLineColorsSomething(t *testing.T) {
	for name, lines := range corpus {
		s := New(name)
		colored := false
		for _, line := range lines {
			if strings.Contains(s.Line(line), "\x1b[") {
				colored = true
				break
			}
		}
		if !colored {
			t.Errorf("%s: no line in the corpus received any color", name)
		}
	}
}

func TestLineSpans(t *testing.T) {
	tests := []struct {
		name string
		lang string
		line string
		want []string // substrings the styled output must contain
	}{
		{"go keyword", "go", "func main() {", []string{styled(styleKeyword, "func"), styled(styleFunction, "main")}},
		{"go string", "go", `x := "hi"`, []string{styled(styleString, `"hi"`)}},
		{"go comment", "go", "x := 1 // note", []string{styled(styleComment, "// note")}},
		{"go number", "go", "x := 42", []string{styled(styleNumber, "42")}},
		{"go builtin", "go", "var s string", []string{styled(styleBuiltin, "string")}},
		{"python keyword", "python", "def f():", []string{styled(styleKeyword, "def")}},
		{"python comment", "python", "x = 1  # note", []string{styled(styleComment, "# note")}},
		{"js template", "javascript", "const a = `x`;", []string{styled(styleString, "`x`")}},
		{"shell var", "bash", `echo "$HOME"`, []string{styled(styleBuiltin, "echo")}},
		{"shell keyword", "bash", "for f in *; do", []string{styled(styleKeyword, "for")}},
		{"sql is folded", "sql", "select * from t", []string{styled(styleKeyword, "select"), styled(styleKeyword, "from")}},
		{"sql uppercase", "sql", "SELECT * FROM t", []string{styled(styleKeyword, "SELECT")}},
		{"yaml key", "yaml", "name: mdcli", []string{styled(styleBuiltin, "name")}},
		{"yaml list key", "yaml", "  - name: mdcli", []string{styled(styleBuiltin, "name")}},
		{"toml section", "toml", "[package]", []string{styled(styleKeyword, "[package]")}},
		{"diff add", "diff", "+added", []string{styled(styleAdded, "+added")}},
		{"diff remove", "diff", "-gone", []string{styled(styleRemoved, "-gone")}},
		{"diff hunk", "diff", "@@ -1 +1 @@", []string{styled(styleKeyword, "@@ -1 +1 @@")}},
		{"html tag", "html", `<a href="x">`, []string{styled(styleKeyword, "a"), styled(styleBuiltin, "href"), styled(styleString, `"x"`)}},
		{"css property", "css", "  color: red;", []string{styled(styleBuiltin, "color")}},
		{"dockerfile instruction", "dockerfile", "FROM alpine", []string{styled(styleKeyword, "FROM")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := New(tt.lang).Line(tt.line)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("Line(%q) = %q, missing %q", tt.line, got, want)
				}
			}
		})
	}
}

// TestMultiLineConstructsCarryAcrossLines covers the streaming case: a comment
// or string opened on one line must stay styled on the next, because lines are
// handed to the highlighter one at a time.
func TestMultiLineConstructsCarryAcrossLines(t *testing.T) {
	tests := []struct {
		name  string
		lang  string
		lines []string
		style string
	}{
		{"c block comment", "go", []string{"/* one", "two", "three */", "code"}, styleComment},
		{"go raw string", "go", []string{"s := `one", "two`", "code"}, styleString},
		{"python docstring", "python", []string{`"""one`, "two", `three"""`, "code"}, styleString},
		{"html comment", "html", []string{"<!-- one", "two", "three -->", "<b>"}, styleComment},
		{"css comment", "css", []string{"/* one", "two */", "a {"}, styleComment},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := New(tt.lang)
			var got []string
			for _, line := range tt.lines {
				got = append(got, s.Line(line))
			}
			// The middle line is entirely inside the construct.
			if want := styled(tt.style, tt.lines[1]); got[1] != want {
				t.Errorf("continuation line = %q, want %q", got[1], want)
			}
			// The line after the construct closes must be back to normal.
			last := got[len(got)-1]
			if strings.HasPrefix(last, tt.style) {
				t.Errorf("state leaked past the closing delimiter: %q", last)
			}
		})
	}
}

// TestUnterminatedQuoteDoesNotStainTheLine covers the apostrophe cases that
// naive highlighters get wrong: a Rust lifetime and an English contraction.
func TestUnterminatedQuoteDoesNotStainTheLine(t *testing.T) {
	tests := []struct {
		lang string
		line string
	}{
		{"rust", "fn longest<'a>(x: &'a str, y: &'a str) -> &'a str {"},
		{"bash", "echo it doesn't matter how long this line gets"},
		{"go", "// don't let this comment's quote leak"},
	}
	for _, tt := range tests {
		s := New(tt.lang)
		got := s.Line(tt.line)
		if strings.Contains(got, styleString) {
			t.Errorf("%s: %q was styled as a string: %q", tt.lang, tt.line, got)
		}
		if s.mode != modeNormal {
			t.Errorf("%s: %q left the highlighter in mode %d", tt.lang, tt.line, s.mode)
		}
	}
}

// TestSingleCharCommentNeedsWhitespace keeps "#" inside a value from
// commenting out the rest of the line.
func TestSingleCharCommentNeedsWhitespace(t *testing.T) {
	got := New("yaml").Line(`  url: "http://example.com/#anchor"`)
	if strings.Contains(got, styleComment) {
		t.Errorf("a # inside a URL was treated as a comment: %q", got)
	}
	if want := styled(styleComment, "# real"); !strings.Contains(New("yaml").Line("key: v # real"), want) {
		t.Error("a # after whitespace should still start a comment")
	}
}

func TestLookup(t *testing.T) {
	tests := []struct {
		info string
		want string // the language name expected, or "" for no match
	}{
		{"go", "go"},
		{"Go", "go"},
		{"golang", "go"},
		{"go:main.go", "go"},
		{"{.python}", "python"},
		{"python title=x", "python"},
		{"js", "javascript"},
		{"c++", "cpp"},
		{"c#", "csharp"},
		{"yml", "yaml"},
		{"ini", "toml"},
		{"patch", "diff"},
		{"  bash  ", "bash"},
		{"", ""},
		{"brainfuck", ""},
		{"text", ""},
	}
	for _, tt := range tests {
		lang, ok := Lookup(tt.info)
		if tt.want == "" {
			if ok {
				t.Errorf("Lookup(%q) matched a language, want none", tt.info)
			}
			continue
		}
		if !ok {
			t.Errorf("Lookup(%q) found nothing, want %q", tt.info, tt.want)
			continue
		}
		if lang != languages[tt.want] {
			t.Errorf("Lookup(%q) resolved to the wrong language, want %q", tt.info, tt.want)
		}
	}
}

// TestAliasesResolve catches an alias pointing at a language that was renamed
// or never registered.
func TestAliasesResolve(t *testing.T) {
	for alias, target := range aliases {
		if _, ok := languages[target]; !ok {
			t.Errorf("alias %q points at unregistered language %q", alias, target)
		}
	}
}

func TestNilStateHighlightsNothing(t *testing.T) {
	var s *State
	if got := s.Line("func main() {"); got != "func main() {" {
		t.Errorf("nil State altered the line: %q", got)
	}
	if New("") != nil || New("nosuchlang") != nil {
		t.Error("New should return nil for an unknown language")
	}
}

func TestEmptyLine(t *testing.T) {
	for name := range languages {
		if got := New(name).Line(""); got != "" {
			t.Errorf("%s: empty line became %q", name, got)
		}
	}
}

// FuzzLineIsAdditive exercises the invariant against inputs no corpus would
// think to include: unbalanced quotes, stray delimiters, partial escapes.
func FuzzLineIsAdditive(f *testing.F) {
	for _, lines := range corpus {
		for _, line := range lines {
			f.Add(line)
		}
	}
	f.Add("'")
	f.Add(`"`)
	f.Add("/*")
	f.Add("`")
	f.Add(`"""`)
	f.Add("0x")
	f.Add("1e")
	f.Add("<!--")
	f.Add("<a b='")

	names := make([]string, 0, len(languages))
	for name := range languages {
		names = append(names, name)
	}

	f.Fuzz(func(t *testing.T, line string) {
		// A rendered line never contains a newline; the caller splits first.
		line = strings.ReplaceAll(line, "\n", " ")
		// Source that already contains escape sequences cannot be checked this
		// way: stripANSI would remove the input's own escapes along with any
		// the highlighter added. Such input passes through the renderer
		// unchanged whether or not highlighting is on.
		if strings.ContainsRune(line, '\x1b') {
			t.Skip()
		}
		for _, name := range names {
			s := New(name)
			got := s.Line(line)
			if stripped := stripANSI(got); stripped != line {
				t.Fatalf("%s altered the text:\n in:  %q\n out: %q", name, line, stripped)
			}
		}
	})
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
