package runtime

import (
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Incomplete reports whether source ends in a syntax error that more input can
// complete. It parses source without running it. Top-level await uses the
// same narrow expression forms accepted by Eval.
func (s *Session) Incomplete(source string) bool {
	if strings.TrimSpace(source) == "" || staticModuleDeclaration(source) {
		return false
	}
	if form, ok := asyncInputCandidate(source); ok {
		if form.binding != "" && s.h.rt.CheckSyntax(form.binding+" = undefined") != nil {
			return false
		}
		if _, err := normalizeAsyncExpression(s.h.rt, form.expression); err == nil {
			return false
		}
		openExpression := "async function __noe() { return (\n" + form.expression
		return incompleteSyntax(openExpression, s.h.rt.CheckSyntax(openExpression))
	}
	return incompleteSyntax(source, s.h.rt.CheckSyntax(source))
}

func incompleteSyntax(source string, err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, marker := range []string{
		"unexpected end of input",
		"unexpected end of file",
		"found end of input",
		"found end of file",
		"unterminated",
	} {
		if strings.Contains(message, marker) {
			return true
		}
	}
	var positioned interface {
		Position() (string, int, int)
	}
	if !errors.As(err, &positioned) {
		return false
	}
	_, errorLine, errorColumn := positioned.Position()
	endLine, endColumn := sourceEndPosition(source)
	return errorLine == endLine && errorColumn == endColumn
}

func sourceEndPosition(source string) (line, column int) {
	line, column = 1, 1
	for i := 0; i < len(source); {
		r, size := utf8.DecodeRuneInString(source[i:])
		i += size
		switch r {
		case '\r':
			if i < len(source) && source[i] == '\n' {
				i++
			}
			line++
			column = 1
		case '\n', '\u2028', '\u2029':
			line++
			column = 1
		default:
			column++
		}
	}
	return line, column
}

func staticModuleDeclaration(source string) bool {
	remaining, complete := skipJavaScriptTrivia(source)
	if !complete {
		return false
	}
	keyword := ""
	for _, candidate := range []string{"import", "export"} {
		if strings.HasPrefix(remaining, candidate) {
			end := remaining[len(candidate):]
			if end == "" {
				keyword = candidate
				break
			}
			r, _ := utf8.DecodeRuneInString(end)
			if !identifierContinue(r) {
				keyword = candidate
				break
			}
		}
	}
	if keyword == "" {
		return false
	}
	if keyword == "export" {
		return true
	}
	remaining, complete = skipJavaScriptTrivia(remaining[len(keyword):])
	if !complete {
		return false
	}
	// import(...) is a dynamic import expression and may legitimately be
	// continued. Other import forms are static declarations or import.meta,
	// neither of which the REPL accepts as script input.
	return !strings.HasPrefix(remaining, "(")
}

func skipJavaScriptTrivia(source string) (string, bool) {
	for {
		source = strings.TrimLeftFunc(source, unicode.IsSpace)
		if strings.HasPrefix(source, "//") {
			if i := strings.IndexAny(source, "\r\n\u2028\u2029"); i >= 0 {
				_, size := utf8.DecodeRuneInString(source[i:])
				source = source[i+size:]
				continue
			}
			return "", true
		}
		if strings.HasPrefix(source, "/*") {
			i := strings.Index(source[2:], "*/")
			if i < 0 {
				return "", false
			}
			source = source[i+4:]
			continue
		}
		return source, true
	}
}

func identifierContinue(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) ||
		unicode.In(r, unicode.Pc) || r == '$' || r == '\u200c' || r == '\u200d'
}
