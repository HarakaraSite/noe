package runtime

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type completionToken struct {
	text string
	kind completionTokenKind
}

type completionTokenKind uint8

const (
	completionPunct completionTokenKind = iota
	completionIdentifier
	completionOther
)

// topLevelLexicalNames records global lexical bindings from successful REPL
// inputs. Function and var declarations are object-backed global names and are
// discovered by the completion helper. This scanner deliberately recognizes
// only top-level let/const declaration patterns; it does not report block or
// function locals as REPL bindings.
func topLevelLexicalNames(source string) []string {
	tokens := tokenizeCompletionSource(source)
	var names []string
	var braces, brackets, parens int
	for i, token := range tokens {
		atTopLevel := braces == 0 && brackets == 0 && parens == 0
		if atTopLevel && token.kind == completionIdentifier &&
			(token.text == "let" || token.text == "const") &&
			(i == 0 || (tokens[i-1].text != "." && tokens[i-1].text != "?.")) {
			names = append(names, parseLexicalDeclarators(tokens, i+1)...)
		}

		switch token.text {
		case "{":
			braces++
		case "}":
			if braces > 0 {
				braces--
			}
		case "[":
			brackets++
		case "]":
			if brackets > 0 {
				brackets--
			}
		case "(":
			parens++
		case ")":
			if parens > 0 {
				parens--
			}
		}
	}
	return names
}

func parseLexicalDeclarators(tokens []completionToken, start int) []string {
	var names []string
	i := start
	for {
		declared, next, ok := parseBindingPattern(tokens, i, 0)
		if !ok {
			return names
		}
		names = append(names, declared...)
		i = next
		if i < len(tokens) && tokens[i].text == "=" {
			i = skipDeclarationInitializer(tokens, i+1)
		}
		if i >= len(tokens) || tokens[i].text != "," {
			return names
		}
		i++
	}
}

func skipDeclarationInitializer(tokens []completionToken, start int) int {
	var braces, brackets, parens int
	for i := start; i < len(tokens); i++ {
		token := tokens[i].text
		if braces == 0 && brackets == 0 && parens == 0 && (token == "," || token == ";") {
			return i
		}
		switch token {
		case "{":
			braces++
		case "}":
			if braces > 0 {
				braces--
			}
		case "[":
			brackets++
		case "]":
			if brackets > 0 {
				brackets--
			}
		case "(":
			parens++
		case ")":
			if parens > 0 {
				parens--
			}
		}
	}
	return len(tokens)
}

func tokenizeCompletionSource(source string) []completionToken {
	tokens := make([]completionToken, 0, len(source)/4)
	for i := 0; i < len(source); {
		r, size := utf8.DecodeRuneInString(source[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if strings.HasPrefix(source[i:], "//") {
			i += 2
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				i += size
				if r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' {
					break
				}
			}
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			if end := strings.Index(source[i+2:], "*/"); end >= 0 {
				i += 2 + end + 2
			} else {
				break
			}
			continue
		}
		if r == '\'' || r == '"' {
			i = skipQuoted(source, i, r)
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if r == '`' {
			i = skipTemplate(source, i)
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if r == '/' && regexMayStart(tokens) {
			if next := skipRegex(source, i); next > i {
				i = next
				tokens = append(tokens, completionToken{kind: completionOther})
				continue
			}
		}
		if identifierStart(r) {
			start := i
			i += size
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				if !identifierContinue(r) {
					break
				}
				i += size
			}
			tokens = append(tokens, completionToken{text: source[start:i], kind: completionIdentifier})
			continue
		}
		if unicode.IsDigit(r) {
			i += size
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' {
					break
				}
				i += size
			}
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if strings.HasPrefix(source[i:], "...") {
			tokens = append(tokens, completionToken{text: "...", kind: completionPunct})
			i += 3
			continue
		}
		if strings.HasPrefix(source[i:], "?.") {
			tokens = append(tokens, completionToken{text: "?.", kind: completionPunct})
			i += 2
			continue
		}
		tokens = append(tokens, completionToken{text: string(r), kind: completionPunct})
		i += size
	}
	return tokens
}

func regexMayStart(tokens []completionToken) bool {
	if len(tokens) == 0 {
		return true
	}
	previous := tokens[len(tokens)-1]
	if previous.kind == completionIdentifier {
		switch previous.text {
		case "await", "case", "delete", "do", "else", "in", "instanceof", "new", "of", "return", "throw", "typeof", "void", "yield":
			return true
		default:
			return false
		}
	}
	if previous.kind == completionOther {
		return false
	}
	switch previous.text {
	case ")":
		return closesControlHeader(tokens)
	case "]", "}", "++", "--":
		return false
	default:
		return true
	}
}

func closesControlHeader(tokens []completionToken) bool {
	depth := 0
	for i := len(tokens) - 1; i >= 0; i-- {
		switch tokens[i].text {
		case ")":
			depth++
		case "(":
			depth--
			if depth == 0 {
				if i == 0 || tokens[i-1].kind != completionIdentifier {
					return false
				}
				switch tokens[i-1].text {
				case "catch", "for", "if", "switch", "while", "with":
					return true
				default:
					return false
				}
			}
		}
	}
	return false
}

func skipQuoted(source string, start int, quote rune) int {
	_, size := utf8.DecodeRuneInString(source[start:])
	i := start + size
	for i < len(source) {
		r, size := utf8.DecodeRuneInString(source[i:])
		i += size
		if r == '\\' {
			if i < len(source) {
				_, escapedSize := utf8.DecodeRuneInString(source[i:])
				i += escapedSize
			}
			continue
		}
		if r == quote || r == '\n' || r == '\r' {
			break
		}
	}
	return i
}

func skipTemplate(source string, start int) int {
	i := start + 1
	for i < len(source) {
		r, size := utf8.DecodeRuneInString(source[i:])
		if r == '\\' {
			i += size
			if i < len(source) {
				_, escapedSize := utf8.DecodeRuneInString(source[i:])
				i += escapedSize
			}
			continue
		}
		if r == '`' {
			return i + size
		}
		if r == '$' && i+size < len(source) && source[i+size] == '{' {
			i = skipTemplateExpression(source, i+size+1)
			continue
		}
		i += size
	}
	return i
}

// skipTemplateExpression skips from just after `${` through its matching `}`.
// Nested templates are consumed recursively so their literal contents cannot
// be mistaken for declarations in the surrounding source.
func skipTemplateExpression(source string, start int) int {
	i := start
	var braceDepth int
	var tokens []completionToken
	for i < len(source) {
		r, size := utf8.DecodeRuneInString(source[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if strings.HasPrefix(source[i:], "//") {
			i += 2
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				i += size
				if r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' {
					break
				}
			}
			continue
		}
		if strings.HasPrefix(source[i:], "/*") {
			if end := strings.Index(source[i+2:], "*/"); end >= 0 {
				i += 2 + end + 2
			} else {
				return len(source)
			}
			continue
		}
		if r == '\'' || r == '"' {
			i = skipQuoted(source, i, r)
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if r == '`' {
			i = skipTemplate(source, i)
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if r == '/' && regexMayStart(tokens) {
			if next := skipRegex(source, i); next > i {
				i = next
				tokens = append(tokens, completionToken{kind: completionOther})
				continue
			}
		}
		if identifierStart(r) {
			begin := i
			i += size
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				if !identifierContinue(r) {
					break
				}
				i += size
			}
			tokens = append(tokens, completionToken{text: source[begin:i], kind: completionIdentifier})
			continue
		}
		if unicode.IsDigit(r) {
			i += size
			for i < len(source) {
				r, size = utf8.DecodeRuneInString(source[i:])
				if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' && r != '_' {
					break
				}
				i += size
			}
			tokens = append(tokens, completionToken{kind: completionOther})
			continue
		}
		if strings.HasPrefix(source[i:], "...") {
			tokens = append(tokens, completionToken{text: "...", kind: completionPunct})
			i += 3
			continue
		}
		if strings.HasPrefix(source[i:], "?.") {
			tokens = append(tokens, completionToken{text: "?.", kind: completionPunct})
			i += 2
			continue
		}
		if strings.HasPrefix(source[i:], "++") || strings.HasPrefix(source[i:], "--") {
			tokens = append(tokens, completionToken{text: source[i : i+2], kind: completionPunct})
			i += 2
			continue
		}
		if r == '}' && braceDepth == 0 {
			return i + size
		}
		if r == '{' {
			braceDepth++
		} else if r == '}' {
			braceDepth--
		}
		tokens = append(tokens, completionToken{text: string(r), kind: completionPunct})
		i += size
	}
	return i
}

func skipRegex(source string, start int) int {
	class := false
	escaped := false
	for i := start + 1; i < len(source); i++ {
		switch source[i] {
		case '\n', '\r':
			return start
		case '[':
			if !escaped {
				class = true
			}
		case ']':
			if !escaped {
				class = false
			}
		case '/':
			if !escaped && !class {
				i++
				for i < len(source) {
					r, size := utf8.DecodeRuneInString(source[i:])
					if !identifierContinue(r) {
						break
					}
					i += size
				}
				return i
			}
		case '\\':
			escaped = !escaped
			continue
		}
		escaped = false
	}
	return start
}

func parseBindingPattern(tokens []completionToken, start, depth int) ([]string, int, bool) {
	if depth > 64 || start >= len(tokens) {
		return nil, start, false
	}
	switch tokens[start].text {
	case "{":
		var names []string
		i := start + 1
		for i < len(tokens) {
			if tokens[i].text == "}" {
				return names, i + 1, true
			}
			if tokens[i].text == "," {
				i++
				continue
			}
			if tokens[i].text == "..." {
				child, next, ok := parseBindingPattern(tokens, i+1, depth+1)
				if !ok {
					return nil, start, false
				}
				names = append(names, child...)
				i = next
			} else if tokens[i].text == "[" {
				i = skipBalanced(tokens, i, "[", "]")
				if i < 0 || i >= len(tokens) || tokens[i].text != ":" {
					return nil, start, false
				}
				i++
				child, next, ok := parseBindingPattern(tokens, i, depth+1)
				if !ok {
					return nil, start, false
				}
				names = append(names, child...)
				i = next
			} else {
				key := tokens[i]
				i++
				if i < len(tokens) && tokens[i].text == ":" {
					i++
					child, next, ok := parseBindingPattern(tokens, i, depth+1)
					if !ok {
						return nil, start, false
					}
					names = append(names, child...)
					i = next
				} else if key.kind == completionIdentifier && validIdentifier(key.text) {
					names = append(names, key.text)
				} else {
					return nil, start, false
				}
			}
			if i < len(tokens) && tokens[i].text == "=" {
				i = skipPatternDefault(tokens, i+1, "}")
			}
			if i < len(tokens) && tokens[i].text == "," {
				i++
			}
		}
	case "[":
		var names []string
		i := start + 1
		for i < len(tokens) {
			if tokens[i].text == "]" {
				return names, i + 1, true
			}
			if tokens[i].text == "," {
				i++
				continue
			}
			if tokens[i].text == "..." {
				i++
			}
			child, next, ok := parseBindingPattern(tokens, i, depth+1)
			if !ok {
				return nil, start, false
			}
			names = append(names, child...)
			i = next
			if i < len(tokens) && tokens[i].text == "=" {
				i = skipPatternDefault(tokens, i+1, "]")
			}
			if i < len(tokens) && tokens[i].text == "," {
				i++
			}
		}
	default:
		if tokens[start].kind == completionIdentifier && validIdentifier(tokens[start].text) {
			return []string{tokens[start].text}, start + 1, true
		}
	}
	return nil, start, false
}

func skipBalanced(tokens []completionToken, start int, open, close string) int {
	depth := 0
	for i := start; i < len(tokens); i++ {
		switch tokens[i].text {
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}

func skipPatternDefault(tokens []completionToken, start int, closing string) int {
	var braces, brackets, parens int
	for i := start; i < len(tokens); i++ {
		token := tokens[i].text
		if braces == 0 && brackets == 0 && parens == 0 && (token == "," || token == closing) {
			return i
		}
		switch token {
		case "{":
			braces++
		case "}":
			if braces > 0 {
				braces--
			}
		case "[":
			brackets++
		case "]":
			if brackets > 0 {
				brackets--
			}
		case "(":
			parens++
		case ")":
			if parens > 0 {
				parens--
			}
		}
	}
	return len(tokens)
}
