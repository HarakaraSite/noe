package highlight

import "strings"

// Language configures the generic scanner. A language with a lex function
// bypasses the scanner entirely and lexes lines itself.
type Language struct {
	// LineComments are tokens that comment out the rest of the line. A
	// single-character token only counts at the start of a line or after
	// whitespace, so "#" in a URL or "a#b" does not swallow the line.
	LineComments []string
	// BlockOpen and BlockClose delimit comments that may span lines.
	BlockOpen, BlockClose string
	// Quotes are the quote characters that delimit single-line strings.
	Quotes string
	// Chars are quote characters that delimit a character literal rather than
	// a string. They only open one when what follows is a well-formed literal,
	// which is how a Rust lifetime stays punctuation.
	Chars string
	// RawStrings are delimiters that both open and close a string that may
	// span lines and performs no escape processing. Checked before Quotes, so
	// list longer delimiters here (`"""` before `"`).
	RawStrings []string
	// Escape is the character that escapes the next byte inside a quoted
	// string, or 0 when the language has none.
	Escape byte
	// StartExtra and PartExtra are characters beyond letters, digits and "_"
	// that may begin or continue an identifier.
	StartExtra, PartExtra string
	// VarPrefix marks identifiers as variables when they begin with one of
	// these characters ($foo, @bar).
	VarPrefix string
	// Fold matches keywords case-insensitively.
	Fold bool

	Keywords map[string]bool
	Builtins map[string]bool

	lex func(*State, string) string
}

func (l *Language) lineCommentAt(line string, i int) bool {
	for _, tok := range l.LineComments {
		if !strings.HasPrefix(line[i:], tok) {
			continue
		}
		if len(tok) == 1 && i > 0 && !isSpace(line[i-1]) {
			continue
		}
		return true
	}
	return false
}

func (l *Language) rawStringAt(line string, i int) string {
	for _, d := range l.RawStrings {
		if strings.HasPrefix(line[i:], d) {
			return d
		}
	}
	return ""
}

func (l *Language) isIdentStart(c byte) bool {
	// Bytes above ASCII are treated as identifier characters so a multi-byte
	// rune is always consumed whole and no escape is ever inserted inside one.
	return isLetter(c) || c == '_' || c >= 0x80 || strings.IndexByte(l.StartExtra, c) >= 0
}

func (l *Language) isIdentPart(c byte) bool {
	return l.isIdentStart(c) || isDigit(c) || strings.IndexByte(l.PartExtra, c) >= 0
}

// Lookup resolves a fenced code block's info string to a language.
func Lookup(info string) (*Language, bool) {
	name := normalize(info)
	if name == "" {
		return nil, false
	}
	if alias, ok := aliases[name]; ok {
		name = alias
	}
	lang, ok := languages[name]
	return lang, ok
}

// normalize reduces the many shapes an info string takes — "go", "Go",
// "go:main.go", "{.python}", "js title=x" — to a bare lowercase name.
func normalize(info string) string {
	info = strings.TrimSpace(info)
	info = strings.Trim(info, "{}")
	if i := strings.IndexAny(info, " \t"); i >= 0 {
		info = info[:i]
	}
	if i := strings.IndexAny(info, ":,="); i >= 0 {
		info = info[:i]
	}
	info = strings.TrimPrefix(info, ".")
	return strings.ToLower(info)
}

func words(s string) map[string]bool {
	m := make(map[string]bool)
	for w := range strings.FieldsSeq(s) {
		m[w] = true
	}
	return m
}

// cFamily returns the shared shape of the curly-brace languages: // and /* */
// comments, single and double quotes, backslash escapes.
func cFamily(keywords, builtins string) *Language {
	return &Language{
		LineComments: []string{"//"},
		BlockOpen:    "/*",
		BlockClose:   "*/",
		Quotes:       `"'`,
		Escape:       '\\',
		Keywords:     words(keywords),
		Builtins:     words(builtins),
	}
}

// charLiteral switches a language from "' delimits a string" to "' delimits a
// character literal", which is the rule in every C-descended language.
func charLiteral(l *Language) *Language {
	l.Quotes = `"`
	l.Chars = "'"
	return l
}

// scripting returns the shape of the # comment languages.
func scripting(keywords, builtins string) *Language {
	return &Language{
		LineComments: []string{"#"},
		Quotes:       `"'`,
		Escape:       '\\',
		Keywords:     words(keywords),
		Builtins:     words(builtins),
	}
}

var aliases = map[string]string{
	"golang":     "go",
	"c++":        "cpp",
	"cc":         "cpp",
	"cxx":        "cpp",
	"h":          "c",
	"hpp":        "cpp",
	"c#":         "csharp",
	"cs":         "csharp",
	"js":         "javascript",
	"mjs":        "javascript",
	"cjs":        "javascript",
	"node":       "javascript",
	"jsx":        "javascript",
	"ts":         "typescript",
	"tsx":        "typescript",
	"py":         "python",
	"python3":    "python",
	"rb":         "ruby",
	"rs":         "rust",
	"kt":         "kotlin",
	"kts":        "kotlin",
	"sh":         "bash",
	"shell":      "bash",
	"zsh":        "bash",
	"console":    "bash",
	"terminal":   "bash",
	"ksh":        "bash",
	"postgres":   "sql",
	"postgresql": "sql",
	"psql":       "sql",
	"mysql":      "sql",
	"sqlite":     "sql",
	"docker":     "dockerfile",
	"make":       "makefile",
	"mk":         "makefile",
	"yml":        "yaml",
	"ini":        "toml",
	"cfg":        "toml",
	"conf":       "toml",
	"htm":        "html",
	"xml":        "html",
	"svg":        "html",
	"vue":        "html",
	"scss":       "css",
	"less":       "css",
	"patch":      "diff",
	"jsonc":      "json",
	"json5":      "json",
}

var languages = map[string]*Language{
	"go": withRaw(charLiteral(cFamily(
		`break case chan const continue default defer else fallthrough for func go goto
		 if import interface map package range return select struct switch type var`,
		`any bool byte comparable complex64 complex128 error float32 float64 int int8
		 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr
		 true false iota nil append cap clear close complex copy delete imag len make
		 max min new panic print println real recover`,
	)), "`"),

	"rust": charLiteral(cFamily(
		`as async await break const continue crate dyn else enum extern fn for if impl
		 in let loop match mod move mut pub ref return static struct super trait type
		 unsafe use where while`,
		`bool char f32 f64 i8 i16 i32 i64 i128 isize str u8 u16 u32 u64 u128 usize
		 String Vec Option Some None Result Ok Err Box Self self true false
		 println print format vec panic assert`,
	)),

	"c": charLiteral(cFamily(
		`auto break case const continue default do else enum extern for goto if inline
		 register restrict return sizeof static struct switch typedef union volatile while`,
		`bool char double float int long short signed unsigned void size_t ssize_t
		 int8_t int16_t int32_t int64_t uint8_t uint16_t uint32_t uint64_t
		 NULL true false`,
	)),

	"cpp": charLiteral(cFamily(
		`alignas alignof auto break case catch class co_await co_return co_yield concept
		 const consteval constexpr continue decltype default delete do else enum explicit
		 export extern final for friend goto if inline mutable namespace new noexcept
		 operator override private protected public register requires return sizeof static
		 static_assert static_cast dynamic_cast const_cast reinterpret_cast struct switch
		 template this throw try typedef typeid typename union using virtual volatile while`,
		`bool char char8_t char16_t char32_t double float int long short signed unsigned
		 void wchar_t size_t nullptr true false std string vector map set pair unique_ptr
		 shared_ptr optional`,
	)),

	"java": charLiteral(cFamily(
		`abstract assert break case catch class const continue default do else enum extends
		 final finally for goto if implements import instanceof interface native new package
		 private protected public record return sealed static strictfp super switch
		 synchronized this throw throws transient try var volatile while yield permits`,
		`boolean byte char double float int long short void String Object List Map Set
		 Integer Boolean Double Long Exception System true false null`,
	)),

	"kotlin": charLiteral(cFamily(
		`as break by class companion const constructor continue crossinline data do else
		 enum external field finally for fun get if import in infix init inline inner
		 interface internal is lateinit noinline object open operator out override package
		 private protected public reified return sealed set super suspend tailrec this throw
		 try typealias val var vararg when where while`,
		`Any Boolean Byte Char Double Float Int Long Nothing Number Short String Unit
		 List Map Set Array true false null it this`,
	)),

	"csharp": charLiteral(cFamily(
		`abstract as async await base break case catch checked class const continue default
		 delegate do else enum event explicit extern finally fixed for foreach get goto if
		 implicit in init interface internal is lock namespace new operator out override
		 params partial private protected public readonly record ref return sealed set
		 sizeof stackalloc static struct switch this throw try typeof unchecked unsafe
		 using var virtual void volatile where while yield`,
		`bool byte char decimal double dynamic float int long nint nuint object sbyte short
		 string uint ulong ushort List Dictionary Task Console Exception true false null`,
	)),

	"swift": charLiteral(cFamily(
		`associatedtype async await break case catch class continue default defer deinit do
		 else enum extension fallthrough fileprivate final for func guard if import in init
		 inout internal is let mutating open operator private protocol public repeat rethrows
		 return self static struct subscript switch throw throws try typealias var where while`,
		`Any AnyObject Bool Character Double Float Int Int8 Int16 Int32 Int64 String UInt
		 Void Array Dictionary Set Optional Error nil true false some any`,
	)),

	"javascript": withRaw(cFamily(
		`as async await break case catch class const continue debugger default delete do
		 else export extends finally for from function get if import in instanceof let new
		 of return set static super switch this throw try typeof var void while with yield`,
		`Array Boolean Date Error JSON Map Math Number Object Promise Proxy RegExp Set
		 String Symbol WeakMap WeakSet console document window globalThis process require
		 module exports undefined null true false NaN Infinity`,
	), "`"),

	"typescript": withRaw(cFamily(
		`abstract as asserts async await break case catch class const continue declare
		 default delete do else enum export extends finally for from function get if
		 implements import in infer instanceof interface is keyof let namespace new of
		 private protected public readonly return satisfies set static super switch this
		 throw try type typeof var void while yield`,
		`Array Boolean Date Error JSON Map Math Number Object Promise Readonly Record
		 RegExp Set String Symbol any bigint boolean never number object string symbol
		 unknown void console document window undefined null true false NaN Infinity`,
	), "`"),

	"php": func() *Language {
		l := cFamily(
			`abstract and array as break callable case catch class clone const continue
			 declare default do echo else elseif empty enum extends final finally fn for
			 foreach function global goto if implements include include_once instanceof
			 insteadof interface isset list match namespace new or print private protected
			 public readonly require require_once return static switch throw trait try
			 unset use var while xor yield`,
			`bool int float string array object mixed void iterable null true false
			 self parent static echo count strlen`,
		)
		l.LineComments = []string{"//", "#"}
		l.StartExtra = "$"
		l.VarPrefix = "$"
		return l
	}(),

	"python": func() *Language {
		l := scripting(
			`and as assert async await break class continue def del elif else except
			 finally for from global if import in is lambda match case nonlocal not or
			 pass raise return try while with yield`,
			`abs all any bin bool bytes callable chr dict dir enumerate eval filter float
			 format frozenset getattr hasattr hash hex id input int isinstance issubclass
			 iter len list map max min next object open ord print range repr reversed round
			 set setattr sorted staticmethod str sum super tuple type vars zip self cls
			 True False None NotImplemented Ellipsis Exception ValueError TypeError`,
		)
		l.RawStrings = []string{`"""`, `'''`}
		return l
	}(),

	"ruby": func() *Language {
		l := scripting(
			`alias and begin break case class def defined? do else elsif end ensure for if
			 in module next not or redo rescue retry return self super then undef unless
			 until when while yield lambda proc require require_relative include extend
			 attr_accessor attr_reader attr_writer`,
			`true false nil puts print raise new String Symbol Array Hash Integer Float
			 Proc Struct Module Class Exception`,
		)
		l.PartExtra = "?!"
		l.StartExtra = "@$"
		l.VarPrefix = "@$"
		return l
	}(),

	"bash": func() *Language {
		l := scripting(
			`alias break case continue declare do done elif else esac eval exec exit export
			 fi for function if in local readonly return select set shift source then time
			 trap typeset unset until while`,
			`awk basename cat cd chmod chown cp curl cut date dirname echo env false find
			 grep head jq kill ln ls mkdir mv printf pwd read rm rmdir sed sleep sort tail
			 tar tee test touch tr true uniq wc which xargs`,
		)
		l.StartExtra = "$"
		l.VarPrefix = "$"
		return l
	}(),

	"sql": func() *Language {
		l := cFamily(
			`add all alter and as asc between by cascade case cast check column commit
			 constraint create cross default delete desc distinct drop else end exists
			 foreign from full group having if in index inner insert intersect into is join
			 key left like limit not null offset on or order outer primary references
			 rename replace returning right rollback select set table then transaction
			 union unique update using values view when where with`,
			`bigint boolean bytea char date decimal double float int integer json jsonb
			 numeric real serial smallint text time timestamp uuid varchar
			 avg coalesce count max min now nullif sum true false null`,
		)
		l.LineComments = []string{"--"}
		l.Fold = true
		return l
	}(),

	"lua": func() *Language {
		l := &Language{
			LineComments: []string{"--"},
			BlockOpen:    "--[[",
			BlockClose:   "]]",
			Quotes:       `"'`,
			Escape:       '\\',
			Keywords: words(`and break do else elseif end false for function goto if in
				local nil not or repeat return then true until while`),
			Builtins: words(`assert error getmetatable io ipairs math os pairs pcall print
				require select setmetatable string table tonumber tostring type
				unpack self`),
		}
		return l
	}(),

	"json": {
		Quotes:   `"`,
		Escape:   '\\',
		Builtins: words(`true false null`),
	},

	"dockerfile": func() *Language {
		l := scripting(
			`add arg cmd copy entrypoint env expose from healthcheck label maintainer
			 onbuild run shell stopsignal user volume workdir as`,
			`apt-get apk yum dnf pip npm yarn go make curl wget`,
		)
		l.Fold = true
		l.PartExtra = "-"
		return l
	}(),

	"makefile": func() *Language {
		l := scripting(
			`define else endef endif ifdef ifeq ifndef ifneq include override unexport
			 export vpath .PHONY .DEFAULT .PRECIOUS .SUFFIXES`,
			`addprefix addsuffix basename call dir error eval filter findstring firstword
			 foreach if info notdir patsubst shell sort strip subst wildcard warning`,
		)
		l.StartExtra = "$."
		l.VarPrefix = "$"
		return l
	}(),
}

// withRaw adds multi-line raw string delimiters to a language.
func withRaw(l *Language, delims ...string) *Language {
	l.RawStrings = append(l.RawStrings, delims...)
	return l
}
